package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSystemPrompt_EmbeddedDefaults(t *testing.T) {
	// Use a temp dir with no overrides — should load all embedded sections.
	tmpDir := t.TempDir()
	prompt := loadSystemPrompt(tmpDir)

	// Verify all sections are present.
	for _, section := range promptSections {
		if section == "identity" {
			// identity.md doesn't use XML tags.
			if !strings.Contains(prompt, "aictl") {
				t.Errorf("prompt missing identity section")
			}
			continue
		}
		// Other sections use XML-style tags.
		tagMap := map[string]string{
			"core":          "<core_principles>",
			"tools":         "<tool_strategy>",
			"communication": "<communication_style>",
			"safety":        "<safety_rules>",
			"errors":        "<error_handling>",
		}
		tag := tagMap[section]
		if !strings.Contains(prompt, tag) {
			t.Errorf("prompt missing section %q (looked for %q)", section, tag)
		}
	}

	// Verify sections are in the correct order.
	coreIdx := strings.Index(prompt, "<core_principles>")
	toolIdx := strings.Index(prompt, "<tool_strategy>")
	safetyIdx := strings.Index(prompt, "<safety_rules>")
	if coreIdx >= toolIdx || toolIdx >= safetyIdx {
		t.Error("sections are not in expected order: core < tools < safety")
	}
}

func TestLoadSystemPrompt_UserOverride(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a project-level override for the safety section.
	overrideDir := filepath.Join(tmpDir, ".aictl", "prompts")
	if err := os.MkdirAll(overrideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	customSafety := "<safety_rules>\nCustom safety rules for testing.\n</safety_rules>"
	if err := os.WriteFile(filepath.Join(overrideDir, "safety.md"), []byte(customSafety), 0o644); err != nil {
		t.Fatal(err)
	}

	prompt := loadSystemPrompt(tmpDir)

	// The custom safety content should replace the embedded default.
	if !strings.Contains(prompt, "Custom safety rules for testing") {
		t.Error("user override for safety section was not loaded")
	}

	// The default safety content should NOT be present.
	if strings.Contains(prompt, "rm -rf, curl | sh") {
		t.Error("embedded default safety section was not replaced by override")
	}

	// Other sections should still be present (from embedded defaults).
	if !strings.Contains(prompt, "<core_principles>") {
		t.Error("non-overridden section (core) is missing")
	}
}

func TestLoadSystemPrompt_ExtraMd(t *testing.T) {
	tmpDir := t.TempDir()

	// Create _extra.md in project override dir.
	overrideDir := filepath.Join(tmpDir, ".aictl", "prompts")
	if err := os.MkdirAll(overrideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	extra := "This is extra project-specific context appended to the prompt."
	if err := os.WriteFile(filepath.Join(overrideDir, "_extra.md"), []byte(extra), 0o644); err != nil {
		t.Fatal(err)
	}

	prompt := loadSystemPrompt(tmpDir)

	if !strings.Contains(prompt, extra) {
		t.Error("_extra.md content was not appended to prompt")
	}

	// _extra should come after the last section.
	lastSection := "<anti_hallucination>"
	extraIdx := strings.Index(prompt, extra)
	lastIdx := strings.Index(prompt, lastSection)
	if extraIdx <= lastIdx {
		t.Error("_extra.md content should appear after all sections")
	}
}

func TestLoadPromptSection_Fallback(t *testing.T) {
	// With no override dirs, should return embedded content.
	content := loadPromptSection("identity", nil)
	if !strings.Contains(content, "aictl") {
		t.Error("loadPromptSection should return embedded default when no overrides")
	}
}

func TestLoadPromptSection_NonexistentSection(t *testing.T) {
	content := loadPromptSection("nonexistent", nil)
	if content != "" {
		t.Errorf("expected empty string for nonexistent section, got %q", content)
	}
}
