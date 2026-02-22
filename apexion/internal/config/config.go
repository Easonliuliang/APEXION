// Package config loads and manages apexion configuration.
// Configuration source priority (highest to lowest):
// 1. Environment variables (LLM_API_KEY, LLM_BASE_URL, LLM_MODEL, ANTHROPIC_API_KEY, etc.)
// 2. Config file path specified via --config flag
// 3. ~/.config/apexion/config.yaml
package config

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

//go:embed providers_default.yaml
var defaultProvidersYAML []byte

// ProviderDefaults holds the default base URL and model for a provider.
type ProviderDefaults struct {
	BaseURL      string `yaml:"base_url"`
	DefaultModel string `yaml:"default_model"`
}

// LoadProviderDefaults parses the embedded defaults and merges any user
// overrides from ~/.config/apexion/providers.yaml.
func LoadProviderDefaults() map[string]ProviderDefaults {
	defs := make(map[string]ProviderDefaults)
	_ = yaml.Unmarshal(defaultProvidersYAML, &defs)

	home, err := os.UserHomeDir()
	if err == nil {
		userPath := filepath.Join(home, ".config", "apexion", "providers.yaml")
		if data, err := os.ReadFile(userPath); err == nil {
			userDefs := make(map[string]ProviderDefaults)
			if yaml.Unmarshal(data, &userDefs) == nil {
				for name, ud := range userDefs {
					d := defs[name]
					if ud.BaseURL != "" {
						d.BaseURL = ud.BaseURL
					}
					if ud.DefaultModel != "" {
						d.DefaultModel = ud.DefaultModel
					}
					defs[name] = d
				}
			}
		}
	}
	return defs
}

// ProviderConfig holds configuration for a single provider.
type ProviderConfig struct {
	APIKey  string `yaml:"api_key"`
	BaseURL string `yaml:"base_url"`
	Model   string `yaml:"model"`
}

// PermissionConfig holds permission system settings.
type PermissionConfig struct {
	// Mode: "interactive" (default) | "auto-approve" | "yolo"
	Mode string `yaml:"mode"`

	// AutoApproveTools: tools auto-approved without confirmation (e.g. ["read_file", "glob", "grep"])
	AutoApproveTools []string `yaml:"auto_approve_tools"`

	// AllowedCommands: bash command allowlist with prefix matching (e.g. ["go test", "go build"])
	AllowedCommands []string `yaml:"allowed_commands"`

	// AllowedPaths: file path glob patterns allowed for modification (e.g. ["./src/**", "./tests/**"])
	// Empty list = allow all paths
	AllowedPaths []string `yaml:"allowed_paths"`

	// DeniedCommands: command denylist (always blocked, even in auto-approve/yolo mode)
	DeniedCommands []string `yaml:"denied_commands"`
}

// WebConfig holds settings for web tools (web_fetch, web_search).
type WebConfig struct {
	// SearchProvider: "tavily" | "exa" | "jina" (free fallback, no key needed)
	SearchProvider string `yaml:"search_provider"`

	// SearchAPIKey: API key for the search provider (required for Tavily)
	SearchAPIKey string `yaml:"search_api_key"`
}

