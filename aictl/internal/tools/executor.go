package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
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
	defaultTimeout time.Duration
	maxOutputBytes int
	autoApprove    bool
	autoApproveSet map[string]bool
}

// NewExecutor 创建工具执行器
func NewExecutor(registry *Registry, autoApprove bool, autoApproveTools []string) *Executor {
	approveSet := make(map[string]bool, len(autoApproveTools))
	for _, name := range autoApproveTools {
		approveSet[name] = true
	}
	return &Executor{
		registry:       registry,
		defaultTimeout: 30 * time.Second,
		maxOutputBytes: 100 * 1024,
		autoApprove:    autoApprove,
		autoApproveSet: approveSet,
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

	// 权限检查
	if !tool.IsReadOnly() && !e.autoApprove && !e.autoApproveSet[name] {
		if e.confirmer != nil {
			if !e.confirmer.Confirm(name, string(params), tool.PermissionLevel()) {
				return ToolResult{Content: "tool execution cancelled by user", IsError: true}
			}
		}
	}

	ctx, cancel := context.WithTimeout(ctx, e.defaultTimeout)
	defer cancel()

	result, err := tool.Execute(ctx, params)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}

	if len(result.Content) > e.maxOutputBytes {
		result.Content = result.Content[:e.maxOutputBytes] + "\n[Truncated: output too large]"
		result.Truncated = true
	}

	return result
}
