package main

import (
	"encoding/json"
	"testing"
)

// mockChunk 模拟一个 OpenAI streaming chunk
type mockChunk struct {
	text         string // 文本增量
	toolIdx      int    // tool call index
	toolID       string // tool call id（只第一个 delta 有）
	toolName     string // tool call name（只第一个 delta 有）
	toolArgs     string // JSON 参数增量
	finishReason string // "stop" | "tool_calls" | ""
}

// runStateMachine 把 mock chunks 跑过状态机，返回 StreamResult
// 这是从 Chat() 中提取的纯状态机逻辑，不依赖任何 HTTP 调用
func runStateMachine(chunks []mockChunk) *StreamResult {
	var textBuf []byte

	type pendingCall struct {
		id      string
		name    string
		jsonBuf []byte
	}
	pending := make(map[int]*pendingCall)
	var callOrder []int
	var stopReason string

	for _, chunk := range chunks {
		if chunk.finishReason != "" {
			stopReason = chunk.finishReason
		}

		// 文本增量
		if chunk.text != "" {
			textBuf = append(textBuf, chunk.text...)
		}

		// tool call 增量（只有 toolName 或 toolArgs 不为空时才处理）
		if chunk.toolName != "" || chunk.toolArgs != "" || chunk.toolID != "" {
			if _, exists := pending[chunk.toolIdx]; !exists {
				pending[chunk.toolIdx] = &pendingCall{}
				callOrder = append(callOrder, chunk.toolIdx)
			}
			pc := pending[chunk.toolIdx]
			if chunk.toolID != "" {
				pc.id = chunk.toolID
			}
			if chunk.toolName != "" {
				pc.name = chunk.toolName
			}
			if chunk.toolArgs != "" {
				pc.jsonBuf = append(pc.jsonBuf, chunk.toolArgs...)
			}
		}
	}

	result := &StreamResult{
		TextContent: string(textBuf),
		StopReason:  stopReason,
	}
	for _, idx := range callOrder {
		pc := pending[idx]
		inputJSON := string(pc.jsonBuf)
		if inputJSON == "" {
			inputJSON = "{}"
		}
		result.ToolCalls = append(result.ToolCalls, ToolCall{
			ID:    pc.id,
			Name:  pc.name,
			Input: json.RawMessage(inputJSON),
		})
	}
	return result
}

// ── 测试用例 ──────────────────────────────────────────────────────────────────

// TestTextOnly 验证：纯文本响应（无 tool call）
func TestTextOnly(t *testing.T) {
	chunks := []mockChunk{
		{text: "Hello"},
		{text: ", "},
		{text: "world!"},
		{finishReason: "stop"},
	}
	result := runStateMachine(chunks)

	if result.TextContent != "Hello, world!" {
		t.Errorf("text: got %q, want %q", result.TextContent, "Hello, world!")
	}
	if len(result.ToolCalls) != 0 {
		t.Errorf("expected no tool calls, got %d", len(result.ToolCalls))
	}
	if result.StopReason != "stop" {
		t.Errorf("stop_reason: got %q, want %q", result.StopReason, "stop")
	}
}

// TestSingleToolCall 验证：单个 tool call，JSON 参数分多个 delta 到达
func TestSingleToolCall(t *testing.T) {
	chunks := []mockChunk{
		{text: "我来读取文件。"},
		// tool call 第一个 delta：有 id 和 name
		{toolIdx: 0, toolID: "call_abc123", toolName: "read_file", toolArgs: `{"pa`},
		// 后续 delta：只有参数增量
		{toolIdx: 0, toolArgs: `th": "./`},
		{toolIdx: 0, toolArgs: `main.go"}`},
		{finishReason: "tool_calls"},
	}
	result := runStateMachine(chunks)

	if result.TextContent != "我来读取文件。" {
		t.Errorf("text: got %q", result.TextContent)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	tc := result.ToolCalls[0]
	if tc.ID != "call_abc123" {
		t.Errorf("id: got %q, want %q", tc.ID, "call_abc123")
	}
	if tc.Name != "read_file" {
		t.Errorf("name: got %q, want %q", tc.Name, "read_file")
	}

	var params map[string]string
	if err := json.Unmarshal(tc.Input, &params); err != nil {
		t.Errorf("invalid JSON params: %v, raw: %s", err, tc.Input)
	}
	if params["path"] != "./main.go" {
		t.Errorf("path param: got %q, want %q", params["path"], "./main.go")
	}
	if result.StopReason != "tool_calls" {
		t.Errorf("stop_reason: got %q, want %q", result.StopReason, "tool_calls")
	}
}

// TestMultipleToolCalls 验证：一次响应中有多个 tool call（关键场景）
func TestMultipleToolCalls(t *testing.T) {
	chunks := []mockChunk{
		// tool call 0: read_file
		{toolIdx: 0, toolID: "call_111", toolName: "read_file", toolArgs: `{"path":"a.go"}`},
		// tool call 1: bash（与 read_file 交替出现，考验 index 隔离）
		{toolIdx: 1, toolID: "call_222", toolName: "bash", toolArgs: `{"command":"go `},
		{toolIdx: 1, toolArgs: `test ./..."}`},
		{finishReason: "tool_calls"},
	}
	result := runStateMachine(chunks)

	if len(result.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(result.ToolCalls))
	}

	// 验证顺序和内容
	if result.ToolCalls[0].Name != "read_file" {
		t.Errorf("first tool: got %q, want read_file", result.ToolCalls[0].Name)
	}
	if result.ToolCalls[1].Name != "bash" {
		t.Errorf("second tool: got %q, want bash", result.ToolCalls[1].Name)
	}

	var bashParams map[string]string
	json.Unmarshal(result.ToolCalls[1].Input, &bashParams)
	if bashParams["command"] != "go test ./..." {
		t.Errorf("bash command: got %q", bashParams["command"])
	}
}

// TestEmptyArgs 验证：tool call 没有参数的边界情况
func TestEmptyArgs(t *testing.T) {
	chunks := []mockChunk{
		{toolIdx: 0, toolID: "call_999", toolName: "git_status"},
		{finishReason: "tool_calls"},
	}
	result := runStateMachine(chunks)

	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	// 空参数应该返回 "{}" 而不是空字符串
	if string(result.ToolCalls[0].Input) != "{}" {
		t.Errorf("empty args: got %q, want {}", result.ToolCalls[0].Input)
	}
}
