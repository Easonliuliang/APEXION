package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// SkillInfo describes a skill file that can be loaded by the LLM.
type SkillInfo struct {
	Name string // filename without extension
	Path string // full path for read_file
	Desc string // first line or frontmatter description
}

// loadSkills scans for .apexion/skills/*.md in:
//  1. ~/.config/apexion/skills/    (global)
//  2. {gitRoot}/.apexion/skills/   (project)
//  3. {cwd}/.apexion/skills/       (local)
func loadSkills(cwd string) []SkillInfo {
	gitRoot := findGitRoot(cwd)
	dirs := skillDirs(cwd, gitRoot)

	seen := make(map[string]bool)
	var skills []SkillInfo

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			name := strings.TrimSuffix(e.Name(), ".md")
			if seen[name] {
				continue
			}

			path := filepath.Join(dir, e.Name())
			desc := extractSkillDesc(path)
			seen[name] = true
			skills = append(skills, SkillInfo{
				Name: name,
				Path: path,
				Desc: desc,
			})
		}
	}

	return skills
}

// skillDirs returns directories to scan for skills.
func skillDirs(cwd, gitRoot string) []string {
	seen := make(map[string]bool)
	var dirs []string

	add := func(dir string) {
		abs, err := filepath.Abs(dir)
		if err != nil || seen[abs] {
			return
		}
		if info, err := os.Stat(abs); err != nil || !info.IsDir() {
			return
		}
		seen[abs] = true
		dirs = append(dirs, abs)
	}

	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, ".config", "apexion", "skills"))
		add(filepath.Join(home, ".claude", "skills"))
	}

	if gitRoot != "" && gitRoot != cwd {
		add(filepath.Join(gitRoot, ".apexion", "skills"))
	}

	add(filepath.Join(cwd, ".apexion", "skills"))

	return dirs
}

// extractSkillDesc reads the first non-empty line of a skill file as its description.
// If the file has YAML frontmatter with a "description:" field, that is used instead.
func extractSkillDesc(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return ""
	}

	// Check for frontmatter description
	if strings.HasPrefix(content, "---") {
		end := strings.Index(content[3:], "---")
		if end >= 0 {
			fm := content[3 : 3+end]
			for _, line := range strings.Split(fm, "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "description:") {
					desc := strings.TrimSpace(strings.TrimPrefix(trimmed, "description:"))
					return strings.Trim(desc, "\"'")
				}
			}
		}
	}

	// Fall back to first non-empty line (strip markdown heading prefix)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && line != "---" {
			line = strings.TrimLeft(line, "# ")
			if len(line) > 60 {
				line = line[:57] + "..."
			}
			return line
		}
	}
	return ""
}
