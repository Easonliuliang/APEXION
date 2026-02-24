package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// EditFileTool edits files via exact string replacement.
type EditFileTool struct{}

func (t *EditFileTool) Name() string                      { return "edit_file" }
func (t *EditFileTool) IsReadOnly() bool                   { return false }
func (t *EditFileTool) PermissionLevel() PermissionLevel   { return PermissionWrite }

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

	if count == 1 {
		newContent := strings.Replace(content, p.OldString, p.NewString, 1)
		if err := os.WriteFile(p.FilePath, []byte(newContent), 0644); err != nil {
			return ToolResult{}, fmt.Errorf("failed to write file: %w", err)
		}
		return ToolResult{Content: "file edited successfully"}, nil
	}
	if count > 1 {
		return ToolResult{
			Content: fmt.Sprintf("found %d occurrences, provide more context to make the match unique", count),
			IsError: true,
		}, nil
	}

	// count == 0: try fuzzy fallback.
	if result, ok := fuzzyReplace(content, p.OldString, p.NewString); ok {
		if err := os.WriteFile(p.FilePath, []byte(result), 0644); err != nil {
			return ToolResult{}, fmt.Errorf("failed to write file: %w", err)
		}
		return ToolResult{Content: "file edited successfully (fuzzy match)"}, nil
	}

	return ToolResult{Content: "text not found in file", IsError: true}, nil
}

// ── Fuzzy matching ──────────────────────────────────────────────────────────

// fuzzyReplace tries 3 normalization layers to find a unique line-range match:
//  1. Trailing whitespace: strip trailing spaces/tabs per line
//  2. Indentation: additionally normalize tabs → 4 spaces
//  3. Blank lines: additionally collapse consecutive blank lines
//
// Layers 1–2 use line-sliding (preserves original content outside the match).
// Layer 3 falls back to full-string normalized replacement.
func fuzzyReplace(content, oldString, newString string) (string, bool) {
	contentLines := strings.Split(content, "\n")
	oldLines := strings.Split(oldString, "\n")

	// Layer 1: trailing whitespace.
	trimWS := func(s string) string { return strings.TrimRight(s, " \t") }
	if start, ok := fuzzyLineMatch(contentLines, oldLines, trimWS); ok {
		return replaceLineRange(contentLines, start, len(oldLines), newString), true
	}

	// Layer 2: indentation (tab ↔ spaces).
	normIndent := func(s string) string {
		return strings.ReplaceAll(strings.TrimRight(s, " \t"), "\t", "    ")
	}
	if start, ok := fuzzyLineMatch(contentLines, oldLines, normIndent); ok {
		return replaceLineRange(contentLines, start, len(oldLines), newString), true
	}

	// Layer 3: blank line normalization (changes line count, use string-level match).
	normAll := func(s string) string {
		return blankLineRe.ReplaceAllString(
			strings.ReplaceAll(normalizeTrailingWS(s), "\t", "    "),
			"\n\n")
	}
	nc, no := normAll(content), normAll(oldString)
	if strings.Count(nc, no) == 1 {
		return strings.Replace(nc, no, newString, 1), true
	}

	return "", false
}

// fuzzyLineMatch slides a window of len(oldLines) over contentLines,
// comparing with the given per-line normalizer. Returns the start index
// if exactly one match is found.
func fuzzyLineMatch(contentLines, oldLines []string, normalize func(string) string) (int, bool) {
	normOld := make([]string, len(oldLines))
	for i, l := range oldLines {
		normOld[i] = normalize(l)
	}

	matchStart, matchCount := -1, 0
	for i := 0; i <= len(contentLines)-len(normOld); i++ {
		match := true
		for j, want := range normOld {
			if normalize(contentLines[i+j]) != want {
				match = false
				break
			}
		}
		if match {
			matchCount++
			matchStart = i
			if matchCount > 1 {
				return -1, false
			}
		}
	}
	return matchStart, matchCount == 1
}

// replaceLineRange replaces lines[start:start+count] with newString's lines.
func replaceLineRange(lines []string, start, count int, newString string) string {
	var parts []string
	parts = append(parts, lines[:start]...)
	parts = append(parts, strings.Split(newString, "\n")...)
	parts = append(parts, lines[start+count:]...)
	return strings.Join(parts, "\n")
}

// normalizeTrailingWS strips trailing spaces/tabs from every line in s.
func normalizeTrailingWS(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.Join(lines, "\n")
}

// blankLineRe matches two or more consecutive newlines (with optional whitespace-only lines).
var blankLineRe = regexp.MustCompile(`\n[ \t]*\n([ \t]*\n)*`)
