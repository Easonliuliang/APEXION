// Package tui defines the IO interface between the agent loop and the
// user interface layer, plus PlainIO (terminal fallback) and TuiIO (bubbletea).
package tui

import "github.com/apexion-ai/apexion/internal/tools"

// IO is the contract between the agent loop and the UI layer.
// Every method maps to a distinct visual event — this separation ensures
// the agent loop never depends on any specific rendering implementation.
type IO interface {
	// ReadInput blocks until the user submits a line of input.
	// Returns ("", io.EOF) when the user quits.
	ReadInput() (string, error)

	// UserMessage displays the user's submitted message in the output area.
	UserMessage(text string)

	// ThinkingStart signals that the LLM has started processing.
	// Implementations should show a spinner or "Thinking..." indicator.
	ThinkingStart()

	// TextDelta appends an incremental text chunk from the LLM stream.
	TextDelta(delta string)

	// TextDone signals that the current LLM response is complete.
	// fullText contains the entire response assembled from all deltas.
	// TUI implementations use this to trigger Markdown rendering.
	TextDone(fullText string)

	// ToolStart signals that a tool call has begun.
	// id uniquely identifies this call (for correlation with ToolDone).
	ToolStart(id, name, params string)

	// ToolDone signals that a tool call has completed.
	// id matches the id passed to ToolStart.
	ToolDone(id, name, result string, isErr bool)

	// Confirm asks the user whether to allow a tool execution.
	// Returns true if the user approves, false to cancel.
	// level controls the visual urgency (Write vs Dangerous).
	Confirm(name, params string, level tools.PermissionLevel) bool

	// SystemMessage displays a system-level notice (e.g. "/clear" feedback,
	// max-iteration warnings, session status).
	SystemMessage(text string)

	// Error displays an error message with prominent styling.
	Error(msg string)

	// SetTokens updates the token counter shown in the status area.
	SetTokens(n int)

	// SetContextInfo updates the context window usage indicator.
	// used is the number of prompt tokens from the last API call,
	// total is the context window size.
	SetContextInfo(used, total int)

	// SetPlanMode updates the plan mode indicator in the status bar.
	SetPlanMode(active bool)

	// SetCost updates the dollar cost shown in the status bar.
	SetCost(cost float64)
}
