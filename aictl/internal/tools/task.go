package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	taskTimeout       = 120 * time.Second // max time for a sub-agent run
	taskOutputMaxLen  = 24 * 1024         // 24KB max output returned to main LLM
)

// SubAgentRunner is a function that runs a sub-agent with the given prompt
// and returns its text output. Injected by the agent package to avoid
// circular imports.
type SubAgentRunner func(ctx context.Context, prompt string) (string, error)

// TaskTool spawns a read-only sub-agent to perform research tasks
// (searching code, reading files, exploring the codebase) without
// polluting the main conversation context.
type TaskTool struct {
	runner SubAgentRunner
}

func (t *TaskTool) Name() string                     { return "task" }
func (t *TaskTool) IsReadOnly() bool                 { return true }
func (t *TaskTool) PermissionLevel() PermissionLevel { return PermissionRead }

func (t *TaskTool) Description() string {
	return `Launch a sub-agent to perform a research or exploration task.
The sub-agent has access to read-only tools (read_file, glob, grep, list_dir, web_fetch, todo_read).
It CANNOT modify files, run commands, or make git changes.

Use this tool when:
- You need to search or explore the codebase without cluttering the main conversation
- You want to research multiple files or patterns in parallel (call task multiple times)
- The search may produce large results that would waste main context space

The sub-agent receives your prompt, works autonomously, and returns a text summary.
Keep prompts specific and focused for best results.`
}

func (t *TaskTool) Parameters() map[string]any {
	return map[string]any{
		"prompt": map[string]any{
			"type":        "string",
			"description": "A clear, specific task description for the sub-agent. Include what to search for, which files/patterns to look at, and what information to return.",
		},
	}
}

// SetRunner injects the sub-agent runner function.
// Must be called before Execute (typically by the agent package during init).
func (t *TaskTool) SetRunner(fn SubAgentRunner) {
	t.runner = fn
}

func (t *TaskTool) Execute(ctx context.Context, params json.RawMessage) (ToolResult, error) {
	var p struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("invalid params: %w", err)
	}
	if p.Prompt == "" {
		return ToolResult{}, fmt.Errorf("prompt is required")
	}

	if t.runner == nil {
		return ToolResult{
			Content: "Sub-agent not available (runner not configured)",
			IsError: true,
		}, nil
	}

	// Run with a dedicated timeout.
	taskCtx, cancel := context.WithTimeout(ctx, taskTimeout)
	defer cancel()

	output, err := t.runner(taskCtx, p.Prompt)
	if err != nil {
		if taskCtx.Err() == context.DeadlineExceeded {
			// Return partial output on timeout instead of failing completely.
			if output != "" {
				return ToolResult{
					Content: truncateTaskOutput(output) + "\n\n[Sub-agent timed out, partial results above]",
				}, nil
			}
			return ToolResult{
				Content: "Sub-agent timed out with no output",
				IsError: true,
			}, nil
		}
		if ctx.Err() != nil {
			return ToolResult{}, fmt.Errorf("cancelled")
		}
		return ToolResult{
			Content: fmt.Sprintf("Sub-agent error: %v", err),
			IsError: true,
		}, nil
	}

	if strings.TrimSpace(output) == "" {
		return ToolResult{Content: "Sub-agent returned no output."}, nil
	}

	return ToolResult{Content: truncateTaskOutput(output)}, nil
}

// truncateTaskOutput limits sub-agent output to avoid blowing up main context.
func truncateTaskOutput(s string) string {
	if len(s) <= taskOutputMaxLen {
		return s
	}
	return s[:taskOutputMaxLen] + "\n\n[Output truncated to 24KB]"
}
