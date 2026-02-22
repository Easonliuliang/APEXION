// Package tools defines the tool interface and shared types,
// and provides the tool registry and executor.
package tools

import (
	"context"
	"encoding/json"
)

// PermissionLevel defines the permission level for a tool operation.
type PermissionLevel int

const (
	PermissionRead      PermissionLevel = iota // read-only: auto-allow
	PermissionWrite                             // write: prompt by default
	PermissionExecute                           // execute: prompt by default (shows command)
	PermissionDangerous                         // dangerous: force confirmation (prominent warning)
)

// ToolResult is the result of a tool execution.
type ToolResult struct {
	Content       string // primary output content
	IsError       bool   // whether this is an error result
	Truncated     bool   // whether content was truncated
	UserCancelled bool   // user interrupted (Esc), should stop agent loop
}

// Tool is the unified interface for all tools callable by the LLM.
type Tool interface {
	// Name returns the tool name (snake_case), e.g. "read_file".
	// This is the name the LLM uses to invoke the tool; must be unique.
	Name() string

	// Description returns the tool description sent to the LLM.
	// Should be detailed enough to help the LLM decide when to use this tool.
	Description() string

	// Parameters returns JSON Schema parameter definitions (properties section).
	Parameters() map[string]any

	// Execute runs the tool.
	// ctx comes from the agent loop and can be cancelled by user Ctrl+C.
	// params are the tool call arguments provided by the LLM (validated JSON).
	Execute(ctx context.Context, params json.RawMessage) (ToolResult, error)

	// IsReadOnly indicates whether this tool is a read-only operation.
	// Read-only tools are auto-allowed and may run in parallel.
	IsReadOnly() bool

	// PermissionLevel returns the permission level required by this tool.
	PermissionLevel() PermissionLevel
}
