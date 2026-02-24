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

// modelPromptVariant returns the prompt variant for a given provider.
// "full" for anthropic, "lite" for all other providers (simplified prompt for weaker models).
func modelPromptVariant(providerName string) string {
	switch providerName {
	case "anthropic":
		return "full"
	default:
		return "lite"
	}
}

// loadSystemPrompt assembles the system prompt from embedded defaults and user overrides.
// Override paths (in priority order, higher wins):
//
//	~/.config/apexion/prompts/{section}.md   — global user override
//	{gitRoot}/.apexion/prompts/{section}.md  — project-level override
//
// If a user file exists for a section, it replaces the embedded default for that section.
// A special "_extra.md" file in any override directory is appended after all sections.
//
// The variant parameter controls prompt complexity:
//   - "full": load all sections as-is (for Claude/Anthropic)
//   - "lite": use communication_lite.md and strip todo_write/todo_read from tools.md
func loadSystemPrompt(cwd, variant string) string {
	gitRoot := findGitRoot(cwd)
	overrideDirs := promptOverrideDirs(cwd, gitRoot)

	var sections []string
	for _, name := range promptSections {
		actualName := name
		if variant == "lite" && name == "communication" {
			actualName = "communication_lite"
		}
		content := loadPromptSection(actualName, overrideDirs)
		if variant == "lite" && name == "tools" {
			content = stripTodoSection(content)
		}
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

// stripTodoSection removes the todo_write / todo_read guideline block from tools.md content.
func stripTodoSection(content string) string {
	const startMarker = "todo_write / todo_read"
	idx := strings.Index(content, startMarker)
	if idx == -1 {
		return content
	}
	// Find the start of the line containing the marker.
	lineStart := strings.LastIndex(content[:idx], "\n")
	if lineStart == -1 {
		lineStart = 0
	}
	// Find the end of the section: next tool section header or end of parent block.
	rest := content[idx:]
	endOffset := -1
	for _, marker := range []string{"\ntask ", "\n</tool_guidelines>"} {
		if pos := strings.Index(rest, marker); pos != -1 {
			if endOffset == -1 || pos < endOffset {
				endOffset = pos
			}
		}
	}
	if endOffset == -1 {
		// No next section found; strip to end.
		return strings.TrimRight(content[:lineStart], "\n")
	}
	return content[:lineStart] + content[idx+endOffset:]
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
		add(filepath.Join(home, ".config", "apexion", "prompts"))
	}

	// 2. Git root project overrides
	if gitRoot != "" && gitRoot != cwd {
		add(filepath.Join(gitRoot, ".apexion", "prompts"))
	}

	// 3. Current working directory overrides (highest priority)
	add(filepath.Join(cwd, ".apexion", "prompts"))

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
