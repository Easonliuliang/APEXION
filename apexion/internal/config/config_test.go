package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Provider != "openai" {
		t.Errorf("expected default provider 'openai', got %q", cfg.Provider)
	}
	if cfg.MaxIterations != 0 {
		t.Errorf("expected default max_iterations 0 (unlimited), got %d", cfg.MaxIterations)
	}
	if cfg.Permissions.Mode != "interactive" {
		t.Errorf("expected default permission mode 'interactive', got %q", cfg.Permissions.Mode)
	}
	if len(cfg.Permissions.AutoApproveTools) != 6 {
		t.Errorf("expected 6 auto-approve tools, got %d", len(cfg.Permissions.AutoApproveTools))
	}
	if cfg.ContextWindow != 0 {
		t.Errorf("expected default context_window 0, got %d", cfg.ContextWindow)
	}
	if !cfg.ToolRouting.Enabled {
		t.Error("expected tool_routing.enabled default true")
	}
	if cfg.ToolRouting.Strategy != "legacy" {
		t.Errorf("expected tool_routing.strategy default legacy, got %q", cfg.ToolRouting.Strategy)
	}
	if cfg.ToolRouting.MaxCandidates != 0 {
		t.Errorf("expected tool_routing.max_candidates default 0, got %d", cfg.ToolRouting.MaxCandidates)
	}
	if cfg.ToolRouting.ShadowEval {
		t.Error("expected tool_routing.shadow_eval default false")
	}
	if cfg.ToolRouting.ShadowSampleRate != 1.0 {
		t.Errorf("expected tool_routing.shadow_sample_rate default 1.0, got %f", cfg.ToolRouting.ShadowSampleRate)
	}
	if cfg.ToolRouting.DeterministicFastpath {
		t.Error("expected tool_routing.deterministic_fastpath default false")
	}
	if cfg.ToolRouting.FastpathConfidence != 0.85 {
		t.Errorf("expected tool_routing.fastpath_confidence default 0.85, got %f", cfg.ToolRouting.FastpathConfidence)
	}
	if !cfg.ToolRouting.EnableRepair {
		t.Error("expected tool_routing.enable_repair default true")
	}
	if !cfg.ToolRouting.EnableFallback {
		t.Error("expected tool_routing.enable_fallback default true")
	}
	if !cfg.ToolRouting.CircuitBreaker.Enabled {
		t.Error("expected tool_routing.circuit_breaker.enabled default true")
	}
	if cfg.ToolRouting.CircuitBreaker.FailThreshold != 3 {
		t.Errorf("expected tool_routing.circuit_breaker.fail_threshold default 3, got %d", cfg.ToolRouting.CircuitBreaker.FailThreshold)
	}
	if cfg.ToolRouting.CircuitBreaker.CooldownSec != 120 {
		t.Errorf("expected tool_routing.circuit_breaker.cooldown_sec default 120, got %d", cfg.ToolRouting.CircuitBreaker.CooldownSec)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	cfg, err := Load("/nonexistent/config.yaml")
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	// Should return default config.
	if cfg.Provider != "openai" {
		t.Errorf("expected default provider, got %q", cfg.Provider)
	}
}

