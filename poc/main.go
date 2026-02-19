package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
)

const systemPrompt = `You are aictl, an AI coding assistant running in the terminal.
You help users with software engineering tasks by reading files and running commands.

## Rules
- Before making changes, read the relevant files first to understand the current code
- Always explain what you're about to do before calling tools
- Be concise in your explanations`

const maxIterations = 25 // 防止无限 tool call 循环

func main() {
	registry := NewToolRegistry()
	client := NewLLMClient(registry)

	var history []Message

	fmt.Println("aictl POC — type your request, /quit to exit")
	fmt.Println(strings.Repeat("─", 50))

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 支持长输入

	for {
		fmt.Print("\n> ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())

		if input == "" {
			continue
		}

		// ── 内置 slash 命令 ───────────────────────────────────────────────────
		switch input {
		case "/quit", "/exit", "/q":
			fmt.Println("Bye.")
			return
		case "/clear":
			history = nil
			fmt.Println("Session cleared.")
			continue
		case "/history":
			printHistory(history)
			continue
		}

		// ── 将用户输入追加到历史 ──────────────────────────────────────────────
		history = append(history, Message{
			Role:    "user",
			Content: []Content{{Type: "text", Text: input}},
		})

		// ── Agent Loop ────────────────────────────────────────────────────────
		ctx := context.Background()
		if err := runAgentLoop(ctx, client, &history); err != nil {
			fmt.Fprintf(os.Stderr, "\nerror: %v\n", err)
		}

		fmt.Println() // 输出结束后换行
	}
}

// runAgentLoop 是核心 agentic loop：
//  1. 调用 LLM（streaming）
//  2. 如果有 tool call → 执行 → 把结果追加到历史 → 重新调用 LLM
//  3. 直到 LLM 输出 final answer（无 tool call）或超过最大迭代次数
func runAgentLoop(ctx context.Context, client *LLMClient, history *[]Message) error {
	for iteration := range maxIterations {
		// ── 调用 LLM（streaming）────────────────────────────────────────────
		fmt.Println() // 在 AI 输出前换行
		result, err := client.Chat(ctx, *history, systemPrompt)
		if err != nil {
			return fmt.Errorf("LLM call failed: %w", err)
		}

		// ── 将 LLM 响应追加到历史 ─────────────────────────────────────────────
		assistantMsg := buildAssistantMessage(result)
		*history = append(*history, assistantMsg)

		// ── 无 tool call → 对话结束，等待用户下一轮输入 ──────────────────────
		if len(result.ToolCalls) == 0 {
			return nil
		}

		// ── 有 tool call → 逐个执行，收集结果 ──────────────────────────────────
		if iteration == maxIterations-1 {
			fmt.Fprintf(os.Stderr, "\nwarning: reached max iterations (%d), stopping\n", maxIterations)
			return nil
		}

		fmt.Println(strings.Repeat("─", 30))
		toolResults := executeToolCalls(ctx, client.registry, result.ToolCalls)

		// ── 将工具执行结果追加到历史（作为 user 消息中的 tool_result blocks）──
		*history = append(*history, Message{
			Role:    "user",
			Content: toolResults,
		})
	}
	return nil
}

// buildAssistantMessage 将 StreamResult 转换为历史消息格式
// 同时记录文本内容和 tool_use blocks（Anthropic 要求两者都保留）
func buildAssistantMessage(result *StreamResult) Message {
	var contents []Content

	if result.TextContent != "" {
		contents = append(contents, Content{
			Type: "text",
			Text: result.TextContent,
		})
	}

	for _, tc := range result.ToolCalls {
		contents = append(contents, Content{
			Type:      "tool_use",
			ToolUseID: tc.ID,
			ToolName:  tc.Name,
			ToolInput: tc.Input,
		})
	}

	return Message{Role: "assistant", Content: contents}
}

// executeToolCalls 顺序执行所有 tool call，返回 tool_result content blocks
// POC 阶段串行执行；正式版可对只读工具并行
func executeToolCalls(ctx context.Context, registry *ToolRegistry, calls []ToolCall) []Content {
	var results []Content

	for _, call := range calls {
		fmt.Printf("⚙️  Executing %s...\n", call.Name)

		tool, ok := registry.Get(call.Name)
		if !ok {
			results = append(results, Content{
				Type:       "tool_result",
				ToolUseID:  call.ID,
				ToolResult: fmt.Sprintf("Error: unknown tool '%s'", call.Name),
				IsError:    true,
			})
			continue
		}

		output, err := tool.Execute(ctx, call.Input)
		if err != nil {
			fmt.Printf("   ❌ Error: %v\n", err)
			results = append(results, Content{
				Type:       "tool_result",
				ToolUseID:  call.ID,
				ToolResult: fmt.Sprintf("Error: %v", err),
				IsError:    true,
			})
		} else {
			preview := truncate(strings.ReplaceAll(output, "\n", "↵"), 60)
			fmt.Printf("   ✅ Result: %s\n", preview)
			results = append(results, Content{
				Type:       "tool_result",
				ToolUseID:  call.ID,
				ToolResult: output,
				IsError:    false,
			})
		}
	}

	return results
}

// printHistory 打印当前会话历史（调试用）
func printHistory(history []Message) {
	if len(history) == 0 {
		fmt.Println("No history.")
		return
	}
	fmt.Printf("\n=== History (%d messages) ===\n", len(history))
	for i, msg := range history {
		fmt.Printf("[%d] %s:\n", i, msg.Role)
		for _, c := range msg.Content {
			switch c.Type {
			case "text":
				fmt.Printf("    text: %s\n", truncate(c.Text, 100))
			case "tool_use":
				fmt.Printf("    tool_use: %s(%s)\n", c.ToolName, truncate(string(c.ToolInput), 60))
			case "tool_result":
				status := "✅"
				if c.IsError {
					status = "❌"
				}
				fmt.Printf("    tool_result[%s]: %s\n", status, truncate(c.ToolResult, 60))
			}
		}
	}
	fmt.Println("===")
}
