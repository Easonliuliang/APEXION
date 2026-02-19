package agent

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aictl/aictl/internal/provider"
	"github.com/aictl/aictl/internal/session"
)

// runAgentLoop executes the core agentic loop:
//  1. Send messages to the LLM via streaming Chat()
//  2. Collect text deltas (print to terminal) and tool calls
//  3. If tool calls exist, execute them, append results to history, and loop
//  4. If no tool calls, return (wait for next user input)
//
// Stops after config.MaxIterations to prevent runaway loops.
func (a *Agent) runAgentLoop(ctx context.Context) error {
	maxIter := a.config.MaxIterations
	if maxIter <= 0 {
		maxIter = 25
	}

	for iteration := range maxIter {
		// Trim context window if needed (128k tokens default)
		a.session.Messages = session.TrimHistory(a.session.Messages, 128000)

		// Build tool schemas for the provider
		toolSchemas := a.buildToolSchemas()

		// Build the chat request
		req := &provider.ChatRequest{
			Model:        a.config.Model,
			Messages:     a.session.Messages,
			Tools:        toolSchemas,
			SystemPrompt: a.systemPrompt,
			MaxTokens:    8192,
		}

		events, err := a.provider.Chat(ctx, req)
		if err != nil {
			return fmt.Errorf("LLM call failed: %w", err)
		}

		// Consume the event stream
		var textContent strings.Builder
		var toolCalls []*provider.ToolCallRequest

		fmt.Println() // newline before AI output
		for event := range events {
			switch event.Type {
			case provider.EventTextDelta:
				fmt.Print(event.TextDelta)
				textContent.WriteString(event.TextDelta)

			case provider.EventToolCallDone:
				toolCalls = append(toolCalls, event.ToolCall)

			case provider.EventDone:
				if event.Usage != nil {
					a.session.TokensUsed += event.Usage.InputTokens + event.Usage.OutputTokens
				}

			case provider.EventError:
				return fmt.Errorf("stream error: %w", event.Error)
			}
		}

		// Build and append the assistant message to history
		assistantMsg := buildAssistantMessage(textContent.String(), toolCalls)
		a.session.AddMessage(assistantMsg)

		// No tool calls means the LLM is done; return to user.
		if len(toolCalls) == 0 {
			return nil
		}

		// Check iteration limit
		if iteration == maxIter-1 {
			fmt.Fprintf(os.Stderr, "\nwarning: reached max iterations (%d), stopping\n", maxIter)
			return nil
		}

		// Execute tool calls and append results as a user message
		fmt.Println(strings.Repeat("-", 30))
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

// executeToolCalls runs each tool call sequentially and returns tool_result content blocks.
func (a *Agent) executeToolCalls(ctx context.Context, calls []*provider.ToolCallRequest) []provider.Content {
	var results []provider.Content

	for _, call := range calls {
		fmt.Printf("  Executing %s...\n", call.Name)

		result := a.executor.Execute(ctx, call.Name, call.Input)

		if result.IsError {
			fmt.Printf("    Error: %s\n", truncate(result.Content, 80))
		} else {
			preview := truncate(strings.ReplaceAll(result.Content, "\n", " "), 60)
			fmt.Printf("    Result: %s\n", preview)
		}

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
