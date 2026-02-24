package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepoMapTool_Basic(t *testing.T) {
	tmp := t.TempDir()
	src := `package demo

type Server struct{}

func Start() {}
`
	path := filepath.Join(tmp, "main.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &RepoMapTool{}
	params, _ := json.Marshal(map[string]any{
		"path":       tmp,
		"max_tokens": 1024,
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Repository map:") {
		t.Fatalf("expected repo map header, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "main.go") {
		t.Fatalf("expected file name in output, got: %s", result.Content)
	}
}

func TestSymbolNavTool_DefinitionAndReference(t *testing.T) {
	tmp := t.TempDir()
	src := `package demo

func Start() {}

func Run() {
	Start()
}
`
	path := filepath.Join(tmp, "service.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &SymbolNavTool{}
	params, _ := json.Marshal(map[string]any{
		"symbol": "Start",
		"path":   tmp,
		"mode":   "both",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Definitions (1)") {
		t.Fatalf("expected one definition, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "References (1)") {
		t.Fatalf("expected one reference, got: %s", result.Content)
	}
}

func TestDocContextTool_Validation(t *testing.T) {
	tool := NewDocContextTool("", "")
	params, _ := json.Marshal(map[string]any{})
	_, err := tool.Execute(context.Background(), params)
	if err == nil {
		t.Fatal("expected validation error for empty topic")
	}
}
