package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/ssestream"
	"github.com/openai/openai-go/shared"
)

// OpenAIProvider implements Provider for all OpenAI-compatible APIs,
// including OpenAI, DeepSeek, MiniMax, Kimi, Qwen, etc.
type OpenAIProvider struct {
	client  openai.Client
	model   string
	name    string
	baseURL string
}

func NewOpenAIProvider(apiKey, baseURL, model string) *OpenAIProvider {
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	if model == "" {
		model = "gpt-4o-mini"
	}

	name := "openai"
	if baseURL != "" {
		switch {
		case strings.Contains(baseURL, "deepseek"):
			name = "deepseek"
		case strings.Contains(baseURL, "minimax"):
			name = "minimax"
		case strings.Contains(baseURL, "moonshot"):
			name = "kimi"
		case strings.Contains(baseURL, "dashscope"):
			name = "qwen"
		}
	}

	return &OpenAIProvider{
		client:  openai.NewClient(opts...),
		model:   model,
		name:    name,
		baseURL: baseURL,
	}
}

func (p *OpenAIProvider) Name() string        { return p.name }
func (p *OpenAIProvider) Models() []string     { return []string{p.model} }
func (p *OpenAIProvider) DefaultModel() string { return p.model }

func (p *OpenAIProvider) Chat(ctx context.Context, req *ChatRequest) (<-chan Event, error) {
	msgs := p.buildMessages(req)
	tools := p.buildTools(req.Tools)

	model := req.Model
	if model == "" {
		model = p.model
	}

	params := openai.ChatCompletionNewParams{
		Model:    openai.ChatModel(model),
		Messages: msgs,
	}
	if len(tools) > 0 {
		params.Tools = tools
	}

	stream := p.client.Chat.Completions.NewStreaming(ctx, params)

	ch := make(chan Event, 16)
	go p.processStream(ctx, stream, ch)
	return ch, nil
}

// processStream reads the OpenAI SSE stream and emits unified events.
//
// OpenAI streaming tool use key behavior:
//   - tool call deltas arrive via delta.ToolCalls[]
//   - each tool call has an index field to distinguish multiple concurrent calls
//   - id and name only appear in the first delta for that index
//   - arguments are incremental JSON strings that must be concatenated
func (p *OpenAIProvider) processStream(ctx context.Context, stream *ssestream.Stream[openai.ChatCompletionChunk], ch chan<- Event) {
	defer close(ch)

	type pendingCall struct {
		id      string
		name    string
		jsonBuf strings.Builder
	}
	pending := make(map[int]*pendingCall)
	var callOrder []int

	for stream.Next() {
		select {
		case <-ctx.Done():
			ch <- Event{Type: EventError, Error: ctx.Err()}
			return
		default:
		}

		chunk := stream.Current()
		if len(chunk.Choices) == 0 {
			// Final chunk may only carry usage.
			if chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
				ch <- Event{
					Type: EventDone,
					Usage: &Usage{
						InputTokens:  int(chunk.Usage.PromptTokens),
						OutputTokens: int(chunk.Usage.CompletionTokens),
					},
				}
			}
			continue
		}

		choice := chunk.Choices[0]
		delta := choice.Delta

		// Text delta
		if delta.Content != "" {
			ch <- Event{Type: EventTextDelta, TextDelta: delta.Content}
		}

		// Tool call deltas
		for _, tc := range delta.ToolCalls {
			idx := int(tc.Index)
			if _, exists := pending[idx]; !exists {
				pending[idx] = &pendingCall{}
				callOrder = append(callOrder, idx)
			}
			pc := pending[idx]
			if tc.ID != "" {
				pc.id = tc.ID
			}
			if tc.Function.Name != "" {
				pc.name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				pc.jsonBuf.WriteString(tc.Function.Arguments)
			}
		}

		// When finish_reason is set, emit completed tool calls then done.
		if string(choice.FinishReason) != "" {
			for _, idx := range callOrder {
				pc := pending[idx]
				inputJSON := pc.jsonBuf.String()
				if inputJSON == "" {
					inputJSON = "{}"
				}
				ch <- Event{
					Type: EventToolCallDone,
					ToolCall: &ToolCallRequest{
						ID:    pc.id,
						Name:  pc.name,
						Input: json.RawMessage(inputJSON),
					},
				}
			}
			ch <- Event{
				Type: EventDone,
				Usage: &Usage{
					InputTokens:  int(chunk.Usage.PromptTokens),
					OutputTokens: int(chunk.Usage.CompletionTokens),
				},
			}
			return
		}
	}

	if err := stream.Err(); err != nil {
		ch <- Event{Type: EventError, Error: fmt.Errorf("openai streaming error: %w", err)}
		return
	}

	// Fallback: emit any remaining pending calls and done.
	for _, idx := range callOrder {
		pc := pending[idx]
		inputJSON := pc.jsonBuf.String()
		if inputJSON == "" {
			inputJSON = "{}"
		}
		ch <- Event{
			Type: EventToolCallDone,
			ToolCall: &ToolCallRequest{
				ID:    pc.id,
				Name:  pc.name,
				Input: json.RawMessage(inputJSON),
			},
		}
	}
	ch <- Event{Type: EventDone, Usage: &Usage{}}
}

// buildMessages converts unified Message types to OpenAI API params.
func (p *OpenAIProvider) buildMessages(req *ChatRequest) []openai.ChatCompletionMessageParamUnion {
	var params []openai.ChatCompletionMessageParamUnion

	if req.SystemPrompt != "" {
		params = append(params, openai.SystemMessage(req.SystemPrompt))
	}

	for _, msg := range req.Messages {
		switch msg.Role {
		case RoleUser:
			for _, c := range msg.Content {
				switch c.Type {
				case ContentTypeText:
					params = append(params, openai.UserMessage(c.Text))
				case ContentTypeToolResult:
					params = append(params, openai.ToolMessage(c.ToolResult, c.ToolUseID))
				}
			}

		case RoleAssistant:
			var text string
			var toolCalls []openai.ChatCompletionMessageToolCallParam
			for _, c := range msg.Content {
				switch c.Type {
				case ContentTypeText:
					text = c.Text
				case ContentTypeToolUse:
					toolCalls = append(toolCalls, openai.ChatCompletionMessageToolCallParam{
						ID:   c.ToolUseID,
						Type: "function",
						Function: openai.ChatCompletionMessageToolCallFunctionParam{
							Name:      c.ToolName,
							Arguments: string(c.ToolInput),
						},
					})
				}
			}
			assistant := openai.ChatCompletionAssistantMessageParam{
				Content:   openai.ChatCompletionAssistantMessageParamContentUnion{OfString: openai.String(text)},
				ToolCalls: toolCalls,
			}
			params = append(params, openai.ChatCompletionMessageParamUnion{OfAssistant: &assistant})
		}
	}
	return params
}

// buildTools converts unified ToolSchema to OpenAI tool params.
func (p *OpenAIProvider) buildTools(tools []ToolSchema) []openai.ChatCompletionToolParam {
	var result []openai.ChatCompletionToolParam
	for _, t := range tools {
		result = append(result, openai.ChatCompletionToolParam{
			Type: "function",
			Function: shared.FunctionDefinitionParam{
				Name:        t.Name,
				Description: openai.String(t.Description),
				Parameters: shared.FunctionParameters{
					"type":       "object",
					"properties": t.Parameters,
				},
			},
		})
	}
	return result
}
