package mcp

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Manager 管理所有配置的 MCP server 连接。
// 线程安全：并发调用 CallTool 是安全的。
type Manager struct {
	mu      sync.RWMutex
	servers map[string]*serverConn
}

// serverConn 维护单个 MCP server 的连接状态和工具缓存。
type serverConn struct {
	mu      sync.Mutex
	config  ServerConfig
	name    string // server 名称，用于日志
	client  *mcp.Client
	session *mcp.ClientSession
	tools   []*mcp.Tool // ListTools 缓存
}

// NewManager 根据配置创建 Manager，但不立即连接。
func NewManager(cfg *MCPConfig) *Manager {
	m := &Manager{
		servers: make(map[string]*serverConn),
	}
	for name, srv := range cfg.MCPServers {
		m.servers[name] = &serverConn{
			config: srv,
			name:   name,
			client: mcp.NewClient(&mcp.Implementation{
				Name:    "aictl",
				Version: "1.0.0",
			}, nil),
		}
	}
	return m
}

// ConnectAll 尝试连接所有配置的 server，并缓存工具列表。
// 单个 server 连接失败不影响其他 server，错误列表一并返回。
func (m *Manager) ConnectAll(ctx context.Context) []error {
	m.mu.RLock()
	servers := make([]*serverConn, 0, len(m.servers))
	for _, conn := range m.servers {
		servers = append(servers, conn)
	}
	m.mu.RUnlock()

	var errs []error
	for _, conn := range servers {
		if err := conn.connect(ctx); err != nil {
			errs = append(errs, fmt.Errorf("mcp server %q: %w", conn.name, err))
		}
	}
	return errs
}

// CallTool 在指定 server 上调用工具。自动重连一次。
// 返回 (output, isError, error)：
//   - error 表示传输层/协议层错误
//   - isError=true 表示工具本身返回了错误内容
func (m *Manager) CallTool(ctx context.Context, serverName, toolName string, args map[string]any) (string, bool, error) {
	m.mu.RLock()
	conn, ok := m.servers[serverName]
	m.mu.RUnlock()
	if !ok {
		return "", false, fmt.Errorf("mcp server %q not found", serverName)
	}

	result, err := conn.callTool(ctx, toolName, args)
	if err != nil {
		// 重连一次再试
		if reconnErr := conn.connect(ctx); reconnErr != nil {
			return "", false, fmt.Errorf("call tool %q on %q (reconnect failed: %v): %w",
				toolName, serverName, reconnErr, err)
		}
		result, err = conn.callTool(ctx, toolName, args)
		if err != nil {
			return "", false, fmt.Errorf("call tool %q on %q: %w", toolName, serverName, err)
		}
	}

	return extractContent(result), result.IsError, nil
}

// AllTools 返回所有已连接 server 的工具列表，格式为 map[serverName]tools。
func (m *Manager) AllTools() map[string][]*mcp.Tool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make(map[string][]*mcp.Tool)
	for name, conn := range m.servers {
		conn.mu.Lock()
		if conn.tools != nil {
			cp := make([]*mcp.Tool, len(conn.tools))
			copy(cp, conn.tools)
			out[name] = cp
		}
		conn.mu.Unlock()
	}
	return out
}

// Status 返回每个 server 的连接状态描述，供 /mcp 命令展示。
func (m *Manager) Status() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make(map[string]string, len(m.servers))
	for name, conn := range m.servers {
		conn.mu.Lock()
		if conn.session != nil {
			out[name] = fmt.Sprintf("connected (%d tools)", len(conn.tools))
		} else {
			out[name] = "disconnected"
		}
		conn.mu.Unlock()
	}
	return out
}

