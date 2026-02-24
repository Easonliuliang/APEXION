package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGlob_SimplePattern(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "main.go"), []byte(""), 0644)
	os.WriteFile(filepath.Join(tmp, "util.go"), []byte(""), 0644)
	os.WriteFile(filepath.Join(tmp, "readme.md"), []byte(""), 0644)

	tool := &GlobTool{}
	params, _ := json.Marshal(map[string]any{"pattern": "*.go", "path": tmp})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "main.go") {
		t.Error("should match main.go")
	}
	if !strings.Contains(result.Content, "util.go") {
		t.Error("should match util.go")
	}
	if strings.Contains(result.Content, "readme.md") {
		t.Error("should not match readme.md")
	}
}

func TestGlob_RecursiveDoublestar(t *testing.T) {
	tmp := t.TempDir()

	// Create nested structure
	dirs := []string{
		filepath.Join(tmp, "src"),
		filepath.Join(tmp, "src", "pkg"),
		filepath.Join(tmp, "src", "pkg", "deep"),
	}
	for _, d := range dirs {
		os.MkdirAll(d, 0755)
	}
	os.WriteFile(filepath.Join(tmp, "main.go"), []byte(""), 0644)
	os.WriteFile(filepath.Join(tmp, "src", "handler.go"), []byte(""), 0644)
	os.WriteFile(filepath.Join(tmp, "src", "pkg", "util.go"), []byte(""), 0644)
	os.WriteFile(filepath.Join(tmp, "src", "pkg", "deep", "deep.go"), []byte(""), 0644)
	os.WriteFile(filepath.Join(tmp, "src", "style.css"), []byte(""), 0644)

	tool := &GlobTool{}
	params, _ := json.Marshal(map[string]any{"pattern": "**/*.go", "path": tmp})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, f := range []string{"main.go", "handler.go", "util.go", "deep.go"} {
		if !strings.Contains(result.Content, f) {
			t.Errorf("should match %s", f)
		}
	}
	if strings.Contains(result.Content, "style.css") {
		t.Error("should not match style.css")
	}
}

func TestGlob_RecursiveWithPrefix(t *testing.T) {
	tmp := t.TempDir()

	os.MkdirAll(filepath.Join(tmp, "src", "sub"), 0755)
	os.MkdirAll(filepath.Join(tmp, "other"), 0755)
	os.WriteFile(filepath.Join(tmp, "src", "a.go"), []byte(""), 0644)
	os.WriteFile(filepath.Join(tmp, "src", "sub", "b.go"), []byte(""), 0644)
	os.WriteFile(filepath.Join(tmp, "other", "c.go"), []byte(""), 0644)

	tool := &GlobTool{}
	params, _ := json.Marshal(map[string]any{"pattern": "src/**/*.go", "path": tmp})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result.Content, "a.go") {
		t.Error("should match src/a.go")
	}
	if !strings.Contains(result.Content, "b.go") {
		t.Error("should match src/sub/b.go")
	}
	if strings.Contains(result.Content, "c.go") {
		t.Error("should not match other/c.go")
	}
}

func TestGlob_SkipsDotGit(t *testing.T) {
	tmp := t.TempDir()

	os.MkdirAll(filepath.Join(tmp, ".git", "objects"), 0755)
	os.WriteFile(filepath.Join(tmp, ".git", "config"), []byte(""), 0644)
	os.WriteFile(filepath.Join(tmp, "main.go"), []byte(""), 0644)

	tool := &GlobTool{}
	params, _ := json.Marshal(map[string]any{"pattern": "**/*", "path": tmp})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(result.Content, ".git") {
		t.Error("should skip .git directory")
	}
	if !strings.Contains(result.Content, "main.go") {
		t.Error("should match main.go")
	}
}

func TestGlob_SkipsNodeModules(t *testing.T) {
	tmp := t.TempDir()

	os.MkdirAll(filepath.Join(tmp, "node_modules", "pkg"), 0755)
	os.WriteFile(filepath.Join(tmp, "node_modules", "pkg", "index.js"), []byte(""), 0644)
	os.WriteFile(filepath.Join(tmp, "app.js"), []byte(""), 0644)

	tool := &GlobTool{}
	params, _ := json.Marshal(map[string]any{"pattern": "**/*.js", "path": tmp})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(result.Content, "node_modules") {
		t.Error("should skip node_modules directory")
	}
	if !strings.Contains(result.Content, "app.js") {
		t.Error("should match app.js")
	}
}

func TestGlob_SortByModTime(t *testing.T) {
	tmp := t.TempDir()

	// Create files with different mod times.
	older := filepath.Join(tmp, "older.go")
	newer := filepath.Join(tmp, "newer.go")

	os.WriteFile(older, []byte(""), 0644)
	os.Chtimes(older, time.Now().Add(-1*time.Hour), time.Now().Add(-1*time.Hour))
	os.WriteFile(newer, []byte(""), 0644)
	// newer gets current time by default.

	tool := &GlobTool{}
	params, _ := json.Marshal(map[string]any{"pattern": "**/*.go", "path": tmp})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(result.Content), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 results, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "newer.go") {
		t.Errorf("newest file should be first, got %q", lines[0])
	}
	if !strings.Contains(lines[1], "older.go") {
		t.Errorf("older file should be second, got %q", lines[1])
	}
}

func TestGlob_NoMatches(t *testing.T) {
	tmp := t.TempDir()

	tool := &GlobTool{}
	params, _ := json.Marshal(map[string]any{"pattern": "**/*.xyz", "path": tmp})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "no files matched" {
		t.Errorf("expected 'no files matched', got %q", result.Content)
	}
}

func TestGlob_MissingPattern(t *testing.T) {
	tool := &GlobTool{}
	params, _ := json.Marshal(map[string]any{})
	_, err := tool.Execute(context.Background(), params)
	if err == nil {
		t.Error("expected error for missing pattern")
	}
}

func TestGlob_DefaultPath(t *testing.T) {
	tool := &GlobTool{}
	// This should use "." as the base path — just verify it doesn't crash.
	params, _ := json.Marshal(map[string]any{"pattern": "*.go"})
	_, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGlob_DoublestarAlone(t *testing.T) {
	tmp := t.TempDir()

	os.MkdirAll(filepath.Join(tmp, "sub"), 0755)
	os.WriteFile(filepath.Join(tmp, "a.txt"), []byte(""), 0644)
	os.WriteFile(filepath.Join(tmp, "sub", "b.txt"), []byte(""), 0644)

	tool := &GlobTool{}
	params, _ := json.Marshal(map[string]any{"pattern": "**", "path": tmp})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result.Content, "a.txt") {
		t.Error("should match a.txt")
	}
	if !strings.Contains(result.Content, "b.txt") {
		t.Error("should match sub/b.txt")
	}
}
