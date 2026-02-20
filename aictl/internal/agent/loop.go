package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/aictl/aictl/internal/provider"
	"github.com/aictl/aictl/internal/session"
)

// runAgentLoop executes the core agentic loop:
//  1. Send messages to the LLM via streaming Chat()
//  2. Collect text deltas (stream to UI) and tool calls
//  3. If tool calls exist, execute them, append results to history, and loop
//  4. If no tool calls, return (wait for next user input)
func (a *Agent) runAgentLoop(ctx context.Context) error {
	maxIter := a.config.MaxIterations
	if maxIter <= 0 {
		maxIter = 25
	}

	// Compute token budget.
	contextWindow := a.config.ContextWindow
	if contextWindow <= 0 {
		contextWindow = a.provider.ContextWindow()
	}
	budget := session.NewTokenBudget(contextWindow, estimateTokens(a.systemPrompt))

	for iteration := range maxIter {
		// Auto-compact if summarizer is available and threshold exceeded.
		if a.shouldCompact(budget) && a.summarizer != nil {
			summary, err := a.summarizer.Summarize(ctx, a.session.Summary, a.session.Messages)
			if err == nil {
				a.session.Summary = summary
				a.session.Messages = session.TruncateSession(a.session.Messages, 10)
				a.io.SystemMessage("Context compacted.")
			}
		}

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

			events, err := a.provider.Chat(ctx, req)
			if err != nil {
				if attempt < maxRetries && isRetryableError(err) {
					delay := retryDelay(attempt)
					a.io.SystemMessage(formatRetryMessage(attempt, maxRetries, delay, err))
					if sleepErr := sleepWithContext(ctx, delay); sleepErr != nil {
						return sleepErr
					}
					continue
				}
				return fmt.Errorf("LLM call failed: %w", err)
			}

			a.io.ThinkingStart()

			receivedContent := false
			for event := range events {
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

			// If stream error occurred before any content, retry if possible.
			if streamErr != nil && !receivedContent && attempt < maxRetries && isRetryableError(streamErr) {
				delay := retryDelay(attempt)
				a.io.SystemMessage(formatRetryMessage(attempt, maxRetries, delay, streamErr))
				if sleepErr := sleepWithContext(ctx, delay); sleepErr != nil {
					return sleepErr
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

		if iteration == maxIter-1 {
			a.io.SystemMessage(fmt.Sprintf(
				"warning: reached max iterations (%d), stopping", maxIter))
			return nil
		}

		toolResults := a.executeToolCalls(ctx, toolCalls)
		a.session.AddMessage(provider.Message{
			Role:    provider.RoleUser,
			Content: toolResults,
		})
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
func (a *Agent) executeToolCalls(ctx context.Context, calls []*provider.ToolCallRequest) []provider.Content {
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
	}

	return results
}

// shouldCompact returns true when the context is large enough to warrant compaction.
func (a *Agent) shouldCompact(budget *session.TokenBudget) bool {
	// Use actual API-reported prompt tokens if available.
	if a.session.PromptTokens > 0 {
		return a.session.PromptTokens >= budget.CompactThreshold()
	}
	// Fallback to estimation.
	estimated := a.session.EstimateTokens() + estimateTokens(a.systemPrompt)
	return estimated >= budget.CompactThreshold()
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
