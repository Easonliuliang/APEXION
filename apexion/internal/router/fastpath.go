package router

import (
	"encoding/json"
	"regexp"
	"strings"
)

var (
	reSymbolWhere = regexp.MustCompile(`(?i)find\s+where\s+([A-Za-z_][A-Za-z0-9_]*)\s+(is\s+)?(defined|used|called|referenced)`)
	reSymbolCall  = regexp.MustCompile(`(?i)trace\s+call\s+chain\s+for\s+([A-Za-z_][A-Za-z0-9_]*)`)
	reSymbolCN    = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\s*(?:在哪里定义|在哪定义|被哪些地方调用|被哪里调用|调用链)`)
)

func buildDeterministicFastpath(
	input PlanInput,
	intent Intent,
	primary []PlannedTool,
	capabilityByTool map[string]ToolCapability,
	confidenceThreshold float64,
) *FastPathPlan {
	if input.HasImage {
		return nil
	}
	if confidenceThreshold <= 0 {
		confidenceThreshold = 0.85
	}

	primarySet := make(map[string]bool, len(primary))
	for _, p := range primary {
		primarySet[p.Name] = true
	}

	lower := strings.ToLower(strings.TrimSpace(input.UserText))

	if (intent == IntentCodebase || intent == IntentDebug) &&
		primarySet["symbol_nav"] &&
		capabilitySupportsTask(capabilityByTool["symbol_nav"], "symbol_lookup") {
		if symbol := extractSymbolLookupTarget(input.UserText); symbol != "" {
			mode := inferSymbolLookupMode(lower)
			conf := 0.90
			if mode == "both" {
				conf = 0.92
			}
			if conf >= confidenceThreshold {
				payload, _ := json.Marshal(map[string]any{
					"symbol": symbol,
					"path":   ".",
					"mode":   mode,
				})
				return &FastPathPlan{
					Tool:       "symbol_nav",
					Task:       "symbol_lookup",
					InputJSON:  string(payload),
					Confidence: conf,
				}
			}
		}
	}

	if intent == IntentCodebase &&
		primarySet["repo_map"] &&
		capabilitySupportsTask(capabilityByTool["repo_map"], "repo_overview") &&
		isRepoOverviewQuery(lower) {
		conf := 0.90
		if strings.Contains(lower, "quick") || strings.Contains(lower, "快速") {
			conf = 0.93
		}
		if conf >= confidenceThreshold {
			payload, _ := json.Marshal(map[string]any{
				"path": ".",
			})
			return &FastPathPlan{
				Tool:       "repo_map",
				Task:       "repo_overview",
				InputJSON:  string(payload),
				Confidence: conf,
			}
		}
	}

	return nil
}

func capabilitySupportsTask(cap ToolCapability, task string) bool {
	for _, t := range cap.DeterministicFor {
		if t == task {
			return true
		}
	}
	return false
}

func extractSymbolLookupTarget(raw string) string {
	s := strings.TrimSpace(raw)
	for _, re := range []*regexp.Regexp{reSymbolWhere, reSymbolCall, reSymbolCN} {
		if m := re.FindStringSubmatch(s); len(m) >= 2 {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

func inferSymbolLookupMode(lower string) string {
	hasDef := strings.Contains(lower, "defined") || strings.Contains(lower, "definition") ||
		strings.Contains(lower, "实现") || strings.Contains(lower, "定义")
	hasRef := strings.Contains(lower, "used") || strings.Contains(lower, "called") || strings.Contains(lower, "reference") ||
		strings.Contains(lower, "调用") || strings.Contains(lower, "引用")

	switch {
	case hasDef && hasRef:
		return "both"
	case hasRef:
		return "references"
	case hasDef:
		return "definitions"
	default:
		return "both"
	}
}

func isRepoOverviewQuery(lower string) bool {
	return containsAny(lower,
		"repository architecture", "repo architecture", "project architecture", "key modules",
		"main entrypoint", "startup flow", "module map", "repo overview",
		"仓库架构", "项目架构", "关键模块", "入口", "启动流程", "仓库结构", "项目结构",
	)
}
