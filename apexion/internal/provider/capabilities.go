package provider

import (
	"fmt"
	"path"
	"strings"
)

// ImageSupportDecision describes whether image input is supported and how
// confident the detector is about this conclusion.
type ImageSupportDecision struct {
	Supported bool
	Confident bool
	Reason    string
}

// DetectImageSupport estimates whether a provider/model combination supports
// image inputs.
//
// This is intentionally heuristic-based:
//   - Prefer clearly-known model families (gpt-4o/claude/gemini/*vision/*vl, etc.)
//   - Return "unknown" (Supported=true, Confident=false) when uncertain, so we do
//     not accidentally block user input for custom or newly released models.
func DetectImageSupport(providerName, model string) ImageSupportDecision {
	p := strings.ToLower(strings.TrimSpace(providerName))
	m := strings.ToLower(strings.TrimSpace(model))

	// Strong positive signals from model id.
	for _, kw := range []string{
		"gpt-4o", "gpt-4.1", "o1", "o3", "claude", "gemini",
		"vision", "-vl", "multimodal", "glm-4v", "qvq",
		"doubao-vision",
	} {
		if strings.Contains(m, kw) {
			return ImageSupportDecision{
				Supported: true,
				Confident: true,
				Reason:    "model family advertises multimodal image input",
			}
		}
	}

	// Known text-only defaults for some providers/models.
	switch {
	case strings.Contains(m, "deepseek-chat"), strings.Contains(m, "deepseek-reasoner"):
		return ImageSupportDecision{
			Supported: false,
			Confident: true,
			Reason:    "selected DeepSeek chat/reasoner model is text-only",
		}
	case p == "groq" && !strings.Contains(m, "vision"):
		return ImageSupportDecision{
			Supported: false,
			Confident: true,
			Reason:    "selected Groq model is not a vision variant",
		}
	case p == "minimax":
		return ImageSupportDecision{
			Supported: false,
			Confident: true,
			Reason:    "MiniMax OpenAI-compatible chat API is text-only; use MCP understand_image",
		}
	}

	// Provider-level defaults when model name is not descriptive enough.
	switch p {
	case "anthropic", "gemini", "qwen", "glm", "doubao":
		return ImageSupportDecision{
			Supported: true,
			Confident: true,
			Reason:    "provider family commonly supports image input",
		}
	}

	// Unknown model/provider combination: allow by default, but mark uncertain.
	return ImageSupportDecision{
		Supported: true,
		Confident: false,
		Reason:    "unknown model capability",
	}
}

// DetectImageSupportWithConfig applies user-configured image capability rules
// before falling back to DetectImageSupport heuristics.
//
// Priority:
// 1) override (image_input)
// 2) deny list (image_models_deny)
// 3) allow list (image_models_allow)
// 4) heuristic detection
func DetectImageSupportWithConfig(
	providerName, model string,
	override *bool,
	allow, deny []string,
) ImageSupportDecision {
	if override != nil {
		if *override {
			return ImageSupportDecision{
				Supported: true,
				Confident: true,
				Reason:    "enabled by config (image_input=true)",
			}
		}
		return ImageSupportDecision{
			Supported: false,
			Confident: true,
			Reason:    "disabled by config (image_input=false)",
		}
	}

	if rule, ok := matchModelList(model, deny); ok {
		return ImageSupportDecision{
			Supported: false,
			Confident: true,
			Reason:    fmt.Sprintf("blocked by config deny rule %q", rule),
		}
	}

	if len(allow) > 0 {
		if rule, ok := matchModelList(model, allow); ok {
			return ImageSupportDecision{
				Supported: true,
				Confident: true,
				Reason:    fmt.Sprintf("allowed by config allow rule %q", rule),
			}
		}
		return ImageSupportDecision{
			Supported: false,
			Confident: true,
			Reason:    "model not in configured allow list",
		}
	}

	return DetectImageSupport(providerName, model)
}

func matchModelList(model string, rules []string) (string, bool) {
	m := strings.ToLower(strings.TrimSpace(model))
	for _, raw := range rules {
		rule := strings.ToLower(strings.TrimSpace(raw))
		if rule == "" {
			continue
		}
		if isGlobRule(rule) {
			if ok, _ := path.Match(rule, m); ok {
				return raw, true
			}
			continue
		}
		if m == rule {
			return raw, true
		}
	}
	return "", false
}

func isGlobRule(rule string) bool {
	return strings.ContainsAny(rule, "*?[")
}
