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

	for iteration := range maxIter {
		a.session.Messages = session.TrimHistory(a.session.Messages, 128000)

		req := &provider.ChatRequest{
			Model:        a.config.Model,
			Messages:     a.session.Messages,
			Tools:        a.buildToolSchemas(),
			SystemPrompt: a.systemPrompt,
			MaxTokens:    8192,
		}

		events, err := a.provider.Chat(ctx, req)
		if err != nil {
			return fmt.Errorf("LLM call failed: %w", err)
		}

		var textContent strings.Builder
		var toolCalls []*provider.ToolCallRequest

		a.io.ThinkingStart()

		for event := range events {
			switch event.Type {
			case provider.EventTextDelta:
				a.io.TextDelta(event.TextDelta)
				textContent.WriteString(event.TextDelta)

			case provider.EventToolCallDone:
				toolCalls = append(toolCalls, event.ToolCall)

			case provider.EventDone:
				if event.Usage != nil {
					a.session.TokensUsed += event.Usage.InputTokens + event.Usage.OutputTokens
					a.io.SetTokens(a.session.TokensUsed)
				}

			case provider.EventError:
				return fmt.Errorf("stream error: %w", event.Error)
			}
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