// Config is the complete configuration structure for apexion.
type Config struct {
	// Provider is the active provider name (e.g. "deepseek", "anthropic", "openai")
	Provider string `yaml:"provider"`

	// Model overrides the provider's default model.
	Model string `yaml:"model"`

	// Providers holds per-provider configuration.
	Providers map[string]*ProviderConfig `yaml:"providers"`

	// Permissions holds permission system settings.
	Permissions PermissionConfig `yaml:"permissions"`

	// Web holds settings for web tools (web_fetch, web_search)
	Web WebConfig `yaml:"web"`

	// SystemPrompt is a custom system prompt (empty uses default).
	SystemPrompt string `yaml:"system_prompt"`

	// MaxIterations is the max number of agent loop iterations.
	// 0 = unlimited (default). Loop exits when model stops calling tools.
	MaxIterations int `yaml:"max_iterations"`

	// ContextWindow overrides the provider's default context window size.
	// 0 = use provider default.
	ContextWindow int `yaml:"context_window"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		Provider:      "openai",
		MaxIterations: 0,
		Providers:     make(map[string]*ProviderConfig),
		Permissions: PermissionConfig{
			Mode: "interactive",
			AutoApproveTools: []string{
				"read_file", "glob", "grep", "list_dir",
				"web_fetch", "web_search",
			},
		},
	}
}

// Load reads the config file and merges environment variable overrides.
func Load(configPath string) (*Config, error) {
	cfg := DefaultConfig()

	// Determine config file path
	if configPath == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			configPath = filepath.Join(home, ".config", "apexion", "config.yaml")
		}
	}

	// Read config file (use defaults if not found)
	if data, err := os.ReadFile(configPath); err == nil {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("invalid config file %s: %w", configPath, err)
		}
	}

	// Apply environment variable overrides
	applyEnvOverrides(cfg)

	// Initialize providers map
	if cfg.Providers == nil {
		cfg.Providers = make(map[string]*ProviderConfig)
	}

	return cfg, nil
}

// GetProviderConfig returns the config for the named provider, or an empty config if not found.
func (c *Config) GetProviderConfig(name string) *ProviderConfig {
	if pc, ok := c.Providers[name]; ok {
		return pc
	}
	return &ProviderConfig{}
}

var (
	// KnownProviderBaseURLs maps well-known provider names to their base URLs.
	// Populated from providers_default.yaml (embedded) + user overrides.
	KnownProviderBaseURLs map[string]string

	// KnownProviderModels maps well-known provider names to their default models.
	// Populated from providers_default.yaml (embedded) + user overrides.
	KnownProviderModels map[string]string
)

func init() {
	defs := LoadProviderDefaults()
	KnownProviderBaseURLs = make(map[string]string, len(defs))
	KnownProviderModels = make(map[string]string, len(defs))
	for name, d := range defs {
		if d.BaseURL != "" {
			KnownProviderBaseURLs[name] = d.BaseURL
		}
		if d.DefaultModel != "" {
			KnownProviderModels[name] = d.DefaultModel
		}
	}
}

// SaveProviderToFile persists a single provider's config and the active provider
// name into ~/.config/apexion/config.yaml, preserving all other user settings.
func SaveProviderToFile(providerName string, pc ProviderConfig) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}
	cfgPath := filepath.Join(home, ".config", "apexion", "config.yaml")

	// Read existing file into a generic map to preserve unknown fields.
	raw := make(map[string]any)
	if data, err := os.ReadFile(cfgPath); err == nil {
		_ = yaml.Unmarshal(data, &raw) // ignore errors; start fresh if corrupt
	}

	// Ensure providers sub-map exists.
	providers, _ := raw["providers"].(map[string]any)
	if providers == nil {
		providers = make(map[string]any)
	}

	// Build the provider entry.
	entry := map[string]any{
		"api_key": pc.APIKey,
	}
	if pc.BaseURL != "" {
		entry["base_url"] = pc.BaseURL
	}
	if pc.Model != "" {
		entry["model"] = pc.Model
	}
	providers[providerName] = entry
	raw["providers"] = providers

	// Set active provider and clear stale global model override.
	raw["provider"] = providerName
	delete(raw, "model")

	// Ensure config directory exists.
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0755); err != nil {
		return fmt.Errorf("cannot create config directory: %w", err)
	}

	data, err := yaml.Marshal(raw)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	if err := os.WriteFile(cfgPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	return nil
}

// applyEnvOverrides applies environment variable overrides to the config.
func applyEnvOverrides(cfg *Config) {
	// Generic overrides
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

	// Anthropic-specific
	if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" {
		if cfg.Providers["anthropic"] == nil {
			cfg.Providers["anthropic"] = &ProviderConfig{}
		}
		cfg.Providers["anthropic"].APIKey = v
	}

	// Provider selection
	if v := os.Getenv("APEXION_PROVIDER"); v != "" {
		cfg.Provider = v
	}
	if v := os.Getenv("APEXION_MODEL"); v != "" {
		cfg.Model = v
	}

	// Web search
	if v := os.Getenv("TAVILY_API_KEY"); v != "" && cfg.Web.SearchAPIKey == "" {
		cfg.Web.SearchAPIKey = v
		if cfg.Web.SearchProvider == "" {
			cfg.Web.SearchProvider = "tavily"
		}
	}
	if v := os.Getenv("EXA_API_KEY"); v != "" && cfg.Web.SearchAPIKey == "" {
		cfg.Web.SearchAPIKey = v
		if cfg.Web.SearchProvider == "" {
			cfg.Web.SearchProvider = "exa"
		}
	}
}
