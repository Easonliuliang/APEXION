package agent

import (
	"embed"
	"os"
	"path/filepath"
	"strings"
)

//go:embed prompts/*.md
var defaultPromptFS embed.FS

// promptSections defines the section names and their assembly order.
// Each name corresponds to a file "{name}.md" in the embedded prompts/ directory.
var promptSections = []string{
	"identity",
	"core",
	"tools",
	"communication",
	"safety",
	"errors",
}

// loadSystemPrompt assembles the system prompt from embedded defaults and user overrides.
// Override paths (in priority order, higher wins):
//
//	~/.config/aictl/prompts/{section}.md   — global user override
//	{gitRoot}/.aictl/prompts/{section}.md  — project-level override
//
// If a user file exists for a section, it replaces the embedded default for that section.
// A special "_extra.md" file in any override directory is appended after all sections.
func loadSystemPrompt(cwd string) string {
	gitRoot := findGitRoot(cwd)
	overrideDirs := promptOverrideDirs(cwd, gitRoot)

	var sections []string
	for _, name := range promptSections {
		content := loadPromptSection(name, overrideDirs)
		if content != "" {
			sections = append(sections, content)
		}
	}

	result := strings.Join(sections, "\n\n")

	// Append _extra.md from each override directory (if present).
	for _, dir := range overrideDirs {
		extra := readFileString(filepath.Join(dir, "_extra.md"))
		if extra != "" {
			result += "\n\n" + extra
		}
	}

	return result
}

// loadPromptSection loads a single prompt section by name.
// Checks override directories in order (last wins), falls back to embedded default.
func loadPromptSection(name string, overrideDirs []string) string {
	filename := name + ".md"

	// Check override dirs (later entries have higher priority).
	for i := len(overrideDirs) - 1; i >= 0; i-- {
		content := readFileString(filepath.Join(overrideDirs[i], filename))
		if content != "" {
			return content
		}
	}

	// Fall back to embedded default.
	data, err := defaultPromptFS.ReadFile("prompts/" + filename)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// promptOverrideDirs returns the directories to check for prompt overrides,
// in priority order (lowest first).
func promptOverrideDirs(cwd, gitRoot string) []string {
	seen := make(map[string]bool)
	var dirs []string

	add := func(dir string) {
		abs, err := filepath.Abs(dir)
		if err != nil || seen[abs] {
			return
		}
		// Only add if directory actually exists.
		if info, err := os.Stat(abs); err != nil || !info.IsDir() {
			return
		}
		seen[abs] = true
		dirs = append(dirs, abs)
	}

	// 1. Global user overrides (lowest priority)
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, ".config", "aictl", "prompts"))
	}

	// 2. Git root project overrides
	if gitRoot != "" && gitRoot != cwd {
		add(filepath.Join(gitRoot, ".aictl", "prompts"))
	}

	// 3. Current working directory overrides (highest priority)
	add(filepath.Join(cwd, ".aictl", "prompts"))

	return dirs
}

// readFileString reads a file and returns its trimmed content.
// Returns empty string if the file doesn't exist or is empty.
func readFileString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