func TestLoad_ValidYAML(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	yaml := `
provider: deepseek
model: deepseek-chat
max_iterations: 50
context_window: 64000
tool_routing:
  enabled: false
  strategy: hybrid
  max_candidates: 6
  shadow_eval: true
  shadow_sample_rate: 0.5
  deterministic_fastpath: true
  fastpath_confidence: 0.9
  enable_repair: false
  enable_fallback: false
  debug: true
  circuit_breaker:
    enabled: true
    fail_threshold: 4
    cooldown_sec: 60
providers:
  deepseek:
    api_key: "sk-test"
    base_url: "https://api.deepseek.com/v1"
    image_input: true
    image_models_allow:
      - "deepseek-vl*"
    image_models_deny:
      - "*-text"
permissions:
  mode: "yolo"
  denied_commands:
    - "rm -rf /"
  allowed_paths:
    - "./src/**"
`
	os.WriteFile(path, []byte(yaml), 0644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Provider != "deepseek" {
		t.Errorf("expected provider 'deepseek', got %q", cfg.Provider)
	}
	if cfg.Model != "deepseek-chat" {
		t.Errorf("expected model 'deepseek-chat', got %q", cfg.Model)
	}
	if cfg.MaxIterations != 50 {
		t.Errorf("expected max_iterations 50, got %d", cfg.MaxIterations)
	}
	if cfg.ContextWindow != 64000 {
		t.Errorf("expected context_window 64000, got %d", cfg.ContextWindow)
	}
	if cfg.ToolRouting.Enabled {
		t.Error("expected tool_routing.enabled false from yaml")
	}
	if cfg.ToolRouting.Strategy != "hybrid" {
		t.Errorf("expected tool_routing.strategy hybrid, got %q", cfg.ToolRouting.Strategy)
	}
	if cfg.ToolRouting.MaxCandidates != 6 {
		t.Errorf("expected tool_routing.max_candidates 6, got %d", cfg.ToolRouting.MaxCandidates)
	}
	if !cfg.ToolRouting.ShadowEval {
		t.Error("expected tool_routing.shadow_eval true from yaml")
	}
	if cfg.ToolRouting.ShadowSampleRate != 0.5 {
		t.Errorf("expected tool_routing.shadow_sample_rate 0.5, got %f", cfg.ToolRouting.ShadowSampleRate)
	}
	if !cfg.ToolRouting.DeterministicFastpath {
		t.Error("expected tool_routing.deterministic_fastpath true from yaml")
	}
	if cfg.ToolRouting.FastpathConfidence != 0.9 {
		t.Errorf("expected tool_routing.fastpath_confidence 0.9, got %f", cfg.ToolRouting.FastpathConfidence)
	}
	if cfg.ToolRouting.EnableRepair {
		t.Error("expected tool_routing.enable_repair false from yaml")
	}
	if cfg.ToolRouting.EnableFallback {
		t.Error("expected tool_routing.enable_fallback false from yaml")
	}
	if !cfg.ToolRouting.Debug {
		t.Error("expected tool_routing.debug true from yaml")
	}
	if !cfg.ToolRouting.CircuitBreaker.Enabled {
		t.Error("expected circuit_breaker.enabled true from yaml")
	}
	if cfg.ToolRouting.CircuitBreaker.FailThreshold != 4 {
		t.Errorf("expected circuit_breaker.fail_threshold 4, got %d", cfg.ToolRouting.CircuitBreaker.FailThreshold)
	}
	if cfg.ToolRouting.CircuitBreaker.CooldownSec != 60 {
		t.Errorf("expected circuit_breaker.cooldown_sec 60, got %d", cfg.ToolRouting.CircuitBreaker.CooldownSec)
	}
	pc := cfg.GetProviderConfig("deepseek")
	if pc.APIKey != "sk-test" {
		t.Errorf("expected api_key 'sk-test', got %q", pc.APIKey)
	}
	if pc.ImageInput == nil || !*pc.ImageInput {
		t.Errorf("expected image_input=true, got %+v", pc.ImageInput)
	}
	if len(pc.ImageModelsAllow) != 1 || pc.ImageModelsAllow[0] != "deepseek-vl*" {
		t.Errorf("unexpected image_models_allow: %+v", pc.ImageModelsAllow)
	}
	if len(pc.ImageModelsDeny) != 1 || pc.ImageModelsDeny[0] != "*-text" {
		t.Errorf("unexpected image_models_deny: %+v", pc.ImageModelsDeny)
	}
	if cfg.Permissions.Mode != "yolo" {
		t.Errorf("expected permission mode 'yolo', got %q", cfg.Permissions.Mode)
	}
	if len(cfg.Permissions.DeniedCommands) != 1 {
		t.Errorf("expected 1 denied command, got %d", len(cfg.Permissions.DeniedCommands))
	}
	if len(cfg.Permissions.AllowedPaths) != 1 {
		t.Errorf("expected 1 allowed path, got %d", len(cfg.Permissions.AllowedPaths))
	}
}

func TestLoad_MissingMaxIterations(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	// No max_iterations in config → should stay 0 (unlimited).
	os.WriteFile(path, []byte("provider: openai\n"), 0644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.MaxIterations != 0 {
		t.Errorf("expected max_iterations 0 (unlimited) when not specified, got %d", cfg.MaxIterations)
	}
}

func TestLoad_ExplicitMaxIterations(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	os.WriteFile(path, []byte("provider: openai\nmax_iterations: 100\n"), 0644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.MaxIterations != 100 {
		t.Errorf("expected max_iterations 100, got %d", cfg.MaxIterations)
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	os.WriteFile(path, []byte("{{invalid yaml"), 0644)

	_, err := Load(path)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	os.WriteFile(path, []byte("provider: openai\n"), 0644)

	// Set env vars for override.
	t.Setenv("LLM_API_KEY", "env-key-123")
	t.Setenv("LLM_BASE_URL", "https://custom.api.com/v1")
	t.Setenv("LLM_MODEL", "custom-model")
	t.Setenv("APEXION_PROVIDER", "deepseek")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Provider != "deepseek" {
		t.Errorf("APEXION_PROVIDER should override, got %q", cfg.Provider)
	}
	if cfg.Model != "custom-model" {
		t.Errorf("LLM_MODEL should override, got %q", cfg.Model)
	}
	// LLM_API_KEY applies to the provider active at config parse time (openai, before APEXION_PROVIDER override).
	// But APEXION_PROVIDER runs after LLM_API_KEY, so key is on "openai".
	pc := cfg.GetProviderConfig("openai")
	if pc.APIKey != "env-key-123" {
		t.Errorf("LLM_API_KEY should set openai api_key, got %q", pc.APIKey)
	}
	if pc.BaseURL != "https://custom.api.com/v1" {
		t.Errorf("LLM_BASE_URL should set base_url, got %q", pc.BaseURL)
	}
}

func TestLoad_AnthropicAPIKey(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	os.WriteFile(path, []byte("provider: anthropic\n"), 0644)

	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pc := cfg.GetProviderConfig("anthropic")
	if pc.APIKey != "sk-ant-test" {
		t.Errorf("ANTHROPIC_API_KEY should set anthropic api_key, got %q", pc.APIKey)
	}
}

func TestGetProviderConfig_Unknown(t *testing.T) {
	cfg := DefaultConfig()
	pc := cfg.GetProviderConfig("nonexistent")
	if pc == nil {
		t.Fatal("expected non-nil provider config for unknown provider")
	}
	if pc.APIKey != "" {
		t.Error("expected empty api_key for unknown provider")
	}
}
