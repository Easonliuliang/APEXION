package router

import (
	"math/rand"
	"sort"
	"strings"
)

type researchFocus string

const (
	researchFocusGeneral researchFocus = "general"
	researchFocusDocs    researchFocus = "docs"
	researchFocusGitHub  researchFocus = "github"
)

type scoredTool struct {
	tool  CandidateTool
	score int
}

// Plan computes tool routing for one turn.
func Plan(input PlanInput, opts PlanOptions) RoutePlan {
	strategy := NormalizeRoutingStrategy(opts.Strategy)
	shadowRate := clampSampleRate(opts.ShadowSampleRate)
	switch strategy {
	case RoutingCapabilityV2:
		return planInternal(input, opts.MaxCandidates, true, opts)
	case RoutingHybrid:
		legacy := planInternal(input, opts.MaxCandidates, false, opts)
		if shouldEmitShadow(strategy, opts.ShadowEval, shadowRate) {
			shadow := planInternal(input, opts.MaxCandidates, true, opts)
			legacy.Shadow = &ShadowPlan{
				Strategy: RoutingCapabilityV2,
				Primary:  shadow.Primary,
				Fallback: shadow.Fallback,
				Filtered: shadow.Filtered,
			}
			if legacy.FastPath == nil {
				legacy.FastPath = shadow.FastPath
			}
		}
		return legacy
	default:
		legacy := planInternal(input, opts.MaxCandidates, false, opts)
		if shouldEmitShadow(strategy, opts.ShadowEval, shadowRate) {
			shadow := planInternal(input, opts.MaxCandidates, true, opts)
			legacy.Shadow = &ShadowPlan{
				Strategy: RoutingCapabilityV2,
				Primary:  shadow.Primary,
				Fallback: shadow.Fallback,
				Filtered: shadow.Filtered,
			}
		}
		return legacy
	}
}

func planInternal(input PlanInput, maxCandidates int, capabilityV2 bool, opts PlanOptions) RoutePlan {
	plan := RoutePlan{
		Intent: ClassifyIntent(input.UserText, input.HasImage),
	}
	if len(input.Tools) == 0 {
		return plan
	}

	researchMode := inferResearchFocus(plan.Intent, input.UserText)
	preferred := preferredTools(plan.Intent, researchMode)
	preferredRank := make(map[string]int, len(preferred))
	for i, name := range preferred {
		preferredRank[name] = i
	}

	scored := make([]scoredTool, 0, len(input.Tools))
	capabilityByTool := make(map[string]ToolCapability, len(input.Tools))
	for _, t := range input.Tools {
		if reason, filtered := hardGate(input, t); filtered {
			plan.Filtered = append(plan.Filtered, FilteredTool{Name: t.Name, Reason: reason})
			continue
		}
		capability := CapabilityForTool(t)
		capabilityByTool[t.Name] = capability
		if capabilityV2 {
			if reason, filtered := capabilityGate(input, capability); filtered {
				plan.Filtered = append(plan.Filtered, FilteredTool{Name: t.Name, Reason: reason})
				continue
			}
			scored = append(scored, scoredTool{
				tool:  t,
				score: scoreToolV2(input, plan.Intent, researchMode, t, capability, preferredRank),
			})
			continue
		}
		scored = append(scored, scoredTool{tool: t, score: scoreTool(input, plan.Intent, researchMode, t, preferredRank)})
	}

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].tool.Name < scored[j].tool.Name
	})

	if maxCandidates > 0 && len(scored) > maxCandidates {
		for _, st := range scored[maxCandidates:] {
			plan.Fallback = append(plan.Fallback, st.tool.Name)
		}
		scored = scored[:maxCandidates]
	}

	for _, st := range scored {
		plan.Primary = append(plan.Primary, PlannedTool{Name: st.tool.Name, Score: st.score})
	}
	if capabilityV2 && opts.DeterministicFastpath {
		confidenceThreshold := opts.FastpathConfidence
		if confidenceThreshold <= 0 {
			confidenceThreshold = 0.85
		}
		plan.FastPath = buildDeterministicFastpath(input, plan.Intent, plan.Primary, capabilityByTool, confidenceThreshold)
	}
	return plan
}

func shouldEmitShadow(strategy RoutingStrategy, shadowEval bool, sampleRate float64) bool {
	if !shadowEval && strategy != RoutingHybrid {
		return false
	}
	if sampleRate <= 0 {
		return false
	}
	if sampleRate >= 1 {
		return true
	}
	return rand.Float64() < sampleRate
}

