package mcp

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Manager manages all configured MCP server connections.
// Thread-safe: concurrent CallTool calls are safe.
type Manager struct {
	mu              sync.RWMutex
	servers         map[string]*serverConn
	maxActive       int
	idleTTL         time.Duration
	failureCooldown time.Duration
}

// serverConn maintains the connection state and tool cache for a single MCP server.
type serverConn struct {
	mu            sync.Mutex
	config        ServerConfig
	name          string // server name, used in logs
	client        *mcp.Client
	session       *mcp.ClientSession
	tools         []*mcp.Tool // ListTools cache
	lastUsed      time.Time
	failCount     int
	cooldownUntil time.Time
}

const (
	defaultMCPMaxActive       = 2
	defaultMCPIdleTTL         = 5 * time.Minute
	defaultMCPFailureCooldown = 20 * time.Second
)

// NewManager creates a Manager from config without connecting immediately.
func NewManager(cfg *MCPConfig) *Manager {
	m := &Manager{
		servers:         make(map[string]*serverConn),
		maxActive:       defaultMCPMaxActive,
		idleTTL:         defaultMCPIdleTTL,
		failureCooldown: defaultMCPFailureCooldown,
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

// EnsureConnected connects only the specified servers.
// If names is empty, it behaves like ConnectAll.
// Returns the subset of servers that are connected after this call
// (including servers that were already connected).
func (m *Manager) EnsureConnected(ctx context.Context, names []string) ([]string, []error) {
	if len(names) == 0 {
		errs := m.ConnectAll(ctx)
		status := m.Status()
		connected := make([]string, 0, len(status))
		for name, st := range status {
			if strings.HasPrefix(st, "connected") {
				connected = append(connected, name)
			}
		}
		return connected, errs
	}

	uniq := dedupeStrings(names)
	preserve := make(map[string]bool, len(uniq))
	for _, name := range uniq {
		preserve[name] = true
	}
	m.disconnectIdleExcept(time.Now(), preserve)

	var connected []string
	var errs []error
	for _, name := range uniq {
		m.mu.RLock()
		conn, ok := m.servers[name]
		m.mu.RUnlock()
		if !ok {
			errs = append(errs, fmt.Errorf("mcp server %q not found", name))
			continue
		}
		now := time.Now()
		if conn.inCooldown(now) {
			errs = append(errs, fmt.Errorf("mcp server %q temporarily unavailable (cooldown)", name))
			continue
		}
		if !conn.isConnected() {
			m.ensureCapacity(preserve)
			if m.maxActive > 0 && m.connectedCount() >= m.maxActive {
				errs = append(errs, fmt.Errorf("mcp server %q skipped (active connection limit reached)", name))
				continue
			}
		}
		if err := conn.connect(ctx); err != nil {
			conn.noteFailure(now, m.failureCooldown)
			errs = append(errs, fmt.Errorf("mcp server %q: %w", name, err))
			continue
		}
		conn.noteSuccess(time.Now())
		connected = append(connected, name)
	}
	return connected, errs
}

// CallTool calls a tool on the specified server. Automatically retries once after reconnecting.
// Returns (output, isError, error):
//   - error indicates a transport/protocol-level error
//   - isError=true means the tool itself returned error content
func (m *Manager) CallTool(ctx context.Context, serverName, toolName string, args map[string]any) (string, bool, error) {
	m.disconnectIdleExcept(time.Now(), map[string]bool{serverName: true})

	m.mu.RLock()
	conn, ok := m.servers[serverName]
	m.mu.RUnlock()
	if !ok {
		return "", false, fmt.Errorf("mcp server %q not found", serverName)
	}
	now := time.Now()
	if conn.inCooldown(now) {
		return "", false, fmt.Errorf("mcp server %q temporarily unavailable (cooldown)", serverName)
	}
	if err := conn.connect(ctx); err != nil {
		conn.noteFailure(now, m.failureCooldown)
		return "", false, fmt.Errorf("call tool %q on %q (connect): %w", toolName, serverName, err)
	}

	result, err := conn.callTool(ctx, toolName, args)
	if err != nil {
		conn.noteFailure(time.Now(), m.failureCooldown)
		// Reconnect once and retry
		if reconnErr := conn.connect(ctx); reconnErr != nil {
			conn.noteFailure(time.Now(), m.failureCooldown)
			return "", false, fmt.Errorf("call tool %q on %q (reconnect failed: %v): %w",
				toolName, serverName, reconnErr, err)
		}
		result, err = conn.callTool(ctx, toolName, args)
		if err != nil {
			conn.noteFailure(time.Now(), m.failureCooldown)
			return "", false, fmt.Errorf("call tool %q on %q: %w", toolName, serverName, err)
		}
	}
	conn.noteSuccess(time.Now())

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
	m.disconnectIdleExcept(time.Now(), nil)

	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make(map[string]string, len(m.servers))
	now := time.Now()
	for name, conn := range m.servers {
		conn.mu.Lock()
		if conn.session != nil {
			out[name] = fmt.Sprintf("connected (%d tools)", len(conn.tools))
		} else if conn.cooldownUntil.After(now) {
			remain := conn.cooldownUntil.Sub(now).Round(time.Second)
			out[name] = fmt.Sprintf("degraded (cooldown %s)", remain)
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
		conn.failCount = 0
		conn.cooldownUntil = time.Time{}
		conn.mu.Unlock()

		if err := conn.connect(ctx); err != nil {
			conn.noteFailure(time.Now(), m.failureCooldown)
			errs = append(errs, fmt.Errorf("mcp server %q: %w", conn.name, err))
		} else {
			conn.noteSuccess(time.Now())
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

// HasServer returns true if the named server exists in config.
func (m *Manager) HasServer(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.servers[name]
	return ok
}

// CleanupIdle disconnects servers that have been idle longer than the configured TTL.
func (m *Manager) CleanupIdle() {
	m.disconnectIdleExcept(time.Now(), nil)
}

func (m *Manager) disconnectIdleExcept(now time.Time, preserve map[string]bool) {
	if m.idleTTL <= 0 {
		return
	}
	m.mu.RLock()
	type pair struct {
		name string
		conn *serverConn
	}
	servers := make([]pair, 0, len(m.servers))
	for name, conn := range m.servers {
		servers = append(servers, pair{name: name, conn: conn})
	}
	m.mu.RUnlock()

	for _, p := range servers {
		if preserve != nil && preserve[p.name] {
			continue
		}
		p.conn.mu.Lock()
		if p.conn.session != nil && !p.conn.lastUsed.IsZero() && now.Sub(p.conn.lastUsed) > m.idleTTL {
			p.conn.disconnect()
		}
		p.conn.mu.Unlock()
	}
}

func (m *Manager) ensureCapacity(preserve map[string]bool) {
	if m.maxActive <= 0 {
		return
	}

	m.mu.RLock()
	type pair struct {
		name string
		conn *serverConn
	}
	servers := make([]pair, 0, len(m.servers))
	for name, conn := range m.servers {
		servers = append(servers, pair{name: name, conn: conn})
	}
	m.mu.RUnlock()

	connected := 0
	var victim *pair
	var victimLastUsed time.Time
	for i := range servers {
		p := &servers[i]
		p.conn.mu.Lock()
		isConnected := p.conn.session != nil
		lastUsed := p.conn.lastUsed
		p.conn.mu.Unlock()
		if !isConnected {
			continue
		}
		connected++
		if preserve != nil && preserve[p.name] {
			continue
		}
		if victim == nil || lastUsed.Before(victimLastUsed) {
			victim = p
			victimLastUsed = lastUsed
		}
	}
	if connected < m.maxActive || victim == nil {
		return
	}

	victim.conn.mu.Lock()
	victim.conn.disconnect()
	victim.conn.mu.Unlock()
}

func (m *Manager) connectedCount() int {
	m.mu.RLock()
	type pair struct {
		conn *serverConn
	}
	servers := make([]pair, 0, len(m.servers))
	for _, conn := range m.servers {
		servers = append(servers, pair{conn: conn})
	}
	m.mu.RUnlock()

	n := 0
	for _, p := range servers {
		p.conn.mu.Lock()
		if p.conn.session != nil {
			n++
		}
		p.conn.mu.Unlock()
	}
	return n
}

// ── serverConn internal methods ──────────────────────────────────────────────

// connect establishes a connection and caches the tool list (idempotent: skips if already connected).
// When a URL-based config has no explicit type, it tries Streamable HTTP first,
// then falls back to SSE (many existing servers still use the 2024-11-05 SSE protocol).
func (conn *serverConn) connect(ctx context.Context) error {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	now := time.Now()

	// Skip if already connected
	if conn.session != nil {
		conn.lastUsed = now
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
	conn.lastUsed = now
	conn.failCount = 0
	conn.cooldownUntil = time.Time{}

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

func (conn *serverConn) isConnected() bool {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	return conn.session != nil
}

func (conn *serverConn) inCooldown(now time.Time) bool {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	return conn.cooldownUntil.After(now)
}

func (conn *serverConn) noteFailure(now time.Time, cooldown time.Duration) {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	conn.failCount++
	if cooldown > 0 {
		multiplier := conn.failCount
		if multiplier > 3 {
			multiplier = 3
		}
		conn.cooldownUntil = now.Add(time.Duration(multiplier) * cooldown)
	}
}

func (conn *serverConn) noteSuccess(now time.Time) {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	conn.lastUsed = now
	conn.failCount = 0
	conn.cooldownUntil = time.Time{}
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

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
