package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Tool 是所有工具的统一接口
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]any
	Execute(ctx context.Context, params json.RawMessage) (string, error)
}

// ToolRegistry 管理所有已注册工具
type ToolRegistry struct {
	tools map[string]Tool
}

func NewToolRegistry() *ToolRegistry {
	r := &ToolRegistry{tools: make(map[string]Tool)}
	r.Register(&ReadFileTool{})
	r.Register(&BashTool{})
	return r
}

func (r *ToolRegistry) Register(t Tool) {
	r.tools[t.Name()] = t
}

func (r *ToolRegistry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// ToSchema 将所有工具转换为 Anthropic tools 参数格式
func (r *ToolRegistry) ToSchema() []map[string]any {
	var schemas []map[string]any
	for _, t := range r.tools {
		schemas = append(schemas, map[string]any{
			"name":        t.Name(),
			"description": t.Description(),
			"input_schema": map[string]any{
				"type":       "object",
				"properties": t.Parameters(),
			},
		})
	}
	return schemas
}

// ─── ReadFile 工具 ────────────────────────────────────────────────────────────

type ReadFileTool struct{}

func (t *ReadFileTool) Name() string { return "read_file" }

func (t *ReadFileTool) Description() string {
	return "Read the contents of a file at the given path. Returns the file content as a string."
}

func (t *ReadFileTool) Parameters() map[string]any {
	return map[string]any{
		"path": map[string]any{
			"type":        "string",
			"description": "Absolute or relative path to the file to read",
		},
	}
}

func (t *ReadFileTool) Execute(_ context.Context, params json.RawMessage) (string, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return "", fmt.Errorf("invalid params: %w", err)
	}
	if p.Path == "" {
		return "", fmt.Errorf("path is required")
	}

	data, err := os.ReadFile(p.Path)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	const maxBytes = 100 * 1024 // 100KB 截断
	if len(data) > maxBytes {
		return string(data[:maxBytes]) + "\n[Truncated: file too large]", nil
	}
	return string(data), nil
}

// ─── Bash 工具 ────────────────────────────────────────────────────────────────

type BashTool struct{}

func (t *BashTool) Name() string { return "bash" }

func (t *BashTool) Description() string {
	return "Execute a shell command and return its stdout and stderr output."
}

func (t *BashTool) Parameters() map[string]any {
	return map[string]any{
		"command": map[string]any{
			"type":        "string",
			"description": "The shell command to execute",
		},
	}
}

func (t *BashTool) Execute(ctx context.Context, params json.RawMessage) (string, error) {
	var p struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return "", fmt.Errorf("invalid params: %w", err)
	}
	if p.Command == "" {
		return "", fmt.Errorf("command is required")
	}

	// 询问用户确认（POC 阶段简单实现）
	fmt.Printf("\n⚠️  Execute command: %s\n   [y/N] ", p.Command)
	var answer string
	fmt.Scanln(&answer)
	if strings.ToLower(strings.TrimSpace(answer)) != "y" {
		return "", fmt.Errorf("command execution cancelled by user")
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", p.Command)
	out, err := cmd.CombinedOutput()
	result := string(out)

	const maxBytes = 50 * 1024
	if len(result) > maxBytes {
		result = result[:maxBytes] + "\n[Truncated: output too large]"
	}

	if err != nil {
		// 命令执行失败时，把 stderr 也返回给 LLM，让它决定下一步
		return fmt.Sprintf("Exit error: %v\nOutput:\n%s", err, result), nil
	}
	return result, nil
}
