package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// EditFileTool edits files via exact string replacement.
type EditFileTool struct{}

func (t *EditFileTool) Name() string        { return "edit_file" }
func (t *EditFileTool) IsReadOnly() bool     { return false }
func (t *EditFileTool) PermissionLevel() PermissionLevel { return PermissionWrite }

func (t *EditFileTool) Description() string {
	return "Edit a file by replacing an exact string match. " +
		"The old_string must appear exactly once in the file."
}

func (t *EditFileTool) Parameters() map[string]any {
	return map[string]any{
		"file_path": map[string]any{
			"type":        "string",
			"description": "Absolute path to the file to edit",
		},
		"old_string": map[string]any{
			"type":        "string",
			"description": "The exact text to find and replace",
		},
		"new_string": map[string]any{
			"type":        "string",
			"description": "The replacement text",
		},
	}
}

func (t *EditFileTool) Execute(_ context.Context, params json.RawMessage) (ToolResult, error) {
	var p struct {
		FilePath  string `json:"file_path"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("invalid params: %w", err)
	}
	if p.FilePath == "" {
		return ToolResult{}, fmt.Errorf("file_path is required")
	}
	if p.OldString == "" {
		return ToolResult{}, fmt.Errorf("old_string is required")
	}

	data, err := os.ReadFile(p.FilePath)
	if err != nil {
		return ToolResult{}, fmt.Errorf("failed to read file: %w", err)
	}

	content := string(data)
	count := strings.Count(content, p.OldString)

	if count == 0 {
		return ToolResult{Content: "text not found in file", IsError: true}, nil
	}
	if count > 1 {
		return ToolResult{
			Content: fmt.Sprintf("found %d occurrences, provide more context to make the match unique", count),
			IsError: true,
		}, nil
	}

	newContent := strings.Replace(content, p.OldString, p.NewString, 1)
	if err := os.WriteFile(p.FilePath, []byte(newContent), 0644); err != nil {
		return ToolResult{}, fmt.Errorf("failed to write file: %w", err)
	}

	return ToolResult{Content: "file edited successfully"}, nil
}
