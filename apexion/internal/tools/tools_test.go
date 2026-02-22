package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apexion-ai/apexion/internal/permission"
)

// --- Registry tests ---

func TestDefaultRegistry_AllToolsRegistered(t *testing.T) {
	r := DefaultRegistry(nil, nil)
	expected := []string{
		"bash", "edit_file", "git_branch", "git_commit", "git_diff",
		"git_log", "git_push", "git_status", "glob", "grep",
		"list_dir", "question", "read_file", "task", "todo_read",
		"todo_write", "web_fetch", "web_search", "write_file",
	}
	all := r.All()
	if len(all) != len(expected) {
		t.Fatalf("expected %d tools, got %d", len(expected), len(all))
	}
	for i, tool := range all {
		if tool.Name() != expected[i] {
			t.Errorf("tool %d: expected %q, got %q", i, expected[i], tool.Name())
		}
	}
}

func TestRegistry_GetUnknown(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Error("expected Get to return false for unknown tool")
	}
}

// --- ReadFile tests ---

func TestReadFile_Basic(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.txt")
	os.WriteFile(path, []byte("line1\nline2\nline3\n"), 0644)

	tool := &ReadFileTool{}
	params, _ := json.Marshal(map[string]any{"path": path})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("unexpected tool error")
	}
	if !strings.Contains(result.Content, "line1") || !strings.Contains(result.Content, "line3") {
		t.Error("result should contain file content")
	}
}

func TestReadFile_WithOffset(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.txt")
	os.WriteFile(path, []byte("alpha\nbeta\ngamma\ndelta\nepsilon\n"), 0644)

	tool := &ReadFileTool{}
	params, _ := json.Marshal(map[string]any{"path": path, "offset": 2, "limit": 2})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "gamma") {
		t.Error("result should contain line starting at offset")
	}
	if strings.Contains(result.Content, "alpha") {
		t.Error("result should not contain lines before offset")
	}
}

func TestReadFile_MissingPath(t *testing.T) {
	tool := &ReadFileTool{}
	params, _ := json.Marshal(map[string]any{})
	_, err := tool.Execute(context.Background(), params)
	if err == nil {
		t.Error("expected error for missing path")
	}
}

func TestReadFile_NotFound(t *testing.T) {
	tool := &ReadFileTool{}
	params, _ := json.Marshal(map[string]any{"path": "/nonexistent/file.txt"})
	_, err := tool.Execute(context.Background(), params)
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

// --- EditFile tests ---

func TestEditFile_Basic(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.go")
	os.WriteFile(path, []byte("func hello() {\n\treturn\n}\n"), 0644)

	tool := &EditFileTool{}
	params, _ := json.Marshal(map[string]any{
		"file_path":  path,
		"old_string": "return",
		"new_string": "fmt.Println(\"hello\")",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content)
	}

	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "fmt.Println") {
		t.Error("file should contain new string")
	}
}

func TestEditFile_NotFound(t *testing.T) {
	tool := &EditFileTool{}
	params, _ := json.Marshal(map[string]any{
		"file_path":  "nonexistent.go",
		"old_string": "x",
		"new_string": "y",
	})
	_, err := tool.Execute(context.Background(), params)
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestEditFile_NoMatch(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.go")
	os.WriteFile(path, []byte("hello world\n"), 0644)

	tool := &EditFileTool{}
	params, _ := json.Marshal(map[string]any{
		"file_path":  path,
		"old_string": "not found string",
		"new_string": "replacement",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for no match")
	}
}

func TestEditFile_MultipleMatches(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.go")
	os.WriteFile(path, []byte("foo bar foo baz foo\n"), 0644)

	tool := &EditFileTool{}
	params, _ := json.Marshal(map[string]any{
		"file_path":  path,
		"old_string": "foo",
		"new_string": "qux",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for multiple matches")
	}
	if !strings.Contains(result.Content, "3 occurrences") {
		t.Errorf("expected message about 3 occurrences, got: %s", result.Content)
	}
}

// --- WriteFile tests ---

func TestWriteFile_Basic(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "new.txt")

	tool := &WriteFileTool{}
	params, _ := json.Marshal(map[string]any{
		"file_path": path,
		"content":   "hello world",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "hello world" {
		t.Errorf("expected 'hello world', got %q", string(data))
	}
}

func TestWriteFile_CreatesDirectories(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "a", "b", "c", "file.txt")

	tool := &WriteFileTool{}
	params, _ := json.Marshal(map[string]any{
		"file_path": path,
		"content":   "nested",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "nested" {
		t.Errorf("expected 'nested', got %q", string(data))
	}
}

// --- ListDir tests ---

func TestListDir_Basic(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "a.txt"), []byte("hello"), 0644)
	os.Mkdir(filepath.Join(tmp, "subdir"), 0755)

	tool := &ListDirTool{}
	params, _ := json.Marshal(map[string]any{"path": tmp})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "a.txt") {
		t.Error("result should contain file name")
	}
	if !strings.Contains(result.Content, "subdir") {
		t.Error("result should contain directory name")
	}
}

// --- Glob tests ---

func TestGlob_Basic(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "main.go"), []byte(""), 0644)
	os.WriteFile(filepath.Join(tmp, "test.txt"), []byte(""), 0644)

	tool := &GlobTool{}
	params, _ := json.Marshal(map[string]any{"pattern": "*.go", "path": tmp})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "main.go") {
		t.Error("result should contain matching .go file")
	}
	if strings.Contains(result.Content, "test.txt") {
		t.Error("result should not contain non-matching .txt file")
	}
}

