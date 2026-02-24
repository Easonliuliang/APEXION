package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRules(t *testing.T) {
	dir := t.TempDir()
	rulesDir := filepath.Join(dir, ".apexion", "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatal(err)
	}

	content := "Always use gofmt before committing."
	if err := os.WriteFile(filepath.Join(rulesDir, "style.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	rules := loadRules(dir)
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if rules[0].Name != "style" {
		t.Fatalf("expected rule name 'style', got %q", rules[0].Name)
	}
	if rules[0].Content != content {
		t.Fatalf("expected content %q, got %q", content, rules[0].Content)
	}
	if rules[0].Source == "" {
		t.Fatal("expected non-empty source path")
	}
}

func TestLoadRulesMultiple(t *testing.T) {
	dir := t.TempDir()
	rulesDir := filepath.Join(dir, ".apexion", "rules")
	os.MkdirAll(rulesDir, 0755)

	os.WriteFile(filepath.Join(rulesDir, "style.md"), []byte("Style rules"), 0644)
	os.WriteFile(filepath.Join(rulesDir, "testing.md"), []byte("Testing rules"), 0644)
	os.WriteFile(filepath.Join(rulesDir, "not-a-rule.txt"), []byte("Ignored"), 0644) // non-.md

	rules := loadRules(dir)
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules (ignoring .txt), got %d", len(rules))
	}
}

func TestLoadRulesWithFrontmatter(t *testing.T) {
	dir := t.TempDir()
	rulesDir := filepath.Join(dir, ".apexion", "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatal(err)
	}

	content := `---
description: "Go coding conventions"
path_patterns:
  - "*.go"
  - "internal/**"
---
Use table-driven tests.`
	if err := os.WriteFile(filepath.Join(rulesDir, "go-style.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	rules := loadRules(dir)
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	r := rules[0]
	if r.Name != "go-style" {
		t.Fatalf("expected name 'go-style', got %q", r.Name)
	}
	if r.Description != "Go coding conventions" {
		t.Fatalf("expected description 'Go coding conventions', got %q", r.Description)
	}
	if len(r.PathPatterns) != 2 {
		t.Fatalf("expected 2 path patterns, got %d", len(r.PathPatterns))
	}
	if r.PathPatterns[0] != "*.go" || r.PathPatterns[1] != "internal/**" {
		t.Fatalf("unexpected path patterns: %v", r.PathPatterns)
	}
	if r.Content != "Use table-driven tests." {
		t.Fatalf("expected content 'Use table-driven tests.', got %q", r.Content)
	}
}

func TestLoadRulesDescriptionOnly(t *testing.T) {
	dir := t.TempDir()
	rulesDir := filepath.Join(dir, ".apexion", "rules")
	os.MkdirAll(rulesDir, 0755)

	content := `---
description: "Security rules"
---
Never commit secrets.`
	os.WriteFile(filepath.Join(rulesDir, "security.md"), []byte(content), 0644)

	rules := loadRules(dir)
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if rules[0].Description != "Security rules" {
		t.Fatalf("expected description 'Security rules', got %q", rules[0].Description)
	}
	if len(rules[0].PathPatterns) != 0 {
		t.Fatalf("expected 0 path patterns, got %d", len(rules[0].PathPatterns))
	}
}

func TestLoadRulesEmpty(t *testing.T) {
	dir := t.TempDir()
	rules := loadRules(dir)
	if len(rules) != 0 {
		t.Fatalf("expected 0 rules, got %d", len(rules))
	}
}

func TestLoadRulesSkipsEmptyFiles(t *testing.T) {
	dir := t.TempDir()
	rulesDir := filepath.Join(dir, ".apexion", "rules")
	os.MkdirAll(rulesDir, 0755)

	os.WriteFile(filepath.Join(rulesDir, "empty.md"), []byte(""), 0644)
	os.WriteFile(filepath.Join(rulesDir, "whitespace.md"), []byte("   \n  \n"), 0644)

	rules := loadRules(dir)
	if len(rules) != 0 {
		t.Fatalf("expected 0 rules for empty files, got %d", len(rules))
	}
}
