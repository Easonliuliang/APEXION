package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aictl/aictl/internal/permission"
)

// Confirmer is a minimal interface the Executor uses for permission prompts.
// This avoids a circular import with the tui package.
type Confirmer interface {
	Confirm(name, params string, level PermissionLevel) bool
}

// Executor 负责执行工具调用，包含权限检查和超时控制
type Executor struct {
	registry       *Registry
	confirmer      Confirmer
	policy         permission.Policy
	defaultTimeout time.Duration
}

// NewExecutor 创建工具执行器
func NewExecutor(registry *Registry, policy permission.Policy) *Executor {
	return &Executor{
		registry:       registry,
		policy:         policy,
		defaultTimeout: 30 * time.Second,
	}
}

// SetConfirmer injects the UI-layer confirmer (called after New to avoid
// circular dependencies between agent, tui, and tools packages).
func (e *Executor) SetConfirmer(c Confirmer) {
	e.confirmer = c
}

// Registry returns the underlying tool registry.
func (e *Executor) Registry() *Registry {
	return e.registry
}

// Execute 执行单个工具调用
func (e *Executor) Execute(ctx context.Context, name string, params json.RawMessage) ToolResult {
	tool, ok := e.registry.Get(name)
	if !ok {
		return ToolResult{Content: fmt.Sprintf("unknown tool: %s", name), IsError: true}
	}

	// Permission check via policy.
	decision := e.policy.Check(name, params)
	switch decision {
	case permission.Deny:
		return ToolResult{Content: "tool execution denied by policy", IsError: true}
	case permission.NeedConfirmation:
		if e.confirmer != nil {
			if !e.confirmer.Confirm(name, string(params), tool.PermissionLevel()) {
				return ToolResult{Content: "tool execution cancelled by user", IsError: true}
			}
		}
	case permission.Allow:
		// proceed
	}

	ctx, cancel := context.WithTimeout(ctx, e.defaultTimeout)
	defer cancel()

	result, err := tool.Execute(ctx, params)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}

	limit := toolOutputLimit(name)
	if len(result.Content) > limit {
		result.Content = truncateHeadTail(result.Content, limit)
		result.Truncated = true
	}

	return result
}

// toolOutputLimit returns the output byte limit for a given tool.
func toolOutputLimit(name string) int {
	switch name {
	case "read_file", "grep", "bash":
		return 32 * 1024 // 32KB ~8K tokens
	case "git_diff", "git_status", "list_dir", "glob":
		return 16 * 1024 // 16KB
	default: // edit_file, write_file, git_commit, git_push, etc.
		return 4 * 1024 // 4KB
	}
}

// truncateHeadTail keeps the head (60%) and tail (40%) of a string,
// omitting the middle. Tail content (errors, final results) is often more important.
func truncateHeadTail(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	head := maxLen * 3 / 5 // 60%
	tail := maxLen * 2 / 5 // 40%
	omitted := len(s) - head - tail
	return s[:head] + fmt.Sprintf("\n\n[...%d chars omitted...]\n\n", omitted) + s[len(s)-tail:]
}
