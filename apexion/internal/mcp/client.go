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

// Manager manages all configured MCP server connections.
// Thread-safe: concurrent CallTool calls are safe.
type Manager struct {
	mu      sync.RWMutex
	servers map[string]*serverConn
}

// serverConn maintains the connection state and tool cache for a single MCP server.
type serverConn struct {
	mu      sync.Mutex
	config  ServerConfig
	name    string // server name, used in logs
	client  *mcp.Client
	session *mcp.ClientSession
	tools   []*mcp.Tool // ListTools cache
}

// NewManager creates a Manager from config without connecting immediately.
func NewManager(cfg *MCPConfig) *Manager {
	m := &Manager{
		servers: make(map[string]*serverConn),
	}
	for name, srv := range cfg.MCPServers {
		m.servers[name] = &serverConn{
			config: srv,
			name:   name,
			client: mcp.NewClient(&mcp.Implementation{
				Name:    "apexion",
				Version: "1.0.0",
			}, nil),
		}
	}
	return m
}

// ConnectAll connects to all configured servers and caches their tool lists.
// Individual server failures do not affect others; all errors are returned.
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

// CallTool calls a tool on the specified server. Automatically retries once after reconnecting.
// Returns (output, isError, error):
//   - error indicates a transport/protocol-level error
//   - isError=true means the tool itself returned error content
func (m *Manager) CallTool(ctx context.Context, serverName, toolName string, args map[string]any) (string, bool, error) {
	m.mu.RLock()
	conn, ok := m.servers[serverName]
	m.mu.RUnlock()
	if !ok {
		return "", false, fmt.Errorf("mcp server %q not found", serverName)
	}

	result, err := conn.callTool(ctx, toolName, args)
	if err != nil {
		// Reconnect once and retry
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

// AllTools returns all connected servers' tools as map[serverName]tools.
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

// Status returns a connection status description for each server (used by /mcp command).
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

// Reset disconnects and reconnects all servers.
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

// Close shuts down all server connections and releases resources.
func (m *Manager) Close() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, conn := range m.servers {
		conn.mu.Lock()
		conn.disconnect()
		conn.mu.Unlock()
	}
}

// ── serverConn internal methods ──────────────────────────────────────────────

// connect establishes a connection and caches the tool list (idempotent: skips if already connected).
// When a URL-based config has no explicit type, it tries Streamable HTTP first,
// then falls back to SSE (many existing servers still use the 2024-11-05 SSE protocol).
func (conn *serverConn) connect(ctx context.Context) error {
	conn.mu.Lock()
	defer conn.mu.Unlock()

	// Skip if already connected
	if conn.session != nil {
		return nil
	}

	// Determine if we should try auto-detection (URL present, no explicit type).
	autoDetect := conn.config.URL != "" && conn.config.Type == ""

	transport, err := buildTransport(conn.config)
	if err != nil {
		return err
	}

	session, err := conn.client.Connect(ctx, transport, nil)
	if err != nil && autoDetect {
		// Streamable HTTP failed — try SSE fallback.
		// Create a fresh client for the SSE attempt (Connect is one-shot).
		conn.client = mcp.NewClient(&mcp.Implementation{
			Name:    "apexion",
			Version: "1.0.0",
		}, nil)
		sseCfg := conn.config
		sseCfg.Type = ServerTypeSSE
		sseTransport, sseErr := buildTransport(sseCfg)
		if sseErr != nil {
			return fmt.Errorf("connect (streamable HTTP failed: %v; SSE build failed: %v)", err, sseErr)
		}
		session, sseErr = conn.client.Connect(ctx, sseTransport, nil)
		if sseErr != nil {
			return fmt.Errorf("connect failed (tried streamable HTTP: %v; SSE: %v)", err, sseErr)
		}
		// SSE succeeded — remember for future reconnects.
		conn.config.Type = ServerTypeSSE
	} else if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	conn.session = session

	// Cache tool list
	result, err := session.ListTools(ctx, nil)
	if err != nil {
		// Connected but ListTools failed; log but don't error
		conn.tools = nil
	} else {
		conn.tools = result.Tools
	}

	return nil
}

// disconnect closes the connection and cleans up state (caller must hold mu lock).
func (conn *serverConn) disconnect() {
	if conn.session != nil {
		_ = conn.session.Close()
		conn.session = nil
	}
	conn.tools = nil
}

// callTool calls a tool on an existing session (caller does not need to hold the lock).
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

// ── Utility functions ────────────────────────────────────────────────────────

// buildTransport creates the appropriate MCP transport based on ServerConfig.
func buildTransport(cfg ServerConfig) (mcp.Transport, error) {
	switch cfg.EffectiveType() {
	case ServerTypeStdio:
		if cfg.Command == "" {
			return nil, fmt.Errorf("stdio transport requires 'command'")
		}
		cmd := exec.Command(cfg.Command, cfg.Args...)
		// Inherit parent process env, then append custom env
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

	case ServerTypeSSE:
		if cfg.URL == "" {
			return nil, fmt.Errorf("sse transport requires 'url'")
		}
		t := &mcp.SSEClientTransport{Endpoint: cfg.URL}
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

// extractContent extracts text content from a CallToolResult.
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

// headerRoundTripper injects fixed headers into every HTTP request.
type headerRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone request to avoid mutating the original
	r := req.Clone(req.Context())
	if r.Header == nil {
		r.Header = make(http.Header)
	}
	for k, v := range t.headers {
		r.Header.Set(k, v)
	}
	return t.base.RoundTrip(r)
}
