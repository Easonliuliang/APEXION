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
// and mode, and returns its text output. Injected by the agent package to
// avoid circular imports.
// mode is "explore" (default) or "plan".
type SubAgentRunner func(ctx context.Context, prompt string, mode string) (string, error)

// TaskTool spawns a sub-agent to perform tasks without polluting
// the main conversation context. Supports explore, plan, and code modes.
type TaskTool struct {
	runner    SubAgentRunner
	confirmer Confirmer // injected for code mode confirmation
}

func (t *TaskTool) Name() string     { return "task" }
func (t *TaskTool) IsReadOnly() bool { return true } // task itself is read-only; code mode sub-agent gets its own executor
func (t *TaskTool) PermissionLevel() PermissionLevel { return PermissionRead }

func (t *TaskTool) Description() string {
	return `Launch a sub-agent to perform a task autonomously.
Modes:
- 'explore' (default): Read-only research. Has read_file, glob, grep, list_dir, web_fetch, todo_read, git_log.
- 'plan': Read-only analysis. Same tools as explore, outputs a structured implementation plan.
- 'code': Full coding agent. Can read/write/edit files, run bash commands, and use git tools.
  WARNING: code mode can modify your files. You will be asked for confirmation before it starts.

Use this tool when:
- You need to search or explore the codebase without cluttering the main conversation
- You want to research multiple files or patterns in parallel (call task multiple times)
- You need a focused coding task done independently (use mode="code")

The sub-agent receives your prompt, works autonomously, and returns a text summary.
Keep prompts specific and focused for best results.`
}

func (t *TaskTool) Parameters() map[string]any {
	return map[string]any{
		"prompt": map[string]any{
			"type":        "string",
			"description": "A clear, specific task description for the sub-agent. Include what to search for, which files/patterns to look at, and what information to return.",
		},
		"mode": map[string]any{
			"type":        "string",
			"description": "Sub-agent mode: 'explore' (read-only research, default), 'plan' (read-only, structured plan), 'code' (can modify files and run commands).",
			"enum":        []string{"explore", "plan", "code"},
		},
	}
}

// SetRunner injects the sub-agent runner function.
// Must be called before Execute (typically by the agent package during init).
func (t *TaskTool) SetRunner(fn SubAgentRunner) {
	t.runner = fn
}

// SetConfirmer injects the confirmer for code mode confirmation.
func (t *TaskTool) SetConfirmer(c Confirmer) {
	t.confirmer = c
}

func (t *TaskTool) Execute(ctx context.Context, params json.RawMessage) (ToolResult, error) {
	var p struct {
		Prompt string `json:"prompt"`
		Mode   string `json:"mode"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("invalid params: %w", err)
	}
	if p.Prompt == "" {
		return ToolResult{}, fmt.Errorf("prompt is required")
	}
	if p.Mode == "" {
		p.Mode = "explore"
	}

	// Code mode requires user confirmation since the sub-agent can modify files.
	if p.Mode == "code" && t.confirmer != nil {
		if !t.confirmer.Confirm("task (code mode)",
			fmt.Sprintf("Code sub-agent will be launched with write permissions.\nTask: %s", truncateTaskOutput(p.Prompt)),
			PermissionWrite) {
			return ToolResult{
				Content:       "[User cancelled code sub-agent]",
				IsError:       false,
				UserCancelled: true,
			}, nil
		}
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

	output, err := t.runner(taskCtx, p.Prompt, p.Mode)
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
