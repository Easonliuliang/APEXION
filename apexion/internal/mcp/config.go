// Package mcp 提供 MCP (Model Context Protocol) 客户端支持，
// 允许 apexion 连接外部 MCP server 并将其工具纳入 agent 的工具集。
package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ServerType 指定 MCP server 的传输类型。
type ServerType string

const (
	ServerTypeStdio ServerType = "stdio" // 子进程 stdin/stdout
	ServerTypeHTTP  ServerType = "http"  // Streamable HTTP (新协议)
)

// ServerConfig 单个 MCP server 的连接配置。
// JSON 字段与 Claude Code 的 mcp.json 格式兼容：
//
//	{
//	  "command": "npx",
//	  "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
//	  "env": { "KEY": "${ENV_VAR}" }
//	}
type ServerConfig struct {
	// Type 传输类型，省略时按 Command/URL 字段自动推断
	Type ServerType `json:"type,omitempty"`

	// Stdio transport
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`

	// HTTP transport
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// EffectiveType 推断实际传输类型。
func (c *ServerConfig) EffectiveType() ServerType {
	if c.Type != "" {
		return c.Type
	}
	if c.URL != "" {
		return ServerTypeHTTP
	}
	return ServerTypeStdio
}

// MCPConfig 是 mcp.json 文件的顶层结构。
type MCPConfig struct {
	MCPServers map[string]ServerConfig `json:"mcpServers"`
}

// LoadMCPConfig 加载并合并全局和项目级 MCP 配置。
//
// 加载顺序（后者覆盖前者中的同名 server）：
//  1. ~/.config/apexion/mcp.json （全局配置）
//  2. <cwd>/.apexion/mcp.json   （项目配置）
//
// 配置值中的 ${VAR} 和 $VAR 引用会被展开为当前环境变量。
func LoadMCPConfig(cwd string) (*MCPConfig, error) {
	merged := &MCPConfig{MCPServers: make(map[string]ServerConfig)}

	// 1. 全局配置
	home, err := os.UserHomeDir()
	if err == nil {
		globalPath := filepath.Join(home, ".config", "apexion", "mcp.json")
		if cfg, err := loadMCPFile(globalPath); err == nil && cfg != nil {
			for name, srv := range cfg.MCPServers {
				merged.MCPServers[name] = srv
			}
		}
	}

	// 2. 项目配置（覆盖全局同名 server）
	if cwd != "" {
		projectPath := filepath.Join(cwd, ".apexion", "mcp.json")
		if cfg, err := loadMCPFile(projectPath); err == nil && cfg != nil {
			for name, srv := range cfg.MCPServers {
				merged.MCPServers[name] = srv
			}
		}
	}

	// 展开所有字符串值中的环境变量
	expanded := &MCPConfig{MCPServers: make(map[string]ServerConfig)}
	for name, srv := range merged.MCPServers {
		expanded.MCPServers[name] = expandServerConfig(srv)
	}

	return expanded, nil
}

// loadMCPFile 从指定路径加载单个 mcp.json 文件。
// 文件不存在时返回 nil, nil（不视为错误）。
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

// expandServerConfig 展开 ServerConfig 所有字段中的环境变量引用。
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

// expandEnvVars 将字符串中的 ${VAR} 和 $VAR 替换为当前环境变量值。
func expandEnvVars(s string) string {
	return os.Expand(s, os.Getenv)
}
