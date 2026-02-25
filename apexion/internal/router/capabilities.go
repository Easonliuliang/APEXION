package router

import "strings"

// NormalizeRoutingStrategy returns a supported strategy with a safe default.
func NormalizeRoutingStrategy(raw RoutingStrategy) RoutingStrategy {
	switch strings.ToLower(strings.TrimSpace(string(raw))) {
	case string(RoutingHybrid):
		return RoutingHybrid
	case string(RoutingCapabilityV2):
		return RoutingCapabilityV2
	default:
		return RoutingLegacy
	}
}

// CapabilityForTool builds a capability declaration for a candidate tool.
func CapabilityForTool(t CandidateTool) ToolCapability {
	profile := DefaultProfile(t.Name)
	capability := ToolCapability{
		Name:                t.Name,
		Domains:             []Intent{profile.Domain},
		SemanticLevel:       profile.SemanticLevel,
		Risk:                profile.Risk,
		Cost:                inferCostClass(t.Name, profile, t.ReadOnly),
		LatencyHintMs:       inferLatencyHintMs(t.Name, profile),
		SupportsParallel:    t.ReadOnly,
		ProviderConstraints: inferProviderConstraints(t.Name),
		DegradePolicy:       inferDegradePolicy(t.Name),
	}

	switch strings.ToLower(strings.TrimSpace(t.Name)) {
	case "symbol_nav":
		capability.DeterministicFor = []string{"symbol_lookup"}
	case "repo_map":
		capability.DeterministicFor = []string{"repo_overview"}
	case "view_image":
		capability.Requires = []string{"model.image_input"}
	}

	return capability
}

// DegradePolicyForTool returns declarative fallback chain for a tool.
func DegradePolicyForTool(toolName string) []string {
	policy := inferDegradePolicy(toolName)
	if len(policy) == 0 {
		return nil
	}
	out := make([]string, 0, len(policy))
	seen := make(map[string]bool, len(policy))
	for _, name := range policy {
		n := strings.TrimSpace(name)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

func inferCostClass(name string, profile ToolProfile, readOnly bool) CostClass {
	n := strings.ToLower(strings.TrimSpace(name))
	if strings.Contains(n, "bash") || strings.Contains(n, "task") || strings.Contains(n, "commit") || strings.Contains(n, "push") {
		return CostHigh
	}
	if strings.HasPrefix(n, "mcp__") || profile.Risk == RiskNetwork {
		return CostMedium
	}
	if !readOnly {
		return CostMedium
	}
	return CostLow
}

func inferLatencyHintMs(name string, profile ToolProfile) int {
	n := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.Contains(n, "symbol_nav"):
		return 300
	case strings.Contains(n, "repo_map"):
		return 900
	case strings.Contains(n, "doc_context"), strings.Contains(n, "web_search"), strings.Contains(n, "web_fetch"):
		return 1400
	case strings.HasPrefix(n, "mcp__"):
		return 1800
	case profile.Risk == RiskExecute:
		return 1500
	default:
		return 250
	}
}

func inferProviderConstraints(name string) []string {
	n := strings.ToLower(strings.TrimSpace(name))
	if isImageTool(n) {
		return []string{"model.image_input=true"}
	}
	return nil
}

func inferDegradePolicy(toolName string) []string {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "doc_context":
		return []string{"web_search", "web_fetch"}
	case "symbol_nav":
		return []string{"grep", "read_file"}
	case "repo_map":
		return []string{"list_dir", "glob"}
	case "web_fetch":
		return []string{"web_search"}
	case "web_search":
		return []string{"web_fetch"}
	case "grep":
		return []string{"glob", "read_file"}
	case "read_file":
		return []string{"list_dir"}
	default:
		return nil
	}
}

// capabilityGate filters tools based on explicit capability requirements.
func capabilityGate(input PlanInput, cap ToolCapability) (string, bool) {
	for _, req := range cap.Requires {
		switch req {
		case "model.image_input":
			if !input.ModelImageSupported {
				return "requires model image input support", true
			}
		}
	}
	return "", false
}

func capabilityHasDomain(cap ToolCapability, intent Intent) bool {
	for _, d := range cap.Domains {
		if d == intent {
			return true
		}
	}
	return false
}
