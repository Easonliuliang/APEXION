package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandEnvVars(t *testing.T) {
	t.Setenv("TEST_TOKEN", "abc123")
	cases := []struct {
		in   string
		want string
	}{
		{"hello", "hello"},
		{"${TEST_TOKEN}", "abc123"},
		{"$TEST_TOKEN", "abc123"},
		{"Bearer ${TEST_TOKEN}", "Bearer abc123"},
		{"${MISSING_VAR}", ""},
	}
	for _, tc := range cases {
		got := expandEnvVars(tc.in)
		if got != tc.want {
			t.Errorf("expandEnvVars(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLoadMCPConfig_Empty(t *testing.T) {
	cfg, err := LoadMCPConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
}

func TestLoadMCPConfig_ProjectOverridesGlobal(t *testing.T) {
	// Write a project-level mcp.json in a temp dir
	dir := t.TempDir()
	mcpDir := filepath.Join(dir, ".apexion")
	if err := os.MkdirAll(mcpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{
		"mcpServers": {
			"fs": {
				"command": "project-server",
				"args": ["--root", "/tmp"]
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(mcpDir, "mcp.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadMCPConfig(dir)
	if err != nil {
		t.Fatal(err)
	}

	srv, ok := cfg.MCPServers["fs"]
	if !ok {
		t.Fatal("expected server 'fs'")
	}
	if srv.Command != "project-server" {
		t.Errorf("Command = %q, want %q", srv.Command, "project-server")
	}
}

func TestLoadMCPConfig_EnvExpansion(t *testing.T) {
	t.Setenv("MY_TOKEN", "secret")

	dir := t.TempDir()
	mcpDir := filepath.Join(dir, ".apexion")
	if err := os.MkdirAll(mcpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{
		"mcpServers": {
			"api": {
				"type": "http",
				"url": "http://localhost:8080",
				"headers": {
					"Authorization": "Bearer ${MY_TOKEN}"
				}
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(mcpDir, "mcp.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadMCPConfig(dir)
	if err != nil {
		t.Fatal(err)
	}

	srv := cfg.MCPServers["api"]
	if got := srv.Headers["Authorization"]; got != "Bearer secret" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer secret")
	}
}

func TestServerConfig_EffectiveType(t *testing.T) {
	cases := []struct {
		cfg  ServerConfig
		want ServerType
	}{
		{ServerConfig{Type: ServerTypeStdio}, ServerTypeStdio},
		{ServerConfig{Type: ServerTypeHTTP}, ServerTypeHTTP},
		{ServerConfig{Command: "npx"}, ServerTypeStdio},
		{ServerConfig{URL: "http://localhost"}, ServerTypeHTTP},
		{ServerConfig{}, ServerTypeStdio}, // fallback
	}
	for _, tc := range cases {
		got := tc.cfg.EffectiveType()
		if got != tc.want {
			t.Errorf("EffectiveType(%+v) = %q, want %q", tc.cfg, got, tc.want)
		}
	}
}
