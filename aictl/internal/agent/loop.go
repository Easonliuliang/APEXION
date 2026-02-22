package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/aictl/aictl/internal/provider"
	"github.com/aictl/aictl/internal/session"
	"github.com/aictl/aictl/internal/tools"
)

// runAgentLoop executes the core agentic loop:
//  1. Send messages to the LLM via streaming Chat()
//  2. Collect text deltas (stream to UI) and tool calls
//  3. If tool calls exist, execute them, append results to history, and loop
//  4. If no tool calls, return (wait for next user input)
//
// A per-turn child context is created so that Esc can cancel the entire turn
// (including LLM streaming) without affecting the session-level context.
func (a *Agent) runAgentLoop(ctx context.Context) error {
	maxIter := a.config.MaxIterations // 0 = unlimited

	// Per-turn context: Esc cancels this, not the session.
	turnCtx, turnCancel := context.WithCancel(ctx)
	defer turnCancel()

	// Register the turn cancel with the UI so Esc can trigger it.
	if lc, ok := a.io.(tools.LoopCanceller); ok {
		lc.SetLoopCancel(turnCancel)
		defer lc.ClearLoopCancel()
	}

	// Compute token budget.
	contextWindow := a.config.ContextWindow
	if contextWindow <= 0 {
		contextWindow = a.provider.ContextWindow()
	}
	budget := session.NewTokenBudget(contextWindow, estimateTokens(a.systemPrompt))

	doomDetector := &doomLoopDetector{}

	for iteration := 0; maxIter == 0 || iteration < maxIter; iteration++ {
		// Check if the turn was cancelled before starting an iteration.
		if turnCtx.Err() != nil {
			a.io.SystemMessage("Interrupted.")
			return nil
		}

		// Two-stage auto-compaction.
		a.maybeCompact(turnCtx, budget)

		// Generate compacted copy for sending (does not modify session).
		compacted := session.CompactHistory(a.session.Messages, budget.HistoryMax, a.session.Summary)

		req := &provider.ChatRequest{
			Model:        a.config.Model,
			Messages:     compacted,
			Tools:        a.buildToolSchemas(),
			SystemPrompt: a.systemPrompt,
			MaxTokens:    8192,
		}

		var textContent strings.Builder
		var toolCalls []*provider.ToolCallRequest
		var streamErr error

		// Retry loop for transient API errors.
		for attempt := range maxRetries + 1 {
			textContent.Reset()
			toolCalls = nil
			streamErr = nil

			events, err := a.provider.Chat(turnCtx, req)
			if err != nil {
				// If cancelled by user Esc, exit gracefully.
				if turnCtx.Err() != nil {
					a.io.SystemMessage("Interrupted.")
					return nil
				}
				if attempt < maxRetries && isRetryableError(err) {
					delay := retryDelay(attempt)
					a.io.SystemMessage(formatRetryMessage(attempt, maxRetries, delay, err))
					if sleepErr := sleepWithContext(turnCtx, delay); sleepErr != nil {
						a.io.SystemMessage("Interrupted.")
						return nil
					}
					continue
				}
				return fmt.Errorf("LLM call failed: %w", err)
			}

			a.io.ThinkingStart()

			receivedContent := false
			for event := range events {
				// Check for user cancellation mid-stream.
				if turnCtx.Err() != nil {
					break
				}
				switch event.Type {
				case provider.EventTextDelta:
					receivedContent = true
					a.io.TextDelta(event.TextDelta)
					textContent.WriteString(event.TextDelta)

				case provider.EventToolCallDone:
					receivedContent = true
					toolCalls = append(toolCalls, event.ToolCall)

				case provider.EventDone:
					if event.Usage != nil {
						a.session.PromptTokens = event.Usage.InputTokens
						a.session.CompletionTokens = event.Usage.OutputTokens
						a.session.TokensUsed += event.Usage.InputTokens + event.Usage.OutputTokens
						a.io.SetTokens(a.session.TokensUsed)
					}

				case provider.EventError:
					streamErr = event.Error
				}
			}

			// If user cancelled during streaming, exit gracefully.
			if turnCtx.Err() != nil {
				full := textContent.String()
				a.io.TextDone(full)
				if full != "" {
					a.session.AddMessage(buildAssistantMessage(full, nil))
				}
				a.io.SystemMessage("Interrupted.")
				return nil
			}

			// If stream error occurred before any content, retry if possible.
			if streamErr != nil && !receivedContent && attempt < maxRetries && isRetryableError(streamErr) {
				delay := retryDelay(attempt)
				a.io.SystemMessage(formatRetryMessage(attempt, maxRetries, delay, streamErr))
				if sleepErr := sleepWithContext(turnCtx, delay); sleepErr != nil {
					a.io.SystemMessage("Interrupted.")
					return nil
				}
				continue
			}

			// Stream error after content was received — can't retry safely.
			if streamErr != nil {
				return fmt.Errorf("stream error: %w", streamErr)
			}

			break // success
		}

		full := textContent.String()
		a.io.TextDone(full)

		assistantMsg := buildAssistantMessage(full, toolCalls)
		a.session.AddMessage(assistantMsg)

		if len(toolCalls) == 0 {
			return nil
		}

		if maxIter > 0 && iteration == maxIter-1 {
			a.io.SystemMessage(fmt.Sprintf(
				"warning: reached max iterations (%d), stopping", maxIter))
			return nil
		}

		// Doom loop detection: catch the model issuing identical tool calls repeatedly.
		switch doomDetector.check(toolCalls) {
		case doomLoopWarn:
			warning := "You have been issuing the same tool calls repeatedly. " +
				"This looks like an infinite loop. Try a different approach or stop calling tools."
			a.io.SystemMessage("warning: possible doom loop detected — injecting hint to model")
			a.session.AddMessage(provider.Message{
				Role: provider.RoleUser,
				Content: []provider.Content{{
					Type: provider.ContentTypeText,
					Text: "[SYSTEM] " + warning,
				}},
			})
		case doomLoopStop:
			a.io.SystemMessage("error: doom loop detected — same tool calls repeated 5 times, stopping")
			return nil
		}

		toolResults, interrupted := a.executeToolCalls(turnCtx, toolCalls)
		a.session.AddMessage(provider.Message{
			Role:    provider.RoleUser,
			Content: toolResults,
		})

		// If user interrupted during tool execution, stop the loop
		// and return to user input. The partial results are already
		// in the message history for context continuity.
		if interrupted {
			a.io.SystemMessage("Interrupted.")
			return nil
		}
	}
	return nil
}

