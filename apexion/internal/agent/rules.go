package agent

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Rule represents a single rule loaded from .apexion/rules/*.md.
type Rule struct {
	Name         string   // filename without extension
	Description  string   // from frontmatter
	PathPatterns []string // glob patterns for scoped activation
	Content      string   // rule text body
	Source       string   // file path (for display)
}

// loadRules scans for .apexion/rules/*.md in:
//  1. ~/.config/apexion/rules/    (global)
//  2. {gitRoot}/.apexion/rules/   (project)
//  3. {cwd}/.apexion/rules/       (local)
//
// Each .md file can have optional YAML frontmatter:
//
//	---
//	path_patterns:
//	  - "*.go"
//	  - "internal/**"
//	description: "Go coding style rules"
//	---
//	Rule content here...
//
// Rules without path_patterns are always active.
func loadRules(cwd string) []Rule {
	gitRoot := findGitRoot(cwd)
	dirs := ruleDirs(cwd, gitRoot)

	seen := make(map[string]bool) // dedup by rule name
	var rules []Rule

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
				continue // later dirs override earlier ones
			}

			path := filepath.Join(dir, e.Name())
			r, ok := parseRuleFile(path, name)
			if !ok {
				continue
			}
			seen[name] = true
			rules = append(rules, r)
		}
	}

	return rules
}

// ruleDirs returns directories to scan for rules, in priority order (lowest first).
// Later directories override earlier ones for rules with the same filename.
func ruleDirs(cwd, gitRoot string) []string {
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

	// 1. Global (lowest priority)
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, ".config", "apexion", "rules"))
	}

	// 2. Git root project
	if gitRoot != "" && gitRoot != cwd {
		add(filepath.Join(gitRoot, ".apexion", "rules"))
	}

	// 3. Current working directory (highest priority)
	add(filepath.Join(cwd, ".apexion", "rules"))

	return dirs
}

// parseRuleFile reads a rule file and parses optional YAML frontmatter.
func parseRuleFile(path, name string) (Rule, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Rule{}, false
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		return Rule{}, false
	}

	r := Rule{
		Name:   name,
		Source: path,
	}

	// Parse optional YAML frontmatter (between --- delimiters).
	if strings.HasPrefix(content, "---") {
		end := strings.Index(content[3:], "---")
		if end >= 0 {
			frontmatter := content[3 : 3+end]
			content = strings.TrimSpace(content[3+end+3:])
			parseFrontmatter(&r, frontmatter)
		}
	}

	r.Content = content
	return r, true
}

// parseFrontmatter extracts description and path_patterns from simple YAML.
// Uses line-by-line parsing to avoid a YAML dependency.
func parseFrontmatter(r *Rule, fm string) {
	scanner := bufio.NewScanner(strings.NewReader(fm))
	var inPathPatterns bool

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "description:") {
			r.Description = strings.TrimSpace(strings.TrimPrefix(trimmed, "description:"))
			r.Description = strings.Trim(r.Description, "\"'")
			inPathPatterns = false
			continue
		}

		if strings.HasPrefix(trimmed, "path_patterns:") {
			inPathPatterns = true
			continue
		}

		if inPathPatterns && strings.HasPrefix(trimmed, "- ") {
			pattern := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			pattern = strings.Trim(pattern, "\"'")
			if pattern != "" {
				r.PathPatterns = append(r.PathPatterns, pattern)
			}
			continue
		}

		if inPathPatterns && !strings.HasPrefix(trimmed, "-") {
			inPathPatterns = false
		}
	}
}
