package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProjectContext(t *testing.T) {
	dir := t.TempDir()
	content := "# My Project\nTest rules here"
	if err := os.WriteFile(filepath.Join(dir, "APEXION.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := loadProjectContext(dir)
	if ctx == "" {
		t.Fatal("expected non-empty context")
	}
	if !strings.Contains(ctx, "<project_context>") {
		t.Error("context should be wrapped in <project_context> tags")
	}
	if !strings.Contains(ctx, "</project_context>") {
		t.Error("context should have closing </project_context> tag")
	}
	if !strings.Contains(ctx, "My Project") {
		t.Error("context should contain file content")
	}
	if !strings.Contains(ctx, "APEXION.md") {
		t.Error("context should mention source file path")
	}
}

func TestLoadProjectContextAgentsMD(t *testing.T) {
	dir := t.TempDir()
	content := "# Agents Config\nSome agent instructions"
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := loadProjectContext(dir)
	if ctx == "" {
		t.Fatal("expected non-empty context from AGENTS.md")
	}
	if !strings.Contains(ctx, "Agents Config") {
		t.Error("context should contain AGENTS.md content")
	}
}

func TestLoadProjectContextDotApexion(t *testing.T) {
	dir := t.TempDir()
	apexionDir := filepath.Join(dir, ".apexion")
	if err := os.MkdirAll(apexionDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "Context from .apexion/context.md"
	if err := os.WriteFile(filepath.Join(apexionDir, "context.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := loadProjectContext(dir)
	if ctx == "" {
		t.Fatal("expected non-empty context from .apexion/context.md")
	}
	if !strings.Contains(ctx, "Context from .apexion/context.md") {
		t.Error("context should contain .apexion/context.md content")
	}
}

func TestLoadProjectContextEmpty(t *testing.T) {
	dir := t.TempDir()
	ctx := loadProjectContext(dir)
	if ctx != "" {
		t.Fatalf("expected empty context for dir without context files, got %q", ctx)
	}
}

func TestLoadProjectContextEmptyFile(t *testing.T) {
	dir := t.TempDir()
	// Write an empty APEXION.md
	if err := os.WriteFile(filepath.Join(dir, "APEXION.md"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := loadProjectContext(dir)
	if ctx != "" {
		t.Fatalf("expected empty context for empty file, got %q", ctx)
	}
}

func TestFindGitRoot(t *testing.T) {
	// Use the current repo — we know we're in a git repo
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := findGitRoot(cwd)
	if root == "" {
		t.Fatal("expected non-empty git root")
	}
	// Git root should contain .git
	if _, err := os.Stat(filepath.Join(root, ".git")); os.IsNotExist(err) {
		t.Fatalf("git root %q does not contain .git", root)
	}
}

func TestFindGitRootNonRepo(t *testing.T) {
	dir := t.TempDir()
	root := findGitRoot(dir)
	if root != "" {
		t.Fatalf("expected empty git root for non-repo dir, got %q", root)
	}
}

func TestReadContextFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	content := "Hello, world!"
	os.WriteFile(path, []byte(content), 0644)

	got := readContextFile(path)
	if got != content {
		t.Fatalf("expected %q, got %q", content, got)
	}
}

func TestReadContextFileNotExist(t *testing.T) {
	got := readContextFile("/nonexistent/path/file.md")
	if got != "" {
		t.Fatalf("expected empty for nonexistent file, got %q", got)
	}
}

func TestReadContextFileTruncation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.md")

	// Create content larger than maxFileBytes (8KB)
	big := strings.Repeat("x", 10*1024)
	os.WriteFile(path, []byte(big), 0644)

	got := readContextFile(path)
	if len(got) > maxFileBytes+100 { // allow for truncation notice
		t.Fatalf("expected truncation at ~%d bytes, got %d", maxFileBytes, len(got))
	}
	if !strings.Contains(got, "[Truncated") {
		t.Error("expected truncation notice")
	}
}
