package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// Questioner asks the user a question with predefined options.
// Implemented by the TUI layer and injected via SetQuestioner.
type Questioner interface {
	// AskQuestion displays a question with options and blocks until the user answers.
	// Returns the user's chosen option text or custom input.
	AskQuestion(question string, options []string) (string, error)
}

// QuestionTool allows the LLM to ask the user a clarifying question
// with predefined options. The user can select an option or type a
// custom answer.
type QuestionTool struct {
	questioner Questioner
}

func (t *QuestionTool) Name() string                     { return "question" }
func (t *QuestionTool) IsReadOnly() bool                 { return true }
func (t *QuestionTool) PermissionLevel() PermissionLevel { return PermissionRead }

func (t *QuestionTool) Description() string {
	return `Ask the user a question when you encounter ambiguity or need a decision.
Provide a clear question and 2-4 options. The user can select an option or type a custom answer.

Use this tool when:
- A task has multiple valid approaches and you need the user to choose
- Requirements are unclear and guessing would waste effort
- You need confirmation on a destructive or irreversible action

Do NOT use this tool for:
- Questions you can answer yourself by reading code or files
- Trivial decisions that don't meaningfully affect the outcome`
}

func (t *QuestionTool) Parameters() map[string]any {
	return map[string]any{
		"question": map[string]any{
			"type":        "string",
			"description": "A clear, specific question for the user",
		},
		"options": map[string]any{
			"type":        "array",
			"description": "2-4 predefined answer options",
			"items": map[string]any{
				"type": "string",
			},
		},
	}
}

// SetQuestioner injects the UI-layer questioner.
func (t *QuestionTool) SetQuestioner(q Questioner) {
	t.questioner = q
}

func (t *QuestionTool) Execute(_ context.Context, params json.RawMessage) (ToolResult, error) {
	var p struct {
		Question string   `json:"question"`
		Options  []string `json:"options"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("invalid params: %w", err)
	}
	if p.Question == "" {
		return ToolResult{}, fmt.Errorf("question is required")
	}
	if len(p.Options) < 2 {
		return ToolResult{}, fmt.Errorf("at least 2 options are required")
	}

	if t.questioner == nil {
		return ToolResult{
			Content: "Question tool not available (no UI questioner configured)",
			IsError: true,
		}, nil
	}

	answer, err := t.questioner.AskQuestion(p.Question, p.Options)
	if err != nil {
		return ToolResult{
			Content:       "Interrupted",
			IsError:       true,
			UserCancelled: true,
		}, nil
	}

	return ToolResult{
		Content: fmt.Sprintf("User answered: %s", answer),
	}, nil
}
