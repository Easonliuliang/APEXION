package router

import "strings"

// DefaultProfile returns a best-effort profile for built-in and MCP tools.
func DefaultProfile(toolName string) ToolProfile {
	name := strings.ToLower(strings.TrimSpace(toolName))

	switch name {
	case "repo_map":
		return ToolProfile{Domain: IntentCodebase, SemanticLevel: SemanticHigh, Risk: RiskRead}
	case "symbol_nav":
		return ToolProfile{Domain: IntentCodebase, SemanticLevel: SemanticHigh, Risk: RiskRead}
	case "doc_context":
		return ToolProfile{Domain: IntentResearch, SemanticLevel: SemanticHigh, Risk: RiskNetwork}
	case "task":
		return ToolProfile{Domain: IntentCodebase, SemanticLevel: SemanticHigh, Risk: RiskRead}
	case "web_search":
		return ToolProfile{Domain: IntentResearch, SemanticLevel: SemanticHigh, Risk: RiskNetwork}
	case "web_fetch":
		return ToolProfile{Domain: IntentResearch, SemanticLevel: SemanticMedium, Risk: RiskNetwork}
	case "bash":
		return ToolProfile{Domain: IntentSystem, SemanticLevel: SemanticMedium, Risk: RiskExecute}
	case "read_file", "glob", "grep", "list_dir":
		return ToolProfile{Domain: IntentCodebase, SemanticLevel: SemanticPrimitive, Risk: RiskRead}
	case "edit_file", "write_file":
		return ToolProfile{Domain: IntentCodebase, SemanticLevel: SemanticPrimitive, Risk: RiskWrite}
	case "git_status", "git_diff", "git_log", "git_branch":
		return ToolProfile{Domain: IntentGit, SemanticLevel: SemanticMedium, Risk: RiskRead}
	case "git_commit", "git_push":
		return ToolProfile{Domain: IntentGit, SemanticLevel: SemanticMedium, Risk: RiskExecute}
	}

	if strings.HasPrefix(name, "mcp__") {
		if isImageTool(name) {
			return ToolProfile{Domain: IntentVision, SemanticLevel: SemanticHigh, Risk: RiskNetwork}
		}
		if isContext7Tool(name) || isGitHubTool(name) {
			return ToolProfile{Domain: IntentResearch, SemanticLevel: SemanticHigh, Risk: RiskNetwork}
		}
		return ToolProfile{Domain: IntentResearch, SemanticLevel: SemanticMedium, Risk: RiskNetwork}
	}

	if isGitHubTool(name) {
		return ToolProfile{Domain: IntentResearch, SemanticLevel: SemanticMedium, Risk: RiskNetwork}
	}
	if strings.Contains(name, "search") {
		return ToolProfile{Domain: IntentResearch, SemanticLevel: SemanticMedium, Risk: RiskNetwork}
	}
	if strings.Contains(name, "read") || strings.Contains(name, "list") || strings.Contains(name, "grep") || strings.Contains(name, "glob") {
		return ToolProfile{Domain: IntentCodebase, SemanticLevel: SemanticPrimitive, Risk: RiskRead}
	}
	return ToolProfile{Domain: IntentCodebase, SemanticLevel: SemanticPrimitive, Risk: RiskExecute}
}

func isImageTool(name string) bool {
	return strings.Contains(name, "image") || strings.Contains(name, "vision") || strings.Contains(name, "ocr")
}

func isContext7Tool(name string) bool {
	return strings.Contains(name, "context7")
}

func isGitHubTool(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.ReplaceAll(n, "-", "_")
	return strings.Contains(n, "github")
}
