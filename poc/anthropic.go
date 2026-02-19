// provider.go — OpenAI-compatible provider
// 支持 MiniMax、DeepSeek、Kimi、通义千问等所有 OpenAI 兼容模型
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
)

// Message 是对话历史中的一条消息
type Message struct {
	Role    string    // "user" | "assistant"
	Content []Content // 支持多 content block
}

// Content 是消息中的一个内容块
type Content struct {
	Type       string          // "text" | "tool_use" | "tool_result"
	Text       string          // Type=="text"
	ToolUseID  string          // Type=="tool_use" / "tool_result"
	ToolName   string          // Type=="tool_use"
	ToolInput  json.RawMessage // Type=="tool_use"
	ToolResult string          // Type=="tool_result"
	IsError    bool
}

// ToolCall 代表 LLM 请求执行的单个工具调用
type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// StreamResult 是一轮 streaming 完成后的结果
type StreamResult struct {
	TextContent string
	ToolCalls   []ToolCall
	StopReason  string // "stop" | "tool_calls" | "length"
}

// LLMClient 封装 OpenAI-compatible API
type LLMClient struct {
	client   openai.Client // 值类型，不是指针
	model    string
	registry *ToolRegistry
}

func NewLLMClient(registry *ToolRegistry) *LLMClient {
	apiKey := os.Getenv("LLM_API_KEY")
	baseURL := os.Getenv("LLM_BASE_URL")
	model := os.Getenv("LLM_MODEL")

	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "error: LLM_API_KEY is not set")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "MiniMax 示例:")
		fmt.Fprintln(os.Stderr, "  export LLM_API_KEY=your-minimax-key")
		fmt.Fprintln(os.Stderr, "  export LLM_BASE_URL=https://api.minimax.chat/v1")
		fmt.Fprintln(os.Stderr, "  export LLM_MODEL=MiniMax-Text-01")
		os.Exit(1)
	}

	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	if model == "" {
		model = "gpt-4o-mini"
	}

	return &LLMClient{
		client:   openai.NewClient(opts...),
		model:    model,
		registry: registry,
	}
}

// buildAPIMessages 将内部 Message 转换为 OpenAI API 格式
func buildAPIMessages(history []Message) []openai.ChatCompletionMessageParamUnion {
	var params []openai.ChatCompletionMessageParamUnion

	for _, msg := range history {
		switch msg.Role {
		case "user":
			// tool_result 和普通文本分开处理
			for _, c := range msg.Content {
				switch c.Type {
				case "text":
					params = append(params, openai.UserMessage(c.Text))
				case "tool_result":
					params = append(params, openai.ToolMessage(c.ToolResult, c.ToolUseID))
				}
			}

		case "assistant":
			var text string
			var toolCalls []openai.ChatCompletionMessageToolCallParam

			for _, c := range msg.Content {
				switch c.Type {
				case "text":
					text = c.Text
				case "tool_use":
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

// buildTools 将 ToolRegistry 转换为 OpenAI tools 格式
func buildTools(registry *ToolRegistry) []openai.ChatCompletionToolParam {
	var tools []openai.ChatCompletionToolParam
	for _, schema := range registry.ToSchema() {
		props := schema["input_schema"].(map[string]any)["properties"]
		tools = append(tools, openai.ChatCompletionToolParam{
			Type: "function",
			Function: shared.FunctionDefinitionParam{
				Name:        schema["name"].(string),
				Description: openai.String(schema["description"].(string)),
				Parameters: shared.FunctionParameters{
					"type":       "object",
					"properties": props,
				},
			},
		})
	}
	return tools
}

// Chat 发起一次 streaming 对话，处理 tool use 状态机。
//
// OpenAI streaming tool use 的关键差异（对比 Anthropic）：
//   - tool call 通过 delta.tool_calls[] 返回
//   - 每个 tool call 有 index 字段区分（同一响应中多个 tool call 时）
//   - id 和 name 只在该 tool call 的第一个 delta 中出现
//   - arguments 是增量 JSON 字符串，需要拼接
func (c *LLMClient) Chat(ctx context.Context, history []Message, systemPrompt string) (*StreamResult, error) {
	// 构造消息列表
	var msgs []openai.ChatCompletionMessageParamUnion
	if systemPrompt != "" {
		msgs = append(msgs, openai.SystemMessage(systemPrompt))
	}
	msgs = append(msgs, buildAPIMessages(history)...)

	// 发起 streaming 请求
	stream := c.client.Chat.Completions.NewStreaming(ctx, openai.ChatCompletionNewParams{
		Model:    openai.ChatModel(c.model),
		Messages: msgs,
		Tools:    buildTools(c.registry),
	})

	// ── 状态机变量 ────────────────────────────────────────────────────────────

	var textBuf strings.Builder

	type pendingCall struct {
		id      string
		name    string
		jsonBuf strings.Builder
	}
	pending := make(map[int]*pendingCall) // key: tool call index
	var callOrder []int
	var stopReason string

	// ── 逐 chunk 处理 ─────────────────────────────────────────────────────────
	for stream.Next() {
		chunk := stream.Current()

		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]

		if string(choice.FinishReason) != "" {
			stopReason = string(choice.FinishReason)
		}

		delta := choice.Delta

		// 文本增量
		if delta.Content != "" {
			fmt.Print(delta.Content)
			textBuf.WriteString(delta.Content)
		}

		// Tool call 增量
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
				fmt.Printf("\n🔧 Tool: %s ", tc.Function.Name)
			}
			if tc.Function.Arguments != "" {
				pc.jsonBuf.WriteString(tc.Function.Arguments)
			}
		}
	}

	if err := stream.Err(); err != nil {
		return nil, fmt.Errorf("streaming error: %w", err)
	}

	// 打印每个 tool call 的参数摘要
	for _, idx := range callOrder {
		pc := pending[idx]
		fmt.Printf("← %s\n", truncate(pc.jsonBuf.String(), 80))
	}

	// 组装结果
	result := &StreamResult{
		TextContent: textBuf.String(),
		StopReason:  stopReason,
	}
	for _, idx := range callOrder {
		pc := pending[idx]
		inputJSON := pc.jsonBuf.String()
		if inputJSON == "" {
			inputJSON = "{}"
		}
		result.ToolCalls = append(result.ToolCalls, ToolCall{
			ID:    pc.id,
			Name:  pc.name,
			Input: json.RawMessage(inputJSON),
		})
	}

	return result, nil
}

// truncate 截断字符串用于终端展示
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
