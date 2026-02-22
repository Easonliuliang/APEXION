package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ArchitectStep describes a single step in an architect plan.
type ArchitectStep struct {
	Description string   `json:"description"`
	Files       []string `json:"files"`
	Action      string   `json:"action"` // "create" | "modify" | "delete" | "run"
	Details     string   `json:"details"`
}

// ArchitectPlan is the structured output from the architect model.
type ArchitectPlan struct {
	Summary string          `json:"summary"`
	Steps   []ArchitectStep `json:"steps"`
}

// ArchitectMode orchestrates a dual-model workflow:
// big model plans, small model executes.
type ArchitectMode struct {
	agent          *Agent
	architectModel string // big model for planning
	coderModel     string // small model for execution
	autoExecute    bool   // skip per-step confirmation
}

const architectSystemPrompt = `You are a senior software architect. Analyze the user's request and the codebase, then output a structured JSON plan.

Your plan MUST be valid JSON with this exact format:
{
  "summary": "Brief description of what will be done",
  "steps": [
    {
      "description": "What this step does",
      "files": ["path/to/file1.go", "path/to/file2.go"],
      "action": "modify",
      "details": "Specific instructions for the coder: what to change and how"
    }
  ]
}

Actions can be: "create" (new file), "modify" (edit existing), "delete" (remove file), "run" (execute a command).

Rules:
- Be thorough but concise. Each step should be independently actionable.
- Include file paths relative to the project root.
- For "modify" actions, describe exactly what to change (not just "update the file").
- For "run" actions, put the command in the details field.
- Order steps logically (create before use, modify before test).
- Output ONLY the JSON plan, no other text.`

// NewArchitectMode creates an ArchitectMode from configuration.
func NewArchitectMode(a *Agent, architectModel, coderModel string, autoExecute bool) *ArchitectMode {
	return &ArchitectMode{
		agent:          a,
		architectModel: architectModel,
		coderModel:     coderModel,
		autoExecute:    autoExecute,
	}
}

// Run executes the full architect workflow:
// 1. Send request to big model -> get structured plan
// 2. Display plan to user
// 3. Execute each step with small model code sub-agent
// 4. Verify after each step
func (am *ArchitectMode) Run(ctx context.Context, prompt string) error {
	a := am.agent

	// Phase 1: Get plan from architect model
	a.io.SystemMessage("Architect mode: analyzing request...")

	plan, err := am.getPlan(ctx, prompt)
	if err != nil {
		a.io.Error("Architect planning failed: " + err.Error())
		return nil
	}

	if len(plan.Steps) == 0 {
		a.io.SystemMessage("Architect produced an empty plan.")
		return nil
	}

	// Phase 2: Display plan
	a.io.SystemMessage(am.formatPlan(plan))

	if !am.autoExecute {
		a.io.SystemMessage("Execute this plan? (send any message to proceed, or /quit to cancel)")
		input, err := a.io.ReadInput()
		if err != nil {
			return nil
		}
		if strings.HasPrefix(input, "/") {
			a.io.SystemMessage("Plan cancelled.")
			return nil
		}
	}

	// Phase 3: Execute each step
	for i, step := range plan.Steps {
		a.io.SystemMessage(fmt.Sprintf("\n--- Step %d/%d: %s ---", i+1, len(plan.Steps), step.Description))

		if step.Action == "delete" {
			a.io.SystemMessage("Skipping delete step (requires manual confirmation).")
			continue
		}

		stepPrompt := am.buildStepPrompt(step)

		// Use code sub-agent with the coder model
		oldModel := a.config.SubAgentModel
		if am.coderModel != "" {
			a.config.SubAgentModel = am.coderModel
		}

		output, err := a.runSubAgent(ctx, stepPrompt, "code")

		// Restore model
		a.config.SubAgentModel = oldModel

		if err != nil {
			a.io.Error(fmt.Sprintf("Step %d failed: %v", i+1, err))
			if !am.autoExecute {
				a.io.SystemMessage("Continue with remaining steps? (send message to continue, /quit to stop)")
				input, err := a.io.ReadInput()
				if err != nil || strings.HasPrefix(input, "/") {
					a.io.SystemMessage("Remaining steps cancelled.")
					return nil
				}
			}
			continue
		}

		// Show step result (truncated)
		if output != "" {
			summary := output
			if len(summary) > 500 {
				summary = summary[:500] + "..."
			}
			a.io.SystemMessage("Step result: " + summary)
		}

		a.io.SystemMessage(fmt.Sprintf("Step %d/%d complete.", i+1, len(plan.Steps)))
	}

	a.io.SystemMessage("\nArchitect mode complete. All steps executed.")
	return nil
}

