package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// GlobTool 使用 glob 模式匹配文件
type GlobTool struct{}

func (t *GlobTool) Name() string        { return "glob" }
func (t *GlobTool) IsReadOnly() bool     { return true }
func (t *GlobTool) PermissionLevel() PermissionLevel { return PermissionRead }

func (t *GlobTool) Description() string {
	return "Find files matching a glob pattern. Returns a list of matching file paths."
}

func (t *GlobTool) Parameters() map[string]any {
	return map[string]any{
		"pattern": map[string]any{
			"type":        "string",
			"description": "Glob pattern to match files (e.g. '**/*.go', 'src/*.ts')",
		},
		"path": map[string]any{
			"type":        "string",
			"description": "Base directory to search in (default: current directory)",
		},
	}
}

func (t *GlobTool) Execute(_ context.Context, params json.RawMessage) (ToolResult, error) {
	var p struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("invalid params: %w", err)
	}
	if p.Pattern == "" {
		return ToolResult{}, fmt.Errorf("pattern is required")
	}
	if p.Path == "" {
		p.Path = "."
	}

	fullPattern := filepath.Join(p.Path, p.Pattern)
	matches, err := filepath.Glob(fullPattern)
	if err != nil {
		return ToolResult{}, fmt.Errorf("invalid glob pattern: %w", err)
	}

	if len(matches) == 0 {
		return ToolResult{Content: "no files matched"}, nil
	}

	return ToolResult{Content: strings.Join(matches, "\n")}, nil
}
