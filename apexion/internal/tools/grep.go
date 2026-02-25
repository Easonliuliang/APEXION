package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// GrepTool recursively searches file contents.
type GrepTool struct{}

func (t *GrepTool) Name() string                     { return "grep" }
func (t *GrepTool) IsReadOnly() bool                 { return true }
func (t *GrepTool) PermissionLevel() PermissionLevel { return PermissionRead }

func (t *GrepTool) Description() string {
	return "Recursively search file contents using a regex pattern. " +
		"Returns matching lines in 'file:line:content' format (max 50 results). " +
		"ALWAYS use this tool for content search. Do NOT use bash grep or rg."
}

func (t *GrepTool) Parameters() map[string]any {
	return map[string]any{
		"pattern": map[string]any{
			"type":        "string",
			"description": "Regular expression pattern to search for",
		},
		"path": map[string]any{
			"type":        "string",
			"description": "Directory or file to search in (default: current directory)",
		},
		"glob": map[string]any{
			"type":        "string",
			"description": "File glob filter (e.g. '*.go', '*.ts')",
		},
		"case_insensitive": map[string]any{
			"type":        "boolean",
			"description": "Whether to ignore case (default: false)",
		},
	}
}

const maxGrepResults = 50

func (t *GrepTool) Execute(_ context.Context, params json.RawMessage) (ToolResult, error) {
	var p struct {
		Pattern         string `json:"pattern"`
		Path            string `json:"path"`
		Glob            string `json:"glob"`
		CaseInsensitive bool   `json:"case_insensitive"`
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

	regexPattern := p.Pattern
	if p.CaseInsensitive {
		regexPattern = "(?i)" + regexPattern
	}
	re, err := regexp.Compile(regexPattern)
	if err != nil {
		return ToolResult{}, fmt.Errorf("invalid regex pattern: %w", err)
	}

	var results []string

	err = filepath.Walk(p.Path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible files
		}
		if info.IsDir() {
			if path != p.Path && shouldSkipDir(path, info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if shouldSkipFilePath(path, info) {
			return nil
		}
		// Apply glob filter
		if p.Glob != "" {
			matched, _ := filepath.Match(p.Glob, info.Name())
			if !matched {
				return nil
			}
		}

		if err := searchFile(path, re, &results); err != nil {
			return nil // skip files we can't read
		}
		if len(results) >= maxGrepResults {
			return fmt.Errorf("limit reached")
		}
		return nil
	})

	// Ignore "limit reached" error
	if err != nil && err.Error() != "limit reached" {
		return ToolResult{}, fmt.Errorf("search failed: %w", err)
	}

	if len(results) == 0 {
		return ToolResult{Content: "no matches found"}, nil
	}

	content := strings.Join(results, "\n")
	truncated := len(results) >= maxGrepResults
	if truncated {
		content += fmt.Sprintf("\n[Truncated: showing first %d results]", maxGrepResults)
	}

	return ToolResult{Content: content, Truncated: truncated}, nil
}

func searchFile(path string, re *regexp.Regexp, results *[]string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if re.MatchString(line) {
			*results = append(*results, fmt.Sprintf("%s:%d:%s", path, lineNum, line))
			if len(*results) >= maxGrepResults {
				return nil
			}
		}
	}
	return scanner.Err()
}
