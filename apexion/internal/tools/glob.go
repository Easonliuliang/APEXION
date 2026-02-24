package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// GlobTool matches files using glob patterns.
type GlobTool struct{}

func (t *GlobTool) Name() string        { return "glob" }
func (t *GlobTool) IsReadOnly() bool     { return true }
func (t *GlobTool) PermissionLevel() PermissionLevel { return PermissionRead }

func (t *GlobTool) Description() string {
	return "Fast file pattern matching tool that works with any codebase size. " +
		"Supports glob patterns including ** for recursive matching (e.g. '**/*.go', 'src/**/*.ts'). " +
		"Returns matching file paths sorted by modification time (newest first). " +
		"Use this instead of bash find or ls."
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

const maxGlobResults = 1000

// Directories to skip during recursive traversal.
var globSkipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"__pycache__":  true,
	".next":        true,
	"dist":         true,
	"build":        true,
	"target":       true,
	".venv":        true,
	".tox":         true,
	".mypy_cache":  true,
	".pytest_cache": true,
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

	var matches []string
	var err error

	if strings.Contains(p.Pattern, "**") {
		matches, err = globRecursive(p.Path, p.Pattern)
	} else {
		// Original behavior: simple filepath.Glob
		fullPattern := filepath.Join(p.Path, p.Pattern)
		matches, err = filepath.Glob(fullPattern)
	}

	if err != nil {
		return ToolResult{}, fmt.Errorf("invalid glob pattern: %w", err)
	}
	if len(matches) == 0 {
		return ToolResult{Content: "no files matched"}, nil
	}

	// Sort by modification time (newest first).
	sortByModTime(matches)

	truncated := false
	if len(matches) > maxGlobResults {
		matches = matches[:maxGlobResults]
		truncated = true
	}

	content := strings.Join(matches, "\n")
	if truncated {
		content += fmt.Sprintf("\n[Truncated: showing first %d results]", maxGlobResults)
	}

	return ToolResult{Content: content, Truncated: truncated}, nil
}

// globRecursive handles patterns containing ** by walking the directory tree
// and matching each file against the pattern.
func globRecursive(basePath, pattern string) ([]string, error) {
	// Split pattern into segments on "**".
	// E.g. "**/*.go"       → prefix="", suffix="*.go"
	//      "src/**/*.go"   → prefix="src", suffix="*.go"
	//      "**"            → prefix="", suffix=""
	parts := strings.SplitN(pattern, "**", 2)
	prefix := strings.TrimRight(parts[0], "/\\")
	suffix := ""
	if len(parts) > 1 {
		suffix = strings.TrimLeft(parts[1], "/\\")
	}

	// Determine walk root.
	root := basePath
	if prefix != "" {
		root = filepath.Join(basePath, prefix)
	}

	// Check root exists.
	if _, err := os.Stat(root); err != nil {
		return nil, nil // no matches, not an error
	}

	var matches []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible
		}

		// Skip well-known large/irrelevant directories.
		if d.IsDir() {
			if globSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		// Match the file against the suffix pattern.
		if suffix == "" {
			// "**" alone matches every file.
			matches = append(matches, path)
		} else {
			// Match the filename against the suffix.
			// This handles "**/*.go" — suffix is "*.go", match against filename.
			// For deeper suffix like "**/**/foo.go", fall back to matching
			// the relative path from root.
			if strings.Contains(suffix, "/") || strings.Contains(suffix, string(os.PathSeparator)) {
				// Suffix contains path separators — match against relative path from root.
				rel, relErr := filepath.Rel(root, path)
				if relErr != nil {
					return nil
				}
				matched, matchErr := filepath.Match(suffix, rel)
				if matchErr != nil {
					return nil
				}
				if matched {
					matches = append(matches, path)
				}
			} else {
				// Simple suffix — match against filename only.
				matched, matchErr := filepath.Match(suffix, d.Name())
				if matchErr != nil {
					return nil
				}
				if matched {
					matches = append(matches, path)
				}
			}
		}

		return nil
	})

	return matches, err
}

// sortByModTime sorts file paths by modification time, newest first.
// Files that cannot be stat'd are sorted to the end.
func sortByModTime(paths []string) {
	type fileWithTime struct {
		path    string
		modTime int64
	}

	files := make([]fileWithTime, len(paths))
	for i, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			files[i] = fileWithTime{path: p, modTime: 0}
		} else {
			files[i] = fileWithTime{path: p, modTime: info.ModTime().UnixNano()}
		}
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime > files[j].modTime
	})

	for i, f := range files {
		paths[i] = f.path
	}
}
