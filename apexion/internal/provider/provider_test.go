package provider

import (
	"encoding/json"
	"testing"
)

// --- ContextWindow tests ---

func TestOpenAIProvider_ContextWindow(t *testing.T) {
	tests := []struct {
		model    string
		expected int
	}{
		{"gpt-4o-mini", 128000},
		{"gpt-4o", 128000},
		{"gpt-4-turbo", 128000},
		{"o1-preview", 200000},
		{"o3-mini", 200000},
		{"deepseek-chat", 64000},
		{"some-unknown-model", 128000},
	}
	for _, tt := range tests {
		p := &OpenAIProvider{model: tt.model}
		if got := p.ContextWindow(); got != tt.expected {
			t.Errorf("OpenAI ContextWindow(%q) = %d, want %d", tt.model, got, tt.expected)
		}
	}
}

func TestAnthropicProvider_ContextWindow(t *testing.T) {
	tests := []struct {
		model    string
		expected int
	}{
		{"claude-sonnet-4-20250514", 200000},
		{"claude-opus-4-20250514", 200000},
		{"claude-haiku-4-5-20251001", 200000},
		{"claude-unknown-future", 200000},
	}
	for _, tt := range tests {
		p := &AnthropicProvider{model: tt.model}
		if got := p.ContextWindow(); got != tt.expected {
			t.Errorf("Anthropic ContextWindow(%q) = %d, want %d", tt.model, got, tt.expected)
		}
	}
}

// --- Provider metadata tests ---

func TestOpenAIProvider_Metadata(t *testing.T) {
	p := &OpenAIProvider{model: "gpt-4o", name: "openai"}
	if p.Name() != "openai" {
		t.Errorf("expected name 'openai', got %q", p.Name())
	}
	if p.DefaultModel() != "gpt-4o" {
		t.Errorf("expected model 'gpt-4o', got %q", p.DefaultModel())
	}
	models := p.Models()
	if len(models) != 1 || models[0] != "gpt-4o" {
		t.Errorf("expected models [gpt-4o], got %v", models)
	}
}

func TestAnthropicProvider_Metadata(t *testing.T) {
	p := &AnthropicProvider{model: "claude-sonnet-4-20250514"}
	if p.Name() != "anthropic" {
		t.Errorf("expected name 'anthropic', got %q", p.Name())
	}
	if p.DefaultModel() != "claude-sonnet-4-20250514" {
		t.Errorf("expected model 'claude-sonnet-4-20250514', got %q", p.DefaultModel())
	}
}

// --- OpenAI provider name detection ---

func TestOpenAIProvider_NameDetection(t *testing.T) {
	tests := []struct {
		baseURL  string
		expected string
	}{
		{"", "openai"},
		{"https://api.deepseek.com/v1", "deepseek"},
		{"https://api.minimax.chat/v1", "minimax"},
		{"https://generativelanguage.googleapis.com/v1beta/openai/", "gemini"},
		{"https://api.moonshot.cn/v1", "kimi"},
		{"https://dashscope.aliyuncs.com/v1", "qwen"},
		{"https://custom.api.com/v1", "openai"},
	}
	for _, tt := range tests {
		p := NewOpenAIProvider("test-key", tt.baseURL, "test-model")
		if p.Name() != tt.expected {
			t.Errorf("baseURL=%q: expected name %q, got %q", tt.baseURL, tt.expected, p.Name())
		}
	}
}

// --- Message/Content types ---

func TestContentTypes(t *testing.T) {
	if ContentTypeText != "text" {
		t.Errorf("expected 'text', got %q", ContentTypeText)
	}
	if ContentTypeToolUse != "tool_use" {
		t.Errorf("expected 'tool_use', got %q", ContentTypeToolUse)
	}
	if ContentTypeToolResult != "tool_result" {
		t.Errorf("expected 'tool_result', got %q", ContentTypeToolResult)
	}
}

func TestMessage_Roles(t *testing.T) {
	if RoleUser != "user" {
		t.Errorf("expected 'user', got %q", RoleUser)
	}
	if RoleAssistant != "assistant" {
		t.Errorf("expected 'assistant', got %q", RoleAssistant)
	}
}

func TestContent_ToolInput(t *testing.T) {
	input := json.RawMessage(`{"path":"/tmp/test.go"}`)
	c := Content{
		Type:      ContentTypeToolUse,
		ToolUseID: "call_123",
		ToolName:  "read_file",
		ToolInput: input,
	}
	if c.ToolName != "read_file" {
		t.Errorf("expected tool name 'read_file', got %q", c.ToolName)
	}
	var parsed map[string]string
	json.Unmarshal(c.ToolInput, &parsed)
	if parsed["path"] != "/tmp/test.go" {
		t.Errorf("expected path '/tmp/test.go', got %q", parsed["path"])
	}
}

// --- Event types ---

func TestEventTypes(t *testing.T) {
	if EventTextDelta != 0 {
		t.Error("EventTextDelta should be 0")
	}
	if EventToolCallDone != 1 {
		t.Error("EventToolCallDone should be 1")
	}
	if EventDone != 2 {
		t.Error("EventDone should be 2")
	}
	if EventError != 3 {
		t.Error("EventError should be 3")
	}
}

func TestUsage(t *testing.T) {
	u := &Usage{InputTokens: 1000, OutputTokens: 500}
	if u.InputTokens != 1000 || u.OutputTokens != 500 {
		t.Error("usage fields mismatch")
	}
}
