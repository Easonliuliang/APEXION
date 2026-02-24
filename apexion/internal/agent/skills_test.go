package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSkills(t *testing.T) {
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, ".apexion", "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatal(err)
	}

	content := "# Go Patterns\nCommon patterns for this project."
	if err := os.WriteFile(filepath.Join(skillsDir, "go-patterns.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	skills := loadSkillsFromDirs([]string{skillsDir})
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "go-patterns" {
		t.Fatalf("expected skill name 'go-patterns', got %q", skills[0].Name)
	}
	if skills[0].Path == "" {
		t.Fatal("expected non-empty path")
	}
}

func TestLoadSkillsDescFromFirstLine(t *testing.T) {
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, ".apexion", "skills")
	os.MkdirAll(skillsDir, 0755)

	// No frontmatter — description from first line (heading stripped)
	content := "# Deployment Guide\nHow to deploy the app."
	os.WriteFile(filepath.Join(skillsDir, "deploy.md"), []byte(content), 0644)

	skills := loadSkillsFromDirs([]string{skillsDir})
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Desc != "Deployment Guide" {
		t.Fatalf("expected desc 'Deployment Guide', got %q", skills[0].Desc)
	}
}

func TestLoadSkillsDescFromFrontmatter(t *testing.T) {
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, ".apexion", "skills")
	os.MkdirAll(skillsDir, 0755)

	content := `---
description: "API design guidelines"
---
# API Design
Content here.`
	os.WriteFile(filepath.Join(skillsDir, "api-design.md"), []byte(content), 0644)

	skills := loadSkillsFromDirs([]string{skillsDir})
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Desc != "API design guidelines" {
		t.Fatalf("expected desc 'API design guidelines', got %q", skills[0].Desc)
	}
}

func TestLoadSkillsDescTruncation(t *testing.T) {
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, ".apexion", "skills")
	os.MkdirAll(skillsDir, 0755)

	// First line longer than 60 chars — should be truncated
	longLine := strings.Repeat("a", 80)
	os.WriteFile(filepath.Join(skillsDir, "long.md"), []byte(longLine), 0644)

	skills := loadSkillsFromDirs([]string{skillsDir})
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if len(skills[0].Desc) > 61 { // 57 + "..."
		t.Fatalf("expected truncated desc, got length %d", len(skills[0].Desc))
	}
	if !strings.HasSuffix(skills[0].Desc, "...") {
		t.Fatalf("expected '...' suffix, got %q", skills[0].Desc)
	}
}

func TestLoadSkillsMultiple(t *testing.T) {
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, ".apexion", "skills")
	os.MkdirAll(skillsDir, 0755)

	os.WriteFile(filepath.Join(skillsDir, "alpha.md"), []byte("Alpha skill"), 0644)
	os.WriteFile(filepath.Join(skillsDir, "beta.md"), []byte("Beta skill"), 0644)
	os.WriteFile(filepath.Join(skillsDir, "not-skill.txt"), []byte("Ignored"), 0644)

	skills := loadSkillsFromDirs([]string{skillsDir})
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills (ignoring .txt), got %d", len(skills))
	}
}

func TestLoadSkillsEmpty(t *testing.T) {
	dir := t.TempDir()
	skills := loadSkillsFromDirs([]string{dir})
	if len(skills) != 0 {
		t.Fatalf("expected 0 skills, got %d", len(skills))
	}
}
