package provider

import "testing"

func TestDetectImageSupport_KnownPositiveModelFamilies(t *testing.T) {
	tests := []struct {
		provider string
		model    string
	}{
		{"openai", "gpt-4o"},
		{"anthropic", "claude-sonnet-4-20250514"},
		{"gemini", "gemini-2.5-pro"},
		{"qwen", "qwen-vl-max"},
		{"glm", "glm-4v"},
	}

	for _, tt := range tests {
		got := DetectImageSupport(tt.provider, tt.model)
		if !got.Supported || !got.Confident {
			t.Fatalf("expected supported+confident for %s/%s, got %+v", tt.provider, tt.model, got)
		}
	}
}

func TestDetectImageSupport_KnownNegativeModels(t *testing.T) {
	tests := []struct {
		provider string
		model    string
	}{
		{"deepseek", "deepseek-chat"},
		{"deepseek", "deepseek-reasoner"},
		{"groq", "llama-3.3-70b-versatile"},
		{"minimax", "MiniMax-M2.5"},
	}

	for _, tt := range tests {
		got := DetectImageSupport(tt.provider, tt.model)
		if got.Supported || !got.Confident {
			t.Fatalf("expected unsupported+confident for %s/%s, got %+v", tt.provider, tt.model, got)
		}
	}
}

func TestDetectImageSupport_UnknownDefaultsToAllow(t *testing.T) {
	got := DetectImageSupport("custom", "my-new-model")
	if !got.Supported {
		t.Fatalf("expected unknown model to be allowed by default, got %+v", got)
	}
}

func TestDetectImageSupportWithConfig_OverrideTakesPriority(t *testing.T) {
	override := false
	got := DetectImageSupportWithConfig(
		"openai",
		"gpt-4o",
		&override,
		[]string{"gpt-4o"},
		[]string{"gpt-4o"},
	)
	if got.Supported || !got.Confident {
		t.Fatalf("expected override=false to block confidently, got %+v", got)
	}
}

func TestDetectImageSupportWithConfig_DenyBeatsAllow(t *testing.T) {
	got := DetectImageSupportWithConfig(
		"openai",
		"gpt-4o",
		nil,
		[]string{"gpt-4o"},
		[]string{"gpt-*"},
	)
	if got.Supported || !got.Confident {
		t.Fatalf("expected deny to beat allow, got %+v", got)
	}
}

func TestDetectImageSupportWithConfig_AllowListMatchAndMiss(t *testing.T) {
	allow := []string{"gpt-4o*", "claude-*"}

	match := DetectImageSupportWithConfig("openai", "Gpt-4O-mini", nil, allow, nil)
	if !match.Supported || !match.Confident {
		t.Fatalf("expected allow-list match to pass confidently, got %+v", match)
	}

	miss := DetectImageSupportWithConfig("openai", "gpt-4.1", nil, allow, nil)
	if miss.Supported || !miss.Confident {
		t.Fatalf("expected allow-list miss to block confidently, got %+v", miss)
	}
}
