package router

import "sort"

type scoredTool struct {
	tool  CandidateTool
	score int
}

// Plan computes tool routing for one turn.
func Plan(input PlanInput, opts PlanOptions) RoutePlan {
	plan := RoutePlan{
		Intent: ClassifyIntent(input.UserText, input.HasImage),
	}
	if len(input.Tools) == 0 {
		return plan
	}

	preferred := preferredTools(plan.Intent)
	preferredRank := make(map[string]int, len(preferred))
	for i, name := range preferred {
		preferredRank[name] = i
	}

	scored := make([]scoredTool, 0, len(input.Tools))
	for _, t := range input.Tools {
		if reason, filtered := hardGate(input, t); filtered {
			plan.Filtered = append(plan.Filtered, FilteredTool{Name: t.Name, Reason: reason})
			continue
		}
		scored = append(scored, scoredTool{tool: t, score: scoreTool(input, plan.Intent, t, preferredRank)})
	}

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].tool.Name < scored[j].tool.Name
	})

	if opts.MaxCandidates > 0 && len(scored) > opts.MaxCandidates {
		for _, st := range scored[opts.MaxCandidates:] {
			plan.Fallback = append(plan.Fallback, st.tool.Name)
		}
		scored = scored[:opts.MaxCandidates]
	}

	for _, st := range scored {
		plan.Primary = append(plan.Primary, PlannedTool{Name: st.tool.Name, Score: st.score})
	}
	return plan
}

func hardGate(input PlanInput, tool CandidateTool) (string, bool) {
	// Known misrouting pattern: image turns on text-only image providers should not fetch image URLs via web_fetch.
	if input.HasImage && !input.ModelImageSupported && tool.Name == "web_fetch" {
		return "disabled for image turns on text-only image providers", true
	}
	return "", false
}

func scoreTool(input PlanInput, intent Intent, tool CandidateTool, preferredRank map[string]int) int {
	score := 50

	if idx, ok := preferredRank[tool.Name]; ok {
		score += 100 - idx*8
	} else {
		score += 5
	}

	profile := DefaultProfile(tool.Name)
	switch profile.SemanticLevel {
	case SemanticHigh:
		score += 18
	case SemanticMedium:
		score += 10
	default:
		score += 4
	}

	if tool.ReadOnly {
		score += 10
	} else {
		score -= 4
	}

	if profile.Domain == intent {
		score += 20
	}

	// Vision routing: prioritize image-aware tools and penalize text web fetch.
	if input.HasImage {
		if isImageTool(tool.Name) {
			score += 45
		}
		if tool.Name == "web_fetch" {
			score -= 80
		}
	}

	// Research routing: docs and web tools should float up.
	if intent == IntentResearch && (tool.Name == "doc_context" || tool.Name == "web_search" || tool.Name == "web_fetch") {
		score += 20
	}

	// Debug routing: execution tools get a slight boost.
	if intent == IntentDebug && tool.Name == "bash" {
		score += 15
	}

	return score
}

func preferredTools(intent Intent) []string {
	switch intent {
	case IntentVision:
		return []string{
			"mcp__minimax__understand_image",
			"task",
			"repo_map",
			"symbol_nav",
			"read_file",
			"glob",
			"grep",
			"list_dir",
		}
	case IntentGit:
		return []string{
			"git_status",
			"git_diff",
			"git_log",
			"git_branch",
			"git_commit",
			"git_push",
		}
	case IntentResearch:
		return []string{
			"doc_context",
			"web_search",
			"web_fetch",
			"task",
			"repo_map",
			"symbol_nav",
			"read_file",
			"grep",
			"glob",
		}
	case IntentDebug:
		return []string{
			"symbol_nav",
			"bash",
			"read_file",
			"grep",
			"glob",
			"repo_map",
			"git_diff",
			"git_status",
			"task",
		}
	case IntentSystem:
		return []string{
			"bash",
			"list_dir",
			"read_file",
			"glob",
			"grep",
		}
	default:
		return []string{
			"repo_map",
			"symbol_nav",
			"task",
			"read_file",
			"glob",
			"grep",
			"list_dir",
			"todo_read",
			"todo_write",
		}
	}
}
