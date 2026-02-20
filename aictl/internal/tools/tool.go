// Package tools 定义了工具接口和共享类型，
// 并提供工具注册表和执行器。
package tools

import (
	"context"
	"encoding/json"
)

// PermissionLevel 定义工具操作的权限级别
type PermissionLevel int

const (
	PermissionRead      PermissionLevel = iota // 只读：自动允许
	PermissionWrite                             // 写入：默认询问
	PermissionExecute                           // 执行：默认询问（显示具体命令）
	PermissionDangerous                         // 危险：强制确认（醒目警告）
)

// ToolResult 是工具执行的结果
type ToolResult struct {
	Content       string // 主要输出内容
	IsError       bool   // 是否为错误结果
	Truncated     bool   // 内容是否被截断
	UserCancelled bool   // 用户主动中断（Esc），应停止 agent loop
}

// Tool 是所有可被 LLM 调用的工具的统一接口
type Tool interface {
	// Name 返回工具名称（snake_case），如 "read_file"
	// 这是 LLM 调用时使用的名字，必须唯一
	Name() string

	// Description 返回给 LLM 的工具描述
	// 应尽量详细，帮助 LLM 判断何时使用该工具
	Description() string

	// Parameters 返回 JSON Schema 格式的参数定义（properties 部分）
	Parameters() map[string]any

	// Execute 执行工具
	// ctx 来自上层 agent loop，可被用户 Ctrl+C 取消
	// params 是 LLM 提供的工具调用参数（已验证为合法 JSON）
	Execute(ctx context.Context, params json.RawMessage) (ToolResult, error)

	// IsReadOnly 标记该工具是否为只读操作
	// 只读工具：自动允许执行，多个只读工具可并行执行
	IsReadOnly() bool

	// PermissionLevel 返回该工具所需的权限级别
	PermissionLevel() PermissionLevel
}
