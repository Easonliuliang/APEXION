// Package config 负责加载和管理 aictl 的配置。
// 配置来源优先级（从高到低）：
// 1. 环境变量（LLM_API_KEY, LLM_BASE_URL, LLM_MODEL, ANTHROPIC_API_KEY 等）
// 2. --config flag 指定的配置文件路径
// 3. ~/.config/aictl/config.yaml
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ProviderConfig 单个 provider 的配置
type ProviderConfig struct {
	APIKey  string `yaml:"api_key"`
	BaseURL string `yaml:"base_url"`
	Model   string `yaml:"model"`
}

// PermissionConfig 权限系统配置
type PermissionConfig struct {
	// Mode: "interactive"（默认）| "auto-approve" | "yolo"
	Mode string `yaml:"mode"`

	// AutoApproveTools: 自动批准的工具列表（如 ["read_file", "glob", "grep"]）
	AutoApproveTools []string `yaml:"auto_approve_tools"`

	// AllowedCommands: bash 命令白名单（前缀匹配，如 ["go test", "go build"]）
	AllowedCommands []string `yaml:"allowed_commands"`

	// AllowedPaths: 允许修改的文件路径 glob 模式（如 ["./src/**", "./tests/**"]）
	// 空列表 = 允许所有路径
	AllowedPaths []string `yaml:"allowed_paths"`

	// DeniedCommands: 命令黑名单（即使 auto-approve/yolo 模式下也强制拒绝）
	DeniedCommands []string `yaml:"denied_commands"`
}

// Config 是 aictl 的完整配置结构
type Config struct {
	// Provider 当前使用的 provider 名称（如 "deepseek", "anthropic", "openai"）
	Provider string `yaml:"provider"`

	// Model 当前使用的模型（覆盖 provider 默认模型）
	Model string `yaml:"model"`

	// Providers 各 provider 的具体配置
	Providers map[string]*ProviderConfig `yaml:"providers"`

	// Permissions 权限系统配置
	Permissions PermissionConfig `yaml:"permissions"`

	// SystemPrompt 自定义 system prompt（空则使用默认）
	SystemPrompt string `yaml:"system_prompt"`

	// MaxIterations agent loop 最大迭代次数（默认 25）
	MaxIterations int `yaml:"max_iterations"`

	// ContextWindow overrides the provider's default context window size.
	// 0 = use provider default.
	ContextWindow int `yaml:"context_window"`
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		Provider:      "openai",
		MaxIterations: 25,
		Providers:     make(map[string]*ProviderConfig),
		Permissions: PermissionConfig{
			Mode: "interactive",
			AutoApproveTools: []string{
				"read_file", "glob", "grep", "list_dir",
			},
		},
	}
}

// Load 加载配置文件，合并环境变量覆盖
func Load(configPath string) (*Config, error) {
	cfg := DefaultConfig()

	// 确定配置文件路径
	if configPath == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			configPath = filepath.Join(home, ".config", "aictl", "config.yaml")
		}
	}

	// 读取配置文件（不存在时使用默认配置）
	if data, err := os.ReadFile(configPath); err == nil {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("invalid config file %s: %w", configPath, err)
		}
	}

	// 环境变量覆盖
	applyEnvOverrides(cfg)

	// 初始化 providers map
	if cfg.Providers == nil {
		cfg.Providers = make(map[string]*ProviderConfig)
	}

	return cfg, nil
}

// GetProviderConfig 获取指定 provider 的配置，不存在时返回空配置
func (c *Config) GetProviderConfig(name string) *ProviderConfig {
	if pc, ok := c.Providers[name]; ok {
		return pc
	}
	return &ProviderConfig{}
}

// applyEnvOverrides 将环境变量覆盖到配置中
func applyEnvOverrides(cfg *Config) {
	// 通用覆盖
	if v := os.Getenv("LLM_API_KEY"); v != "" {
		provider := cfg.Provider
		if cfg.Providers[provider] == nil {
			cfg.Providers[provider] = &ProviderConfig{}
		}
		cfg.Providers[provider].APIKey = v
	}
	if v := os.Getenv("LLM_BASE_URL"); v != "" {
		provider := cfg.Provider
		if cfg.Providers[provider] == nil {
			cfg.Providers[provider] = &ProviderConfig{}
		}
		cfg.Providers[provider].BaseURL = v
	}
	if v := os.Getenv("LLM_MODEL"); v != "" {
		cfg.Model = v
	}

	// Anthropic 专用
	if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" {
		if cfg.Providers["anthropic"] == nil {
			cfg.Providers["anthropic"] = &ProviderConfig{}
		}
		cfg.Providers["anthropic"].APIKey = v
	}

	// Provider 选择
	if v := os.Getenv("AICTL_PROVIDER"); v != "" {
		cfg.Provider = v
	}
	if v := os.Getenv("AICTL_MODEL"); v != "" {
		cfg.Model = v
	}
}
