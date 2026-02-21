package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// TodoItem represents a single task in the todo list.
type TodoItem struct {
	ID     int    `json:"id"`
	Task   string `json:"task"`
	Status string `json:"status"` // "pending", "in_progress", "completed"
}

// todoStore is a process-level in-memory todo list shared across tool calls.
// This is intentionally a package-level global: each aictl process runs a
// single session, so there is no need for per-session isolation. The mutex
// protects concurrent access from parallel tool calls within the same turn.
var (
	todoItems []TodoItem
	todoMu    sync.Mutex
)

// ResetTodoState clears the global todo list. Intended for use in tests
// to ensure isolation between test cases.
func ResetTodoState() {
	todoMu.Lock()
	defer todoMu.Unlock()
	todoItems = nil
}

// ---------- todo_write ----------

// TodoWriteTool replaces the entire todo list with new items.
type TodoWriteTool struct{}

func (t *TodoWriteTool) Name() string                     { return "todo_write" }
func (t *TodoWriteTool) IsReadOnly() bool                 { return false }
func (t *TodoWriteTool) PermissionLevel() PermissionLevel { return PermissionRead } // no real side effect

func (t *TodoWriteTool) Description() string {
	return `Create or update a todo list to track multi-step tasks.
Accepts the full list of items (replaces any existing list).
Each item has: task (description), status ("pending", "in_progress", or "completed").
Use this to plan work, track progress, and avoid forgetting steps in long conversations.`
}

func (t *TodoWriteTool) Parameters() map[string]any {
	return map[string]any{
		"items": map[string]any{
			"type":        "array",
			"description": "The complete list of todo items (replaces existing list)",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task": map[string]any{
						"type":        "string",
						"description": "Description of the task",
					},
					"status": map[string]any{
						"type":        "string",
						"description": "Task status: pending, in_progress, or completed",
						"enum":        []string{"pending", "in_progress", "completed"},
					},
				},
			},
		},
	}
}

func (t *TodoWriteTool) Execute(_ context.Context, params json.RawMessage) (ToolResult, error) {
	var p struct {
		Items []struct {
			Task   string `json:"task"`
			Status string `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("invalid params: %w", err)
	}

	todoMu.Lock()
	defer todoMu.Unlock()

	todoItems = make([]TodoItem, len(p.Items))
	for i, item := range p.Items {
		status := item.Status
		if status == "" {
			status = "pending"
		}
		todoItems[i] = TodoItem{
			ID:     i + 1,
			Task:   item.Task,
			Status: status,
		}
	}

	return ToolResult{Content: fmt.Sprintf("Todo list updated: %d items", len(todoItems))}, nil
}

// ---------- todo_read ----------

// TodoReadTool returns the current todo list.
type TodoReadTool struct{}

func (t *TodoReadTool) Name() string                     { return "todo_read" }
func (t *TodoReadTool) IsReadOnly() bool                 { return true }
func (t *TodoReadTool) PermissionLevel() PermissionLevel { return PermissionRead }

func (t *TodoReadTool) Description() string {
	return "Read the current todo list. Returns all items with their status. Use this to check progress before continuing work."
}

func (t *TodoReadTool) Parameters() map[string]any {
	return map[string]any{} // no parameters
}

func (t *TodoReadTool) Execute(_ context.Context, _ json.RawMessage) (ToolResult, error) {
	todoMu.Lock()
	defer todoMu.Unlock()

	if len(todoItems) == 0 {
		return ToolResult{Content: "No todo items."}, nil
	}

	var sb strings.Builder
	pending, inProgress, completed := 0, 0, 0
	for _, item := range todoItems {
		icon := "○"
		switch item.Status {
		case "in_progress":
			icon = "◐"
			inProgress++
		case "completed":
			icon = "●"
			completed++
		default:
			pending++
		}
		fmt.Fprintf(&sb, "%s [%d] %s\n", icon, item.ID, item.Task)
	}
	total := len(todoItems)
	fmt.Fprintf(&sb, "\nProgress: %d/%d completed", completed, total)
	if inProgress > 0 {
		fmt.Fprintf(&sb, ", %d in progress", inProgress)
	}
	if pending > 0 {
		fmt.Fprintf(&sb, ", %d pending", pending)
	}

	return ToolResult{Content: sb.String()}, nil
}