func clampSampleRate(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func hardGate(input PlanInput, tool CandidateTool) (string, bool) {
	// Known misrouting pattern: image turns on text-only image providers should not fetch image URLs via web_fetch.
	if input.HasImage && !input.ModelImageSupported && tool.Name == "web_fetch" {
		return "disabled for image turns on text-only image providers", true
	}
	return "", false
}

func scoreTool(input PlanInput, intent Intent, mode researchFocus, tool CandidateTool, preferredRank map[string]int) int {
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

	// Research routing: prioritize high-semantic docs/github tools before generic fetch.
	if intent == IntentResearch {
		switch mode {
		case researchFocusDocs:
			switch {
			case isContext7Tool(tool.Name):
				score += 46
			case tool.Name == "doc_context":
				score += 28
			case tool.Name == "web_search":
				score += 20
			case tool.Name == "web_fetch":
				score += 6
			case isGitHubTool(tool.Name):
				score -= 8
			}
		case researchFocusGitHub:
			switch {
			case isGitHubTool(tool.Name):
				score += 42
			case tool.Name == "web_search":
				score += 22
			case tool.Name == "web_fetch":
				score += 12
			case isContext7Tool(tool.Name):
				score += 10
			case tool.Name == "doc_context":
				score -= 6
			}
		default:
			if tool.Name == "doc_context" || tool.Name == "web_search" || tool.Name == "web_fetch" || isContext7Tool(tool.Name) || isGitHubTool(tool.Name) {
				score += 20
			}
		}
	}

	// Debug routing: execution tools get a slight boost.
	if intent == IntentDebug && tool.Name == "bash" {
		score += 15
	}

	return score
}

func scoreToolV2(input PlanInput, intent Intent, mode researchFocus, tool CandidateTool, cap ToolCapability, preferredRank map[string]int) int {
	score := 40

	if idx, ok := preferredRank[tool.Name]; ok {
		score += 92 - idx*7
	} else {
		score += 4
	}

	switch cap.SemanticLevel {
	case SemanticHigh:
		score += 24
	case SemanticMedium:
		score += 12
	default:
		score += 5
	}

	if capabilityHasDomain(cap, intent) {
		score += 18
	}

	if tool.ReadOnly {
		score += 8
	} else {
		score -= 6
	}

	switch cap.Cost {
	case CostLow:
		score += 4
	case CostHigh:
		score -= 10
	}

	switch cap.Risk {
	case RiskRead:
		score += 3
	case RiskWrite:
		score -= 2
	case RiskExecute:
		score -= 6
	}

	if input.HasImage {
		if isImageTool(tool.Name) {
			score += 40
		}
		if tool.Name == "web_fetch" {
			score -= 90
		}
	}

	if intent == IntentResearch {
		switch mode {
		case researchFocusDocs:
			switch {
			case isContext7Tool(tool.Name):
				score += 40
			case tool.Name == "doc_context":
				score += 24
			case tool.Name == "web_search":
				score += 18
			case tool.Name == "web_fetch":
				score += 4
			case isGitHubTool(tool.Name):
				score -= 10
			}
		case researchFocusGitHub:
			switch {
			case isGitHubTool(tool.Name):
				score += 38
			case tool.Name == "web_search":
				score += 18
			case tool.Name == "web_fetch":
				score += 8
			case isContext7Tool(tool.Name):
				score += 8
			case tool.Name == "doc_context":
				score -= 8
			}
		default:
			if tool.Name == "doc_context" || tool.Name == "web_search" || tool.Name == "web_fetch" || isContext7Tool(tool.Name) || isGitHubTool(tool.Name) {
				score += 16
			}
		}
	}

	if intent == IntentDebug && tool.Name == "bash" {
		score += 12
	}
	if intent == IntentSystem {
		if tool.Name == "bash" {
			score += 14
		}
		if tool.Name == "list_dir" {
			score -= 4
		}
	}

	return score
}

func preferredTools(intent Intent, mode researchFocus) []string {
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
		return preferredResearchTools(mode)
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

func inferResearchFocus(intent Intent, userText string) researchFocus {
	if intent != IntentResearch {
		return researchFocusGeneral
	}
	s := strings.ToLower(strings.TrimSpace(userText))
	if s == "" {
		return researchFocusGeneral
	}
	tokens := tokenize(s)

	if containsAny(s,
		"github.com/", "github ", "repo", "repository", "pull request", "issues", "stars",
		"仓库", "项目地址", "星标", "点赞", "release", "readme",
	) || containsTokenAny(tokens, "github", "repo", "repository", "star", "stars", "fork", "issue", "issues") {
		return researchFocusGitHub
	}
	if containsAny(s,
		"docs", "documentation", "official", "api", "sdk", "context7", "latest",
		"文档", "官方文档", "教程", "示例", "用法", "最新",
	) || containsTokenAny(tokens, "docs", "documentation", "official", "api", "sdk", "library", "context7") {
		return researchFocusDocs
	}
	return researchFocusGeneral
}

func preferredResearchTools(mode researchFocus) []string {
	switch mode {
	case researchFocusDocs:
		return []string{
			"mcp__context7__resolve-library-id",
			"mcp__context7__get-library-docs",
			"mcp__context7__query-docs",
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
	case researchFocusGitHub:
		return []string{
			"mcp__github__search_repositories",
			"mcp__github__get_file_contents",
			"mcp__github__search_code",
			"web_search",
			"web_fetch",
			"doc_context",
			"task",
			"repo_map",
			"symbol_nav",
			"read_file",
			"grep",
			"glob",
		}
	default:
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
	}
}
