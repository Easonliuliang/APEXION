package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// WriteFileTool 写入文件内容
type WriteFileTool struct{}

func (t *WriteFileTool) Name() string        { return "write_file" }
func (t *WriteFileTool) IsReadOnly() bool     { return false }
func (t *WriteFileTool) PermissionLevel() PermissionLevel { return PermissionWrite }

func (t *WriteFileTool) Description() string {
	return "Write content to a file, creating parent directories if needed. " +
		"This will overwrite the file if it already exists."
}

func (t *WriteFileTool) Parameters() map[string]any {
	return map[string]any{
		"file_path": map[string]any{
			"type":        "string",
			"description": "Absolute path to the file to write",
		},
		"content": map[string]any{
			"type":        "string",
			"description": "The content to write to the file",
		},
	}
}

func (t *WriteFileTool) Execute(_ context.Context, params json.RawMessage) (ToolResult, error) {
	var p struct {
		FilePath string `json:"file_path"`
		Content  string `json:"content"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("invalid params: %w", err)
	}
	if p.FilePath == "" {
		return ToolResult{}, fmt.Errorf("file_path is required")
	}

	// Create parent directories
	dir := filepath.Dir(p.FilePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return ToolResult{}, fmt.Errorf("failed to create directories: %w", err)
	}

	if err := os.WriteFile(p.FilePath, []byte(p.Content), 0644); err != nil {
		return ToolResult{}, fmt.Errorf("failed to write file: %w", err)
	}

	return ToolResult{Content: fmt.Sprintf("wrote %d bytes to %s", len(p.Content), p.FilePath)}, nil
}
