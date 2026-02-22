// Package provider defines the unified interface and shared types for all LLM providers.
// Each provider adapter (openai.go, anthropic.go, etc.) implements the Provider interface,
// normalizing vendor-specific streaming responses into a unified Event sequence.
package provider

import (
	"context"
	"encoding/json"
)

// ── Message types ────────────────────────────────────────────────────────────

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
	ContentTypeImage      ContentType = "image"
)

// Content is a single content block within a message.
type Content struct {
	Type           ContentType
	Text           string
	ToolUseID      string          // tool_use / tool_result
	ToolName       string          // tool_use
	ToolInput      json.RawMessage // tool_use
	ToolResult     string          // tool_result
	IsError        bool            // tool_result
	ImageData      string          // image: base64-encoded data
	ImageMediaType string          // image: MIME type (e.g. "image/png")
}

// Message is a single message in the conversation history.
type Message struct {
	Role    Role
	Content []Content
}

// ── Tool Schema ───────────────────────────────────────────────────────────────

// ToolSchema describes a tool sent to the LLM (JSON Schema format).
type ToolSchema struct {
	Name        string
	Description string
	Parameters  map[string]any // JSON Schema properties
}

// ── Request types ────────────────────────────────────────────────────────────

// ChatRequest is the unified request format sent to a provider.
type ChatRequest struct {
	Model        string
	Messages     []Message
	Tools        []ToolSchema
	SystemPrompt string
	MaxTokens    int
}

// ── Event types (streaming output) ───────────────────────────────────────────

type EventType int

const (
	// EventTextDelta: incremental text output from the LLM, rendered in real time.
	EventTextDelta EventType = iota

	// EventToolCallDone: a complete tool call (emitted after internal JSON assembly).
	EventToolCallDone

	// EventDone: end of this message turn, includes token usage.
	EventDone

	// EventError: an error occurred.
	EventError
)

// Event is the unified streaming event emitted by a provider.
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

// ToolCallRequest represents a tool call requested by the LLM.
type ToolCallRequest struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// Usage records token consumption for an API call.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// ── Provider interface ───────────────────────────────────────────────────────

// Provider is the unified interface for all LLM providers.
// Implementors are responsible for:
// 1. Converting the unified ChatRequest into the provider's API request format
// 2. Converting the provider's streaming response into a unified Event sequence
// 3. Internally assembling streaming tool-use JSON fragments (state machine)
// 4. Handling provider-specific error codes and retry logic
type Provider interface {
	// Chat initiates a streaming conversation.
	// The returned channel emits Events until EventDone or EventError, then closes.
	// The caller must fully consume the channel to avoid goroutine leaks.
	Chat(ctx context.Context, req *ChatRequest) (<-chan Event, error)

	// Name returns the provider identifier, e.g. "anthropic", "openai", "deepseek".
	Name() string

	// Models returns the list of supported models.
	Models() []string

	// DefaultModel returns the default model.
	DefaultModel() string

	// ContextWindow returns the default context window size for the current model.
	ContextWindow() int
}
