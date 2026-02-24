package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/apexion-ai/apexion/internal/provider"
	"github.com/apexion-ai/apexion/internal/tools"
)

// executeToolWithRepair executes a tool call with optional name/arg repair and fallback chain.
func (a *Agent) executeToolWithRepair(ctx context.Context, call *provider.ToolCallRequest) (tools.ToolResult, string, []string) {
	executedName := call.Name
	input := call.Input
	notes := make([]string, 0, 3)

	enabled := a.config.ToolRouting.Enabled
	enableRepair := a.config.ToolRouting.EnableRepair
	enableFallback := a.config.ToolRouting.EnableFallback
	if !enabled {
		enableRepair = false
		enableFallback = false
	}

	res := a.executor.Execute(ctx, executedName, input)
	if !enableRepair && !enableFallback {
		return res, executedName, notes
	}

	// Name repair for unknown-tool errors.
	if res.IsError && isUnknownToolError(res.Content) && enableRepair {
		if repaired, ok := repairToolName(executedName, a.executor.Registry()); ok && repaired != executedName {
			repairedInput, changed := repairToolArgs(repaired, input)
			if changed {
				notes = append(notes, fmt.Sprintf("mapped tool name `%s` -> `%s` and adjusted args", executedName, repaired))
			} else {
				notes = append(notes, fmt.Sprintf("mapped tool name `%s` -> `%s`", executedName, repaired))
			}
			executedName = repaired
			input = repairedInput
			res = a.executor.Execute(ctx, executedName, input)
		}
	}

	// Arg repair for schema-style errors.
	if res.IsError && enableRepair && isParamError(res.Content) {
		repairedInput, changed := repairToolArgs(executedName, input)
		if changed {
			notes = append(notes, fmt.Sprintf("repaired arguments for `%s`", executedName))
			input = repairedInput
			res = a.executor.Execute(ctx, executedName, input)
		}
	}

	// Fallback chain.
	if res.IsError && enableFallback {
		for _, fb := range fallbackTools(executedName) {
			if _, ok := a.executor.Registry().Get(fb); !ok {
				continue
			}
			fbInput, _ := repairToolArgs(fb, input)
			next := a.executor.Execute(ctx, fb, fbInput)
			notes = append(notes, fmt.Sprintf("fallback `%s` -> `%s`", executedName, fb))
			if !next.IsError {
				executedName = fb
				res = next
				break
			}
		}
	}

	if len(notes) > 0 {
		prefix := "[Tool repair]\n- " + strings.Join(notes, "\n- ") + "\n\n"
		res.Content = prefix + res.Content
		if a.eventLogger != nil {
			a.eventLogger.Log(EventToolRepair, map[string]any{
				"tool_name":      call.Name,
				"executed_tool":  executedName,
				"repair_actions": notes,
				"is_error":       res.IsError,
			})
		}
	}

	return res, executedName, notes
}

func repairToolName(name string, registry *tools.Registry) (string, bool) {
	if _, ok := registry.Get(name); ok {
		return name, false
	}
	normalized := normalizeToolName(name)
	if _, ok := registry.Get(normalized); ok {
		return normalized, true
	}

	if mapped, ok := toolNameAliases()[normalized]; ok {
		if _, exists := registry.Get(mapped); exists {
			return mapped, true
		}
	}

	// Common MCP alias form: server/tool -> mcp__server__tool
	if strings.Count(normalized, "/") == 1 {
		candidate := "mcp__" + strings.ReplaceAll(normalized, "/", "__")
		if _, ok := registry.Get(candidate); ok {
			return candidate, true
		}
	}
	return name, false
}

func normalizeToolName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, ":", "_")
	s = strings.ReplaceAll(s, ".", "_")
	s = strings.Trim(s, "_")
	return s
}

func toolNameAliases() map[string]string {
	return map[string]string{
		"read":          "read_file",
		"view":          "read_file",
		"cat":           "read_file",
		"write":         "write_file",
		"create_file":   "write_file",
		"edit":          "edit_file",
		"patch":         "edit_file",
		"ls":            "list_dir",
		"list":          "list_dir",
		"search":        "grep",
		"grep_files":    "grep",
		"find_files":    "glob",
		"fetch":         "web_fetch",
		"webfetch":      "web_fetch",
		"websearch":     "web_search",
		"search_web":    "web_search",
		"repomap":       "repo_map",
		"repo_map_tool": "repo_map",
		"symbol":        "symbol_nav",
		"symbol_search": "symbol_nav",
		"symbol_lookup": "symbol_nav",
		"docs":          "doc_context",
		"doc_search":    "doc_context",
		"documentation": "doc_context",
		"gitstatus":     "git_status",
		"gitdiff":       "git_diff",
		"gitlog":        "git_log",
		"gitbranch":     "git_branch",
		"gitcommit":     "git_commit",
		"gitpush":       "git_push",
	}
}

func repairToolArgs(toolName string, raw json.RawMessage) (json.RawMessage, bool) {
	var m map[string]any
	if len(raw) == 0 {
		return raw, false
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return raw, false
	}
	if m == nil {
		m = map[string]any{}
	}
	changed := false
	rename := func(from, to string) {
		if from == to {
			return
		}
		v, ok := m[from]
		if !ok {
			return
		}
		if _, exists := m[to]; !exists {
			m[to] = v
			changed = true
		}
		delete(m, from)
		changed = true
	}

	switch toolName {
	case "read_file":
		rename("path", "file_path")
		rename("file", "file_path")
	case "write_file", "edit_file":
		rename("path", "file_path")
		rename("file", "file_path")
	case "glob":
		rename("file_pattern", "pattern")
		rename("dir", "path")
	case "grep":
		rename("query", "pattern")
		rename("q", "pattern")
		rename("dir", "path")
		rename("file_pattern", "glob")
		rename("ignore_case", "case_insensitive")
	case "list_dir":
		rename("dir", "path")
		rename("directory", "path")
	case "web_search":
		rename("q", "query")
		rename("query_text", "query")
		rename("num_results", "max_results")
		rename("results", "max_results")
	case "web_fetch":
		rename("link", "url")
		rename("uri", "url")
		rename("query", "prompt")
		rename("instruction", "prompt")
		if _, ok := m["prompt"]; !ok {
			m["prompt"] = "Extract the key information relevant to the user request."
			changed = true
		}
	case "repo_map":
		rename("dir", "path")
		rename("root", "path")
		rename("tokens", "max_tokens")
	case "symbol_nav":
		rename("name", "symbol")
		rename("query", "symbol")
		rename("dir", "path")
		if v, ok := m["references_only"].(bool); ok && v {
			if _, exists := m["mode"]; !exists {
				m["mode"] = "references"
				changed = true
			}
		}
	case "doc_context":
		rename("query", "topic")
		rename("q", "topic")
		rename("framework", "library")
		rename("package", "library")
		rename("pkg", "library")
		rename("ver", "version")
		rename("results", "max_results")
		rename("top_k", "fetch_top")
	}

	if !changed {
		return raw, false
	}
	fixed, err := json.Marshal(m)
	if err != nil {
		return raw, false
	}
	return fixed, true
}

func fallbackTools(toolName string) []string {
	switch toolName {
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

func isUnknownToolError(s string) bool {
	return strings.Contains(strings.ToLower(s), "unknown tool")
}

func isParamError(s string) bool {
	low := strings.ToLower(s)
	return strings.Contains(low, "invalid params") ||
		strings.Contains(low, "is required") ||
		strings.Contains(low, "invalid json")
}
