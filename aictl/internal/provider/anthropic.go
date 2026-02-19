package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"
)

// AnthropicProvider implements Provider using the Anthropic native API.
type AnthropicProvider struct {
	client anthropic.Client
	model  string
}

func NewAnthropicProvider(apiKey, model string) *AnthropicProvider {
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}
	return &AnthropicProvider{
		client: anthropic.NewClient(anthropicoption.WithAPIKey(apiKey)),
		model:  model,
	}
}

func (p *AnthropicProvider) Name() string        { return "anthropic" }
func (p *AnthropicProvider) Models() []string     { return []string{p.model} }
func (p *AnthropicProvider) DefaultModel() string { return p.model }

func (p *AnthropicProvider) Chat(ctx context.Context, req *ChatRequest) (<-chan Event, error) {
	msgs := p.buildMessages(req.Messages)
	tools := p.buildTools(req.Tools)

	model := req.Model
	if model == "" {
		model = p.model
	}

	maxTokens := int64(req.MaxTokens)
	if maxTokens <= 0 {
		maxTokens = 8192
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		Messages:  msgs,
		MaxTokens: maxTokens,
	}
	if req.SystemPrompt != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.SystemPrompt}}
	}
	if len(tools) > 0 {
		params.Tools = tools
	}

	stream := p.client.Messages.NewStreaming(ctx, params)

	ch := make(chan Event, 16)
	go p.processStream(ctx, stream, ch)
	return ch, nil
}

// processStream reads the Anthropic SSE stream and emits unified events.
//
// Anthropic streaming event sequence:
//   - ContentBlockStartEvent (tool_use) -> record tool call id/name
//   - ContentBlockDeltaEvent (InputJSONDelta) -> accumulate JSON arguments
//   - ContentBlockStopEvent -> tool call arguments complete, emit EventToolCallDone
//   - ContentBlockDeltaEvent (TextDelta) -> emit EventTextDelta
//   - MessageDeltaEvent -> emit EventDone with usage
func (p *AnthropicProvider) processStream(ctx context.Context, stream *ssestream.Stream[anthropic.MessageStreamEventUnion], ch chan<- Event) {
	defer close(ch)
	defer stream.Close()

	type pendingCall struct {
		id      string
		name    string
		jsonBuf strings.Builder
	}
	// Track pending tool calls by content block index.
	pending := make(map[int64]*pendingCall)

	for stream.Next() {
		select {
		case <-ctx.Done():
			ch <- Event{Type: EventError, Error: ctx.Err()}
			return
		default:
		}

		event := stream.Current()

		switch variant := event.AsAny().(type) {
		case anthropic.ContentBlockStartEvent:
			// Check if this content block is a tool_use block.
			cb := variant.ContentBlock
			if cb.Type == "tool_use" {
				toolUse := cb.AsToolUse()
				pending[variant.Index] = &pendingCall{
					id:   toolUse.ID,
					name: toolUse.Name,
				}
			}

		case anthropic.ContentBlockDeltaEvent:
			delta := variant.Delta
			switch d := delta.AsAny().(type) {
			case anthropic.TextDelta:
				ch <- Event{Type: EventTextDelta, TextDelta: d.Text}
			case anthropic.InputJSONDelta:
				if pc, ok := pending[variant.Index]; ok {
					pc.jsonBuf.WriteString(d.PartialJSON)
				}
			}

		case anthropic.ContentBlockStopEvent:
			if pc, ok := pending[variant.Index]; ok {
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
				delete(pending, variant.Index)
			}

		case anthropic.MessageDeltaEvent:
			ch <- Event{
				Type: EventDone,
				Usage: &Usage{
					InputTokens:  int(variant.Usage.InputTokens),
					OutputTokens: int(variant.Usage.OutputTokens),
				},
			}
			return
		}
	}

	if err := stream.Err(); err != nil {
		ch <- Event{Type: EventError, Error: fmt.Errorf("anthropic streaming error: %w", err)}
		return
	}

	ch <- Event{Type: EventDone, Usage: &Usage{}}
}

// buildMessages converts unified Message types to Anthropic API params.
func (p *AnthropicProvider) buildMessages(msgs []Message) []anthropic.MessageParam {
	var params []anthropic.MessageParam

	for _, msg := range msgs {
		var blocks []anthropic.ContentBlockParamUnion

		for _, c := range msg.Content {
			switch c.Type {
			case ContentTypeText:
				blocks = append(blocks, anthropic.NewTextBlock(c.Text))
			case ContentTypeToolUse:
				// ToolInput is json.RawMessage; parse it to any for the SDK.
				var input any
				if len(c.ToolInput) > 0 {
					_ = json.Unmarshal(c.ToolInput, &input)
				}
				if input == nil {
					input = map[string]any{}
				}
				blocks = append(blocks, anthropic.NewToolUseBlock(c.ToolUseID, input, c.ToolName))
			case ContentTypeToolResult:
				blocks = append(blocks, anthropic.NewToolResultBlock(c.ToolUseID, c.ToolResult, c.IsError))
			}
		}

		switch msg.Role {
		case RoleUser:
			params = append(params, anthropic.NewUserMessage(blocks...))
		case RoleAssistant:
			params = append(params, anthropic.NewAssistantMessage(blocks...))
		}
	}
	return params
}

// buildTools converts unified ToolSchema to Anthropic tool params.
func (p *AnthropicProvider) buildTools(tools []ToolSchema) []anthropic.ToolUnionParam {
	var result []anthropic.ToolUnionParam
	for _, t := range tools {
		result = append(result, anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        t.Name,
				Description: anthropic.String(t.Description),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: t.Parameters,
				},
			},
		})
	}
	return result
}
