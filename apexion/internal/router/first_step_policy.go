package router

import (
	"fmt"
	"strings"
)

type firstStepPolicy struct {
	ReasonCode string
	AllowFirst map[string]struct{}
	BlockFirst map[string]struct{}
}

func policyForIntent(intent Intent, mode researchFocus) firstStepPolicy {
	switch intent {
	case IntentCodebase:
		return firstStepPolicy{
			ReasonCode: "intent_codebase_first_tool_policy",
			AllowFirst: toSet(
				"repo_map",
				"symbol_nav",
				"task",
				"read_file",
			),
			BlockFirst: toSet(
				"bash",
				"web_fetch",
				"web_search",
			),
		}
	case IntentDebug:
		return firstStepPolicy{
			ReasonCode: "intent_debug_first_tool_policy",
			AllowFirst: toSet(
				"symbol_nav",
				"grep",
				"read_file",
				"bash",
			),
			BlockFirst: toSet(
				"repo_map",
				"web_fetch",
				"web_search",
				"doc_context",
			),
		}
	case IntentResearch:
		switch mode {
		case researchFocusDocs:
			return firstStepPolicy{
				ReasonCode: "intent_research_docs_first_tool_policy",
				AllowFirst: toSet(
					"mcp__context7__resolve-library-id",
					"mcp__context7__get-library-docs",
					"mcp__context7__query-docs",
					"doc_context",
					"web_search",
					"web_fetch",
				),
				BlockFirst: toSet(
					"read_file",
					"glob",
					"grep",
					"list_dir",
					"bash",
				),
			}
		case researchFocusGitHub:
			return firstStepPolicy{
				ReasonCode: "intent_research_github_first_tool_policy",
				AllowFirst: toSet(
					"mcp__github__search_repositories",
					"mcp__github__get_file_contents",
					"mcp__github__search_code",
					"web_search",
					"web_fetch",
				),
				BlockFirst: toSet(
					"read_file",
					"glob",
					"grep",
					"list_dir",
					"bash",
				),
			}
		default:
			return firstStepPolicy{
				ReasonCode: "intent_research_general_first_tool_policy",
				AllowFirst: toSet(
					"doc_context",
					"web_search",
					"web_fetch",
					"task",
				),
				BlockFirst: toSet(
					"read_file",
					"glob",
					"grep",
					"list_dir",
					"bash",
				),
			}
		}
	case IntentGit:
		return firstStepPolicy{
			ReasonCode: "intent_git_first_tool_policy",
			AllowFirst: toSet(
				"git_status",
				"git_diff",
				"git_log",
				"git_branch",
				"git_commit",
				"git_push",
			),
			BlockFirst: toSet(
				"bash",
				"read_file",
				"glob",
				"grep",
				"list_dir",
			),
		}
	case IntentSystem:
		return firstStepPolicy{
			ReasonCode: "intent_system_first_tool_policy",
			AllowFirst: toSet(
				"bash",
				"list_dir",
				"read_file",
				"glob",
				"grep",
			),
		}
	case IntentVision:
		return firstStepPolicy{
			ReasonCode: "intent_vision_first_tool_policy",
			AllowFirst: toSet(
				"mcp__minimax__understand_image",
				"view_image",
				"read_file",
			),
			BlockFirst: toSet(
				"web_fetch",
			),
		}
	default:
		return firstStepPolicy{
			ReasonCode: fmt.Sprintf("intent_%s_first_tool_policy", intent),
		}
	}
}

func enforceFirstStepPolicy(scored []scoredTool, plan *RoutePlan, policy firstStepPolicy) []scoredTool {
	if plan != nil && plan.ReasonCode == "" {
		plan.ReasonCode = policy.ReasonCode
	}
	if len(scored) == 0 {
		return scored
	}
	if len(policy.AllowFirst) == 0 && len(policy.BlockFirst) == 0 {
		return scored
	}

	// Hard first-step policy: only keep tools that are allowed as first call.
	kept := make([]scoredTool, 0, len(scored))
	for _, st := range scored {
		name := normalizeToolName(st.tool.Name)
		if isToolAllowedAsFirst(name, policy) {
			kept = append(kept, st)
			continue
		}
		if plan != nil {
			plan.Filtered = append(plan.Filtered, FilteredTool{
				Name:   st.tool.Name,
				Reason: "first-step policy disallow",
			})
		}
	}
	if len(kept) > 0 {
		if plan != nil && policy.ReasonCode != "" {
			plan.ReasonCode = policy.ReasonCode + "_hard"
		}
		scored = kept
	}

	topName := normalizeToolName(scored[0].tool.Name)
	if isToolAllowedAsFirst(topName, policy) {
		return scored
	}

	matchIdx := -1
	for i, st := range scored {
		name := normalizeToolName(st.tool.Name)
		if isToolAllowedAsFirst(name, policy) {
			matchIdx = i
			break
		}
	}
	if matchIdx <= 0 {
		if plan != nil && policy.ReasonCode != "" {
			plan.ReasonCode = policy.ReasonCode + "_no_match"
		}
		return scored
	}

	promoted := scored[matchIdx]
	copy(scored[1:matchIdx+1], scored[0:matchIdx])
	scored[0] = promoted
	if plan != nil && policy.ReasonCode != "" {
		plan.ReasonCode = policy.ReasonCode + "_promoted"
	}
	return scored
}

func isToolAllowedAsFirst(toolName string, policy firstStepPolicy) bool {
	if _, blocked := policy.BlockFirst[toolName]; blocked {
		return false
	}
	if len(policy.AllowFirst) == 0 {
		return true
	}
	_, ok := policy.AllowFirst[toolName]
	return ok
}

func toSet(names ...string) map[string]struct{} {
	if len(names) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		n := normalizeToolName(name)
		if n == "" {
			continue
		}
		set[n] = struct{}{}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

func normalizeToolName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
