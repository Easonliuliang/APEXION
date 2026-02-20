package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	maxFileBytes  = 8 * 1024  // 8 KB per file
	maxTotalBytes = 16 * 1024 // 16 KB total
)

// loadProjectContext scans for AICTL.md / .aictl/context.md files and returns
// a formatted string to append to the system prompt.
// Returns empty string if no context files are found.
func loadProjectContext(cwd string) string {
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return ""
		}
	}

	gitRoot := findGitRoot(cwd)
	paths := candidatePaths(cwd, gitRoot)

	var sections []string
	totalBytes := 0

	for _, p := range paths {
		if totalBytes >= maxTotalBytes {
			break
		}

		content := readContextFile(p)
		if content == "" {
			continue
		}

		remaining := maxTotalBytes - totalBytes
		if len(content) > remaining {
			content = content[:remaining] + "\n[Truncated: context file too large]"
		}

		totalBytes += len(content)
		sections = append(sections, fmt.Sprintf("<!-- Source: %s -->\n%s", p, content))
	}

	if len(sections) == 0 {
		return ""
	}

	return "\n\n<project_context>\n" +
		strings.Join(sections, "\n\n") +
		"\n</project_context>"
}

// findGitRoot runs `git rev-parse --show-toplevel` to find the repository root.
// Returns empty string if not inside a git repository.
func findGitRoot(cwd string) string {
	cmd := exec.Command(gitExecutable(), "rev-parse", "--show-toplevel")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitExecutable returns the git binary path, checking common locations.
func gitExecutable() string {
	if p, err := exec.LookPath("git"); err == nil {
		return p
	}
	for _, candidate := range []string{"/usr/bin/git", "/usr/local/bin/git", "/opt/homebrew/bin/git"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "git"
}

// candidatePaths returns the ordered list of context file paths to try,
// from lowest to highest priority (later entries override earlier ones conceptually,
// but here we load all and concatenate, with global context first).
// Duplicate paths (when cwd == gitRoot) are automatically removed.
func candidatePaths(cwd, gitRoot string) []string {
	seen := make(map[string]bool)
	var paths []string

	add := func(p string) {
		abs, err := filepath.Abs(p)
		if err != nil || seen[abs] {
			return
		}
		seen[abs] = true
		paths = append(paths, abs)
	}

	// 1. User global context (lowest priority, always loaded first)
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, ".config", "aictl", "context.md"))
		add(filepath.Join(home, ".config", "aictl", "AICTL.md"))
	}

	// 2. Git root project context
	if gitRoot != "" && gitRoot != cwd {
		add(filepath.Join(gitRoot, ".aictl", "context.md"))
		add(filepath.Join(gitRoot, "AICTL.md"))
	}

	// 3. Current working directory context (highest priority)
	add(filepath.Join(cwd, ".aictl", "context.md"))
	add(filepath.Join(cwd, "AICTL.md"))

	return paths
}

// readContextFile reads a context file and returns its content.
// Returns empty string if the file doesn't exist or is empty.
// Truncates at maxFileBytes with a notice.
func readContextFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "" // file doesn't exist — expected, not an error
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		return ""
	}

	if len(content) > maxFileBytes {
		content = content[:maxFileBytes] + "\n[Truncated: file exceeds 8KB limit]"
	}

	return content
}
