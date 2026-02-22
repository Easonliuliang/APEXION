// Package mcp provides MCP (Model Context Protocol) client support,
// allowing apexion to connect to external MCP servers and integrate their tools.
package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ServerType specifies the MCP server transport type.
type ServerType string

const (
	ServerTypeStdio ServerType = "stdio" // child process stdin/stdout
	ServerTypeHTTP  ServerType = "http"  // Streamable HTTP
)

// ServerConfig holds connection settings for a single MCP server.
// JSON fields are compatible with Claude Code's mcp.json format:
//
//	{
//	  "command": "npx",
//	  "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
//	  "env": { "KEY": "${ENV_VAR}" }
//	}
type ServerConfig struct {
	// Type is the transport type; inferred from Command/URL if omitted.
	Type ServerType `json:"type,omitempty"`

	// Stdio transport
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`

	// HTTP transport
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// EffectiveType infers the actual transport type.
func (c *ServerConfig) EffectiveType() ServerType {
	if c.Type != "" {
		return c.Type
	}
	if c.URL != "" {
		return ServerTypeHTTP
	}
	return ServerTypeStdio
}

// MCPConfig is the top-level structure of an mcp.json file.
type MCPConfig struct {
	MCPServers map[string]ServerConfig `json:"mcpServers"`
}

// LoadMCPConfig loads and merges global and project-level MCP configuration.
//
// Loading order (later entries override earlier ones with the same server name):
//  1. ~/.config/apexion/mcp.json  (global config)
//  2. <cwd>/.apexion/mcp.json     (project config)
//
// ${VAR} and $VAR references in config values are expanded to environment variables.
func LoadMCPConfig(cwd string) (*MCPConfig, error) {
	merged := &MCPConfig{MCPServers: make(map[string]ServerConfig)}

	// 1. Global config
	home, err := os.UserHomeDir()
	if err == nil {
		globalPath := filepath.Join(home, ".config", "apexion", "mcp.json")
		if cfg, err := loadMCPFile(globalPath); err == nil && cfg != nil {
			for name, srv := range cfg.MCPServers {
				merged.MCPServers[name] = srv
			}
		}
	}

	// 2. Project config (overrides global servers with same name)
	if cwd != "" {
		projectPath := filepath.Join(cwd, ".apexion", "mcp.json")
		if cfg, err := loadMCPFile(projectPath); err == nil && cfg != nil {
			for name, srv := range cfg.MCPServers {
				merged.MCPServers[name] = srv
			}
		}
	}

	// Expand environment variables in all string values
	expanded := &MCPConfig{MCPServers: make(map[string]ServerConfig)}
	for name, srv := range merged.MCPServers {
		expanded.MCPServers[name] = expandServerConfig(srv)
	}

	return expanded, nil
}

// loadMCPFile loads a single mcp.json file from the given path.
// Returns nil, nil if the file does not exist (not treated as an error).
func loadMCPFile(path string) (*MCPConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read mcp config %s: %w", path, err)
	}

	var cfg MCPConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse mcp config %s: %w", path, err)
	}
	if cfg.MCPServers == nil {
		cfg.MCPServers = make(map[string]ServerConfig)
	}
	return &cfg, nil
}

// expandServerConfig expands environment variable references in all ServerConfig fields.
func expandServerConfig(srv ServerConfig) ServerConfig {
	srv.Command = expandEnvVars(srv.Command)
	srv.URL = expandEnvVars(srv.URL)

	expanded := make([]string, len(srv.Args))
	for i, a := range srv.Args {
		expanded[i] = expandEnvVars(a)
	}
	srv.Args = expanded

	if len(srv.Env) > 0 {
		envExp := make(map[string]string, len(srv.Env))
		for k, v := range srv.Env {
			envExp[k] = expandEnvVars(v)
		}
		srv.Env = envExp
	}

	if len(srv.Headers) > 0 {
		hdrs := make(map[string]string, len(srv.Headers))
		for k, v := range srv.Headers {
			hdrs[k] = expandEnvVars(v)
		}
		srv.Headers = hdrs
	}

	return srv
}

// expandEnvVars replaces ${VAR} and $VAR in a string with current environment variable values.
func expandEnvVars(s string) string {
	return os.Expand(s, os.Getenv)
}
