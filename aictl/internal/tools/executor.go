package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Executor 负责执行工具调用，包含权限检查和超时控制
type Executor struct {
	registry       *Registry
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

	// 权限检查：只读工具自动通过，其他需要确认
	if !tool.IsReadOnly() && !e.autoApprove && !e.autoApproveSet[name] {
		if !e.confirmWithUser(name, string(params)) {
			return ToolResult{Content: "tool execution cancelled by user", IsError: true}
		}
	}

	// 超时控制
	ctx, cancel := context.WithTimeout(ctx, e.defaultTimeout)
	defer cancel()

	result, err := tool.Execute(ctx, params)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}

	// 输出截断
	if len(result.Content) > e.maxOutputBytes {
		result.Content = result.Content[:e.maxOutputBytes] + "\n[Truncated: output too large]"
		result.Truncated = true
	}

	return result
}

// confirmWithUser 在终端询问用户确认
func (e *Executor) confirmWithUser(toolName, params string) bool {
	// 截断过长的参数显示
	display := params
	if len(display) > 200 {
		display = display[:200] + "..."
	}
	fmt.Printf("\n--- Tool: %s ---\n%s\n[y/N] ", toolName, display)
	var answer string
	fmt.Scanln(&answer)
	return strings.ToLower(strings.TrimSpace(answer)) == "y"
}