// --- Executor tests ---

type allowAllPolicy struct{}

func (p *allowAllPolicy) Check(string, json.RawMessage) permission.Decision {
	return permission.Allow
}

type denyAllPolicy struct{}

func (p *denyAllPolicy) Check(string, json.RawMessage) permission.Decision {
	return permission.Deny
}

func TestExecutor_UnknownTool(t *testing.T) {
	r := NewRegistry()
	e := NewExecutor(r, &allowAllPolicy{})
	result := e.Execute(context.Background(), "nonexistent", nil)
	if !result.IsError {
		t.Error("expected error for unknown tool")
	}
	if !strings.Contains(result.Content, "unknown tool") {
		t.Errorf("expected 'unknown tool' message, got: %s", result.Content)
	}
}

func TestExecutor_PolicyDeny(t *testing.T) {
	r := DefaultRegistry(nil, nil)
	e := NewExecutor(r, &denyAllPolicy{})

	params, _ := json.Marshal(map[string]any{"command": "echo hi"})
	result := e.Execute(context.Background(), "bash", params)
	if !result.IsError {
		t.Error("expected error for denied tool")
	}
	if !strings.Contains(result.Content, "denied") {
		t.Errorf("expected 'denied' message, got: %s", result.Content)
	}
}

func TestExecutor_ReadFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.txt")
	os.WriteFile(path, []byte("content here\n"), 0644)

	r := DefaultRegistry(nil, nil)
	e := NewExecutor(r, &allowAllPolicy{})

	params, _ := json.Marshal(map[string]any{"path": path})
	result := e.Execute(context.Background(), "read_file", params)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "content here") {
		t.Error("result should contain file content")
	}
}

// --- Truncation tests ---

func TestToolOutputLimit(t *testing.T) {
	tests := []struct {
		name     string
		expected int
	}{
		{"read_file", 32 * 1024},
		{"grep", 32 * 1024},
		{"bash", 32 * 1024},
		{"web_fetch", 32 * 1024},
		{"web_search", 32 * 1024},
		{"git_diff", 16 * 1024},
		{"glob", 16 * 1024},
		{"list_dir", 16 * 1024},
		{"edit_file", 4 * 1024},
		{"write_file", 4 * 1024},
		{"git_commit", 4 * 1024},
		{"unknown_tool", 4 * 1024},
	}
	for _, tt := range tests {
		if got := toolOutputLimit(tt.name); got != tt.expected {
			t.Errorf("toolOutputLimit(%q) = %d, want %d", tt.name, got, tt.expected)
		}
	}
}

func TestTruncateHeadTail_NoTruncation(t *testing.T) {
	s := "short string"
	result := truncateHeadTail(s, 100)
	if result != s {
		t.Errorf("expected no truncation, got %q", result)
	}
}

func TestTruncateHeadTail_Truncates(t *testing.T) {
	s := strings.Repeat("x", 1000)
	result := truncateHeadTail(s, 100)

	if len(result) > 200 { // head + tail + omitted message
		t.Errorf("result too long: %d", len(result))
	}
	if !strings.Contains(result, "chars omitted") {
		t.Error("result should contain omitted message")
	}
	// Check head (60%) and tail (40%)
	if !strings.HasPrefix(result, strings.Repeat("x", 60)) {
		t.Error("result should start with head content")
	}
	if !strings.HasSuffix(result, strings.Repeat("x", 40)) {
		t.Error("result should end with tail content")
	}
}
