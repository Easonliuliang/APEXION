package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// ListDirTool 列出目录内容
type ListDirTool struct{}

func (t *ListDirTool) Name() string        { return "list_dir" }
func (t *ListDirTool) IsReadOnly() bool     { return true }
func (t *ListDirTool) PermissionLevel() PermissionLevel { return PermissionRead }

func (t *ListDirTool) Description() string {
	return "List the contents of a directory, showing files and subdirectories with their sizes."
}

func (t *ListDirTool) Parameters() map[string]any {
	return map[string]any{
		"path": map[string]any{
			"type":        "string",
			"description": "Absolute path to the directory to list",
		},
	}
}

func (t *ListDirTool) Execute(_ context.Context, params json.RawMessage) (ToolResult, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("invalid params: %w", err)
	}
	if p.Path == "" {
		return ToolResult{}, fmt.Errorf("path is required")
	}

	entries, err := os.ReadDir(p.Path)
	if err != nil {
		return ToolResult{}, fmt.Errorf("failed to read directory: %w", err)
	}

	if len(entries) == 0 {
		return ToolResult{Content: "(empty directory)"}, nil
	}

	var sb strings.Builder
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if entry.IsDir() {
			fmt.Fprintf(&sb, "[DIR]  %s\n", entry.Name())
		} else {
			fmt.Fprintf(&sb, "[FILE] %s (%s)\n", entry.Name(), formatSize(info.Size()))
		}
	}

	return ToolResult{Content: sb.String()}, nil
}

func formatSize(bytes int64) string {
	switch {
	case bytes >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
	case bytes >= 1024:
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