// getPlan sends the prompt to the architect model and parses the JSON plan.
func (am *ArchitectMode) getPlan(ctx context.Context, prompt string) (*ArchitectPlan, error) {
	a := am.agent

	// Temporarily switch to architect model if configured
	oldModel := a.config.Model
	if am.architectModel != "" {
		a.config.Model = am.architectModel
	}

	// Use explore sub-agent to generate the plan (read-only, returns text)
	planPrompt := fmt.Sprintf("%s\n\nUser request:\n%s\n\nExplore the codebase first to understand the current architecture, then output your JSON plan.", architectSystemPrompt, prompt)

	output, err := a.runSubAgent(ctx, planPrompt, "explore")

	// Restore model
	a.config.Model = oldModel

	if err != nil {
		return nil, err
	}

	// Parse JSON from the output (may be wrapped in markdown code blocks)
	jsonStr := extractJSON(output)
	if jsonStr == "" {
		return nil, fmt.Errorf("architect did not produce valid JSON plan")
	}

	var plan ArchitectPlan
	if err := json.Unmarshal([]byte(jsonStr), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse architect plan: %w", err)
	}

	return &plan, nil
}

// formatPlan renders the plan as human-readable text.
func (am *ArchitectMode) formatPlan(plan *ArchitectPlan) string {
	var sb strings.Builder
	sb.WriteString("\n=== Architect Plan ===\n")
	sb.WriteString(fmt.Sprintf("Summary: %s\n\n", plan.Summary))

	for i, step := range plan.Steps {
		sb.WriteString(fmt.Sprintf("  %d. [%s] %s\n", i+1, step.Action, step.Description))
		if len(step.Files) > 0 {
			sb.WriteString(fmt.Sprintf("     Files: %s\n", strings.Join(step.Files, ", ")))
		}
		if step.Details != "" {
			details := step.Details
			if len(details) > 200 {
				details = details[:200] + "..."
			}
			sb.WriteString(fmt.Sprintf("     Details: %s\n", details))
		}
	}
	sb.WriteString(fmt.Sprintf("\nTotal: %d steps", len(plan.Steps)))
	return sb.String()
}

// buildStepPrompt constructs a focused prompt for the coder sub-agent.
func (am *ArchitectMode) buildStepPrompt(step ArchitectStep) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Task: %s\n\n", step.Description))

	if len(step.Files) > 0 {
		sb.WriteString("Files to work on:\n")
		for _, f := range step.Files {
			sb.WriteString("  - " + f + "\n")
		}
		sb.WriteString("\n")
	}

	if step.Action == "run" {
		sb.WriteString(fmt.Sprintf("Run this command:\n%s\n", step.Details))
	} else if step.Details != "" {
		sb.WriteString(fmt.Sprintf("Instructions:\n%s\n", step.Details))
	}

	return sb.String()
}

// extractJSON finds a JSON object in text, handling markdown code blocks.
func extractJSON(text string) string {
	// Try to find JSON in code blocks first
	if idx := strings.Index(text, "```json"); idx >= 0 {
		start := idx + 7
		if end := strings.Index(text[start:], "```"); end >= 0 {
			return strings.TrimSpace(text[start : start+end])
		}
	}
	if idx := strings.Index(text, "```"); idx >= 0 {
		start := idx + 3
		// Skip language identifier on same line
		if nl := strings.Index(text[start:], "\n"); nl >= 0 {
			start += nl + 1
		}
		if end := strings.Index(text[start:], "```"); end >= 0 {
			candidate := strings.TrimSpace(text[start : start+end])
			if strings.HasPrefix(candidate, "{") {
				return candidate
			}
		}
	}

	// Try to find raw JSON object
	start := strings.Index(text, "{")
	if start < 0 {
		return ""
	}
	// Find matching closing brace (simple approach: find last })
	end := strings.LastIndex(text, "}")
	if end <= start {
		return ""
	}
	return text[start : end+1]
}
