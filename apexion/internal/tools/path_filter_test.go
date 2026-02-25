package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGrepSkipsBenchmarkArtifacts(t *testing.T) {
	tmp := t.TempDir()
	artifactDir := filepath.Join(tmp, "benchmark", "ab", "results", "run1")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatal(err)
	}
	codeDir := filepath.Join(tmp, "internal", "agent")
	if err := os.MkdirAll(codeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	const symbol = "runAgentLoop"
	if err := os.WriteFile(filepath.Join(artifactDir, "trace.jsonl"), []byte(symbol+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	codePath := filepath.Join(codeDir, "loop.go")
	if err := os.WriteFile(codePath, []byte("func "+symbol+"() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := &GrepTool{}
	params, _ := json.Marshal(map[string]any{
		"pattern": symbol,
		"path":    tmp,
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "internal/agent/loop.go") {
		t.Fatalf("expected grep to find source file, got: %s", result.Content)
	}
	if strings.Contains(result.Content, "benchmark/ab/results") {
		t.Fatalf("expected benchmark artifacts to be excluded, got: %s", result.Content)
	}
}

func TestGlobSkipsBenchmarkArtifacts(t *testing.T) {
	tmp := t.TempDir()
	artifactDir := filepath.Join(tmp, "benchmark", "ab", "results", "run1")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatal(err)
	}
	srcDir := filepath.Join(tmp, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(artifactDir, "artifact.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := &GlobTool{}
	params, _ := json.Marshal(map[string]any{
		"pattern": "**/*.go",
		"path":    tmp,
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "src/main.go") {
		t.Fatalf("expected src/main.go in results, got: %s", result.Content)
	}
	if strings.Contains(result.Content, "benchmark/ab/results") {
		t.Fatalf("expected benchmark artifacts to be excluded, got: %s", result.Content)
	}
}