// buildAssistantMessage creates a history message from the LLM response.
func buildAssistantMessage(text string, toolCalls []*provider.ToolCallRequest) provider.Message {
	var contents []provider.Content

	if text != "" {
		contents = append(contents, provider.Content{
			Type: provider.ContentTypeText,
			Text: text,
		})
	}

	for _, tc := range toolCalls {
		contents = append(contents, provider.Content{
			Type:      provider.ContentTypeToolUse,
			ToolUseID: tc.ID,
			ToolName:  tc.Name,
			ToolInput: tc.Input,
		})
	}

	return provider.Message{Role: provider.RoleAssistant, Content: contents}
}

// executeToolCalls runs each tool call and returns tool_result content blocks.
// The second return value is true if the user interrupted (Esc) during execution,
// signaling that the agent loop should stop.
func (a *Agent) executeToolCalls(ctx context.Context, calls []*provider.ToolCallRequest) ([]provider.Content, bool) {
	var results []provider.Content

	for _, call := range calls {
		a.io.ToolStart(call.ID, call.Name, string(call.Input))

		result := a.executor.Execute(ctx, call.Name, call.Input)

		a.io.ToolDone(call.ID, call.Name, result.Content, result.IsError)

		results = append(results, provider.Content{
			Type:       provider.ContentTypeToolResult,
			ToolUseID:  call.ID,
			ToolResult: result.Content,
			IsError:    result.IsError,
		})

		// User pressed Esc — stop executing remaining tools immediately.
		if result.UserCancelled {
			// Fill in empty results for remaining tool calls so the
			// message history stays structurally valid (every tool_use
			// must have a corresponding tool_result).
			for _, remaining := range calls[len(results):] {
				results = append(results, provider.Content{
					Type:       provider.ContentTypeToolResult,
					ToolUseID:  remaining.ID,
					ToolResult: "[User cancelled this turn — tool was not executed. Do not retry unless the user asks.]",
					IsError:    false,
				})
			}
			return results, true
		}
	}

	return results, false
}

// maybeCompact runs two-stage auto-compaction if context is growing large.
//
// Stage 1 (70% threshold): Mask old tool outputs — fast, no LLM call.
// Stage 2 (80% threshold): Full summarization + truncation.
func (a *Agent) maybeCompact(ctx context.Context, budget *session.TokenBudget) {
	tokens := a.currentTokens(budget)

	// Stage 1: Gentle masking at 70%.
	if tokens >= budget.GentleThreshold() && !a.session.GentleCompactDone {
		before := tokens
		a.session.Messages = session.MaskOldToolOutputs(a.session.Messages, 10)
		a.session.GentleCompactDone = true
		after := a.currentTokens(budget)
		saved := before - after
		if saved > 0 {
			a.io.SystemMessage(fmt.Sprintf(
				"Context growing large — masked old tool outputs (%dk → %dk tokens, saved %dk).",
				before/1000, after/1000, saved/1000))
		}
		return // don't also do full compact in the same iteration
	}

	// Stage 2: Full summarization at 80%.
	if tokens >= budget.CompactThreshold() && a.summarizer != nil {
		before := tokens
		a.io.SystemMessage("Compacting context (summarizing conversation)...")
		summary, err := a.summarizer.Summarize(ctx, a.session.Summary, a.session.Messages)
		if err != nil {
			a.io.Error("Compact failed: " + err.Error())
			return
		}
		a.session.Summary = summary
		a.session.Messages = session.TruncateSession(a.session.Messages, 10)
		a.session.GentleCompactDone = false // reset for next cycle
		after := a.currentTokens(budget)
		a.io.SystemMessage(fmt.Sprintf(
			"Context compacted: %dk → %dk tokens. %d messages retained.",
			before/1000, after/1000, len(a.session.Messages)))
	}
}

// currentTokens returns the current token usage estimate.
func (a *Agent) currentTokens(budget *session.TokenBudget) int {
	// Prefer actual API-reported tokens.
	if a.session.PromptTokens > 0 {
		return a.session.PromptTokens
	}
	return a.session.EstimateTokens() + estimateTokens(a.systemPrompt)
}

// estimateTokens returns a rough token estimate for a string (chars / 4).
func estimateTokens(s string) int {
	return len(s) / 4
}

// buildToolSchemas converts the executor's registry tools into provider.ToolSchema.
func (a *Agent) buildToolSchemas() []provider.ToolSchema {
	registryTools := a.executor.Registry().All()
	schemas := make([]provider.ToolSchema, 0, len(registryTools))
	for _, t := range registryTools {
		schemas = append(schemas, provider.ToolSchema{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		})
	}
	return schemas
}