// Reset 断开并重新连接所有 server。
func (m *Manager) Reset(ctx context.Context) []error {
	m.mu.RLock()
	servers := make([]*serverConn, 0, len(m.servers))
	for _, conn := range m.servers {
		servers = append(servers, conn)
	}
	m.mu.RUnlock()

	var errs []error
	for _, conn := range servers {
		conn.mu.Lock()
		conn.disconnect()
		conn.mu.Unlock()

		if err := conn.connect(ctx); err != nil {
			errs = append(errs, fmt.Errorf("mcp server %q: %w", conn.name, err))
		}
	}
	return errs
}

// Close 关闭所有 server 连接，释放资源。
func (m *Manager) Close() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, conn := range m.servers {
		conn.mu.Lock()
		conn.disconnect()
		conn.mu.Unlock()
	}
}

// ── serverConn 内部方法 ────────────────────────────────────────────────────────

// connect 建立连接并缓存工具列表（幂等：已连接则跳过）。
func (conn *serverConn) connect(ctx context.Context) error {
	conn.mu.Lock()
	defer conn.mu.Unlock()

	// 已连接则跳过
	if conn.session != nil {
		return nil
	}

	transport, err := buildTransport(conn.config)
	if err != nil {
		return err
	}

	session, err := conn.client.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	conn.session = session

	// 缓存工具列表
	result, err := session.ListTools(ctx, nil)
	if err != nil {
		// 连接成功但 ListTools 失败，记录但不报错
		conn.tools = nil
	} else {
		conn.tools = result.Tools
	}

	return nil
}

// disconnect 关闭连接，清理状态（调用方需持有 mu 锁）。
func (conn *serverConn) disconnect() {
	if conn.session != nil {
		_ = conn.session.Close()
		conn.session = nil
	}
	conn.tools = nil
}

// callTool 在已有 session 上调用工具（调用方无需持锁）。
func (conn *serverConn) callTool(ctx context.Context, toolName string, args map[string]any) (*mcp.CallToolResult, error) {
	conn.mu.Lock()
	session := conn.session
	conn.mu.Unlock()

	if session == nil {
		return nil, fmt.Errorf("not connected")
	}

	return session.CallTool(ctx, &mcp.CallToolParams{
		Name:      toolName,
		Arguments: args,
	})
}

// ── 工具函数 ──────────────────────────────────────────────────────────────────

// buildTransport 根据 ServerConfig 创建合适的 MCP 传输层。
func buildTransport(cfg ServerConfig) (mcp.Transport, error) {
	switch cfg.EffectiveType() {
	case ServerTypeStdio:
		if cfg.Command == "" {
			return nil, fmt.Errorf("stdio transport requires 'command'")
		}
		cmd := exec.Command(cfg.Command, cfg.Args...)
		// 继承父进程环境，再追加自定义 env
		if len(cfg.Env) > 0 {
			cmd.Env = os.Environ()
			for k, v := range cfg.Env {
				cmd.Env = append(cmd.Env, k+"="+v)
			}
		}
		return &mcp.CommandTransport{Command: cmd}, nil

	case ServerTypeHTTP:
		if cfg.URL == "" {
			return nil, fmt.Errorf("http transport requires 'url'")
		}
		t := &mcp.StreamableClientTransport{Endpoint: cfg.URL}
		if len(cfg.Headers) > 0 {
			t.HTTPClient = &http.Client{
				Transport: &headerRoundTripper{
					base:    http.DefaultTransport,
					headers: cfg.Headers,
				},
			}
		}
		return t, nil

	default:
		return nil, fmt.Errorf("unknown transport type: %q", cfg.EffectiveType())
	}
}

// extractContent 从 CallToolResult 提取文本内容。
func extractContent(result *mcp.CallToolResult) string {
	if result == nil {
		return ""
	}
	var parts []string
	for _, c := range result.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// headerRoundTripper 为每个 HTTP 请求注入固定请求头。
type headerRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// 克隆请求以避免修改原始请求
	r := req.Clone(req.Context())
	if r.Header == nil {
		r.Header = make(http.Header)
	}
	for k, v := range t.headers {
		r.Header.Set(k, v)
	}
	return t.base.RoundTrip(r)
}
