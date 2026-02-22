package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// SlashMenuItem is a single entry in the slash command autocomplete menu.
type SlashMenuItem struct {
	Name string // e.g. "/provider"
	Desc string // e.g. "Switch provider"
}

// BuiltinSlashCommands returns the hardcoded list of built-in slash commands.
func BuiltinSlashCommands() []SlashMenuItem {
	return []SlashMenuItem{
		{Name: "/help", Desc: "Show help message"},
		{Name: "/model", Desc: "Switch model"},
		{Name: "/provider", Desc: "Switch provider"},
		{Name: "/config", Desc: "Show configuration"},
		{Name: "/plan", Desc: "Toggle plan mode"},
		{Name: "/compact", Desc: "Compact context"},
		{Name: "/changes", Desc: "Show file changes"},
		{Name: "/trust", Desc: "Show/reset approvals"},
		{Name: "/rules", Desc: "List loaded rules"},
		{Name: "/skills", Desc: "List available skills"},
		{Name: "/commands", Desc: "List custom commands"},
		{Name: "/memory", Desc: "Manage memories"},
		{Name: "/mcp", Desc: "MCP server status"},
		{Name: "/test", Desc: "Run test for a file"},
		{Name: "/map", Desc: "Show repository map"},
		{Name: "/architect", Desc: "Architect mode (dual-model)"},
		{Name: "/bg", Desc: "Background agents status"},
		{Name: "/audit", Desc: "Show command audit log"},
		{Name: "/save", Desc: "Save session"},
		{Name: "/sessions", Desc: "List saved sessions"},
		{Name: "/resume", Desc: "Resume a session"},
		{Name: "/history", Desc: "Show message history"},
		{Name: "/cost", Desc: "Show cost & token usage"},
		{Name: "/clear", Desc: "Clear history"},
		{Name: "/quit", Desc: "Save and exit"},
	}
}

// filterSlashItems returns items whose Name starts with the given prefix (case-insensitive).
func filterSlashItems(items []SlashMenuItem, prefix string) []SlashMenuItem {
	if prefix == "" || prefix == "/" {
		return items
	}
	lower := strings.ToLower(prefix)
	var out []SlashMenuItem
	for _, it := range items {
		if strings.HasPrefix(strings.ToLower(it.Name), lower) {
			out = append(out, it)
		}
	}
	return out
}

// ── styles for the slash menu ────────────────────────────────────────────────

var (
	slashMenuBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("63")).
			Padding(0, 1)

	slashMenuItemNormal = lipgloss.NewStyle().
				Foreground(lipgloss.Color("252"))

	slashMenuItemSelected = lipgloss.NewStyle().
				Foreground(lipgloss.Color("220")).
				Bold(true)

	slashMenuDesc = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	slashMenuDescSelected = lipgloss.NewStyle().
				Foreground(lipgloss.Color("178"))
)

// renderSlashMenu renders the slash command dropdown menu.
// sel is the currently highlighted index. width is the available terminal width.
func renderSlashMenu(items []SlashMenuItem, sel int, width int) string {
	if len(items) == 0 {
		return ""
	}

	// Compute the max command name width for alignment.
	maxName := 0
	for _, it := range items {
		if len(it.Name) > maxName {
			maxName = len(it.Name)
		}
	}

	var lines []string
	for i, it := range items {
		name := it.Name
		// Pad name for alignment.
		padded := name + strings.Repeat(" ", maxName-len(name))

		var line string
		if i == sel {
			line = slashMenuItemSelected.Render(padded) + "   " + slashMenuDescSelected.Render(it.Desc)
		} else {
			line = slashMenuItemNormal.Render(padded) + "   " + slashMenuDesc.Render(it.Desc)
		}
		lines = append(lines, line)
	}

	inner := strings.Join(lines, "\n")

	// Constrain the box width.
	maxWidth := width - 6
	if maxWidth < 30 {
		maxWidth = 30
	}
	return slashMenuBorder.MaxWidth(maxWidth).Render(inner)
}
