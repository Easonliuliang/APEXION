// Package provider 定义了所有 LLM provider 的统一接口和共享类型。
// 每个 provider adapter（openai.go, anthropic.go 等）实现 Provider 接口，
// 负责将各家 API 的 streaming 响应归一化为统一的 Event 序列。
package provider

import (
	"context"
	"encoding/json"
)

// ── 消息类型 ──────────────────────────────────────────────────────────────────

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type ContentType string

const (
	ContentTypeText       ContentType = "text"
	ContentTypeToolUse    ContentType = "tool_use"
	ContentTypeToolResult ContentType = "tool_result"
)

// Content 是消息中的一个内容块
type Content struct {
	Type       ContentType
	Text       string
	ToolUseID  string          // tool_use / tool_result
	ToolName   string          // tool_use
	ToolInput  json.RawMessage // tool_use
	ToolResult string          // tool_result
	IsError    bool            // tool_result
}

// Message 是对话历史中的一条消息
type Message struct {
	Role    Role
	Content []Content
}

// ── Tool Schema ───────────────────────────────────────────────────────────────

// ToolSchema 是发送给 LLM 的工具描述（JSON Schema 格式）
type ToolSchema struct {
	Name        string
	Description string
	Parameters  map[string]any // JSON Schema properties
}

// ── 请求类型 ──────────────────────────────────────────────────────────────────

// ChatRequest 是发送给 provider 的统一请求格式
type ChatRequest struct {
	Model        string
	Messages     []Message
	Tools        []ToolSchema
	SystemPrompt string
	MaxTokens    int
}

// ── 事件类型（streaming 输出）────────────────────────────────────────────────

type EventType int

const (
	// EventTextDelta: LLM 输出的文本增量，应实时渲染到终端
	EventTextDelta EventType = iota

	// EventToolCallDone: 一个完整的 tool call（provider 内部完成 JSON 拼接后发出）
	EventToolCallDone

	// EventDone: 本轮消息结束，附带 token 用量
	EventDone

	// EventError: 发生错误
	EventError
)

// Event 是 provider streaming 输出的统一事件
type Event struct {
	Type EventType

	// EventTextDelta
	TextDelta string

	// EventToolCallDone
	ToolCall *ToolCallRequest

	// EventDone
	Usage *Usage

	// EventError
	Error error
}

// ToolCallRequest 代表 LLM 请求执行的一个工具调用
type ToolCallRequest struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// Usage 记录本次 API 调用的 token 消耗
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// ── Provider 接口 ─────────────────────────────────────────────────────────────

// Provider 是所有 LLM provider 的统一接口。
// 实现者负责：
// 1. 将统一 ChatRequest 转换为该 provider 的 API 请求格式
// 2. 将该 provider 的 streaming 响应转换为统一 Event 序列
// 3. 内部处理 streaming tool use 的 JSON 片段拼接（状态机）
// 4. 处理该 provider 特有的错误码和重试逻辑
type Provider interface {
	// Chat 发起 streaming 对话。
	// 返回的 channel 会持续发出 Event，直到 EventDone 或 EventError 后关闭。
	// 调用方必须消费完 channel，否则会导致 goroutine 泄漏。
	Chat(ctx context.Context, req *ChatRequest) (<-chan Event, error)

	// Name 返回 provider 标识符，如 "anthropic", "openai", "deepseek"
	Name() string

	// Models 返回支持的模型列表
	Models() []string

	// DefaultModel 返回默认模型
	DefaultModel() string
}
