package mcp

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestServerConnCooldownLifecycle(t *testing.T) {
	conn := &serverConn{}
	now := time.Now()

	conn.noteFailure(now, 2*time.Second)
	if !conn.inCooldown(now.Add(1 * time.Second)) {
		t.Fatal("expected cooldown after failure")
	}
	conn.noteSuccess(now.Add(2 * time.Second))
	if conn.inCooldown(now.Add(2 * time.Second)) {
		t.Fatal("expected cooldown cleared after success")
	}
}

func TestManagerStatusShowsDegradedWhenCoolingDown(t *testing.T) {
	m := NewManager(&MCPConfig{
		MCPServers: map[string]ServerConfig{
			"github": {Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-github"}},
		},
	})

	conn := m.servers["github"]
	conn.mu.Lock()
	conn.cooldownUntil = time.Now().Add(3 * time.Second)
	conn.mu.Unlock()

	status := m.Status()
	if !strings.HasPrefix(status["github"], "degraded (cooldown") {
		t.Fatalf("expected degraded cooldown status, got %q", status["github"])
	}
}

func TestEnsureConnectedSkipsCoolingDownServer(t *testing.T) {
	m := NewManager(&MCPConfig{
		MCPServers: map[string]ServerConfig{
			"github": {Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-github"}},
		},
	})

	conn := m.servers["github"]
	conn.mu.Lock()
	conn.cooldownUntil = time.Now().Add(5 * time.Second)
	conn.mu.Unlock()

	connected, errs := m.EnsureConnected(context.Background(), []string{"github"})
	if len(connected) != 0 {
		t.Fatalf("expected no connected servers during cooldown, got %v", connected)
	}
	if len(errs) == 0 || !strings.Contains(errs[0].Error(), "cooldown") {
		t.Fatalf("expected cooldown error, got %v", errs)
	}
}
