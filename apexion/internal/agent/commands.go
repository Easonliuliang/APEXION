package agent

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

// CustomCommand is a user-defined slash command loaded from a .md file.
type CustomCommand struct {
	Name        string       `yaml:"name"`
	Description string       `yaml:"description"`
	Args        []CommandArg `yaml:"args"`
	body        string       // Markdown/template body after frontmatter
	source      string       // file path this command was loaded from
}

// CommandArg defines a positional argument for a custom command.
type CommandArg struct {
	Name     string `yaml:"name"`
	Required bool   `yaml:"required"`
	Default  string `yaml:"default"`
}

// loadCustomCommands scans command directories and returns a map of name → CustomCommand.
// Later directories override earlier ones (project > global).
func loadCustomCommands(cwd string) map[string]*CustomCommand {
	commands := make(map[string]*CustomCommand)

	dirs := commandDirs(cwd, findGitRoot(cwd))
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			cmd, err := parseCommandFile(path)
			if err != nil {
				continue
			}
			commands[cmd.Name] = cmd
		}
	}
	return commands
}

// commandDirs returns directories to scan for custom commands, lowest to highest priority.
func commandDirs(cwd, gitRoot string) []string {
	var dirs []string

	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".config", "apexion", "commands"))
	}

	if gitRoot != "" && gitRoot != cwd {
		dirs = append(dirs, filepath.Join(gitRoot, ".apexion", "commands"))
	}

	dirs = append(dirs, filepath.Join(cwd, ".apexion", "commands"))

	return dirs
}

// parseCommandFile reads a .md file with YAML frontmatter and returns a CustomCommand.
func parseCommandFile(path string) (*CustomCommand, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	content := string(data)
	front, body, err := splitFrontmatter(content)
	if err != nil {
		return nil, err
	}

	var cmd CustomCommand
	if err := yaml.Unmarshal([]byte(front), &cmd); err != nil {
		return nil, fmt.Errorf("invalid frontmatter in %s: %w", path, err)
	}

	if cmd.Name == "" {
		// Derive name from filename.
		cmd.Name = strings.TrimSuffix(filepath.Base(path), ".md")
	}

	cmd.body = strings.TrimSpace(body)
	cmd.source = path
	return &cmd, nil
}

// splitFrontmatter splits "---\nyaml\n---\nbody" into (yaml, body, err).
func splitFrontmatter(content string) (string, string, error) {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "---") {
		// No frontmatter — treat entire content as body.
		return "", content, nil
	}

	// Find closing "---".
	rest := content[3:]
	if idx := strings.Index(rest, "\n---"); idx >= 0 {
		front := rest[:idx]
		body := rest[idx+4:] // skip "\n---"
		return strings.TrimSpace(front), strings.TrimSpace(body), nil
	}

	return "", "", fmt.Errorf("unclosed frontmatter (missing closing ---)")
}

// renderCommand expands the command template with the given arguments.
func renderCommand(cmd *CustomCommand, rawArgs string) (string, error) {
	// Parse positional args from the raw arg string.
	data := buildArgMap(cmd, rawArgs)

	tmpl, err := template.New(cmd.Name).Parse(cmd.body)
	if err != nil {
		return "", fmt.Errorf("template error in /%s: %w", cmd.Name, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("template execution error in /%s: %w", cmd.Name, err)
	}

	return buf.String(), nil
}

// buildArgMap maps command arg definitions to actual values from the raw input.
func buildArgMap(cmd *CustomCommand, rawArgs string) map[string]string {
	data := make(map[string]string)

	// Split rawArgs by spaces, respecting the last arg getting the rest.
	var parts []string
	if rawArgs != "" {
		parts = strings.Fields(rawArgs)
	}

	for i, arg := range cmd.Args {
		if i < len(parts) {
			if i == len(cmd.Args)-1 && len(parts) > len(cmd.Args) {
				// Last arg gets the remainder.
				data[arg.Name] = strings.Join(parts[i:], " ")
			} else {
				data[arg.Name] = parts[i]
			}
		} else if arg.Default != "" {
			data[arg.Name] = arg.Default
		} else {
			data[arg.Name] = ""
		}
	}

	// Also expose the raw args string.
	data["_args"] = rawArgs

	return data
}

// formatCommandList returns a formatted list of custom commands for display.
func formatCommandList(commands map[string]*CustomCommand) string {
	if len(commands) == 0 {
		return "No custom commands found.\nPlace .md files in ~/.config/apexion/commands/ or .apexion/commands/"
	}

	// Sort by name.
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	sort.Strings(names)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Custom commands (%d):\n", len(commands)))
	for _, name := range names {
		cmd := commands[name]
		desc := cmd.Description
		if desc == "" {
			desc = "(no description)"
		}

		// Build args hint.
		var argHints []string
		for _, arg := range cmd.Args {
			if arg.Required {
				argHints = append(argHints, "<"+arg.Name+">")
			} else {
				argHints = append(argHints, "["+arg.Name+"]")
			}
		}

		usage := "/" + name
		if len(argHints) > 0 {
			usage += " " + strings.Join(argHints, " ")
		}

		sb.WriteString(fmt.Sprintf("  %-28s %s\n", usage, desc))
	}
	sb.WriteString("\nSource directories: ~/.config/apexion/commands/, .apexion/commands/")
	return sb.String()
}
