package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// ReadFileTool 读取文件内容
type ReadFileTool struct{}

func (t *ReadFileTool) Name() string        { return "read_file" }
func (t *ReadFileTool) IsReadOnly() bool     { return true }
func (t *ReadFileTool) PermissionLevel() PermissionLevel { return PermissionRead }

func (t *ReadFileTool) Description() string {
	return "Read the contents of a file at the given path. " +
		"Use offset and limit to read specific line ranges for large files."
}

func (t *ReadFileTool) Parameters() map[string]any {
	return map[string]any{
		"path": map[string]any{
			"type":        "string",
			"description": "Absolute path to the file to read",
		},
		"offset": map[string]any{
			"type":        "integer",
			"description": "Line number to start reading from (0-based, optional)",
		},
		"limit": map[string]any{
			"type":        "integer",
			"description": "Maximum number of lines to read (default 2000)",
		},
	}
}

func (t *ReadFileTool) Execute(_ context.Context, params json.RawMessage) (ToolResult, error) {
	var p struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("invalid params: %w", err)
	}
	if p.Path == "" {
		return ToolResult{}, fmt.Errorf("path is required")
	}
	if p.Limit <= 0 {
		p.Limit = 2000
	}

	data, err := os.ReadFile(p.Path)
	if err != nil {
		return ToolResult{}, fmt.Errorf("failed to read file: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	totalLines := len(lines)

	// Apply offset
	if p.Offset > 0 {
		if p.Offset >= totalLines {
			return ToolResult{Content: fmt.Sprintf("[File has %d lines, offset %d is beyond end]", totalLines, p.Offset)}, nil
		}
		lines = lines[p.Offset:]
	}

	// Apply limit with truncation notice
	truncated := false
	if len(lines) > p.Limit {
		lines = lines[:p.Limit]
		truncated = true
	}

	// Format with line numbers
	var sb strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&sb, "%6d\t%s\n", p.Offset+i+1, line)
	}

	if truncated {
		fmt.Fprintf(&sb, "[Truncated: %d total lines. Use offset/limit to read more.]", totalLines)
	}

	return ToolResult{Content: sb.String(), Truncated: truncated}, nil
}
