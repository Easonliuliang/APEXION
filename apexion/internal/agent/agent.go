package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/apexion-ai/apexion/internal/config"
	"github.com/apexion-ai/apexion/internal/mcp"
	"github.com/apexion-ai/apexion/internal/permission"
	"github.com/apexion-ai/apexion/internal/provider"
	"github.com/apexion-ai/apexion/internal/session"
	"github.com/apexion-ai/apexion/internal/tools"
	"github.com/apexion-ai/apexion/internal/tui"
)

// defaultSystemPrompt is loaded from embedded prompts/*.md files at runtime
// via loadSystemPrompt(). Users can override individual sections by placing
// files in ~/.config/apexion/prompts/ or {project}/.apexion/prompts/.

const subAgentSystemPrompt = `You are a research sub-agent. Your job is to explore and gather information, then return a clear summary.

You have read-only tools: read_file, glob, grep, list_dir, web_fetch, todo_read.
You CANNOT modify files, run commands, or make git changes.

Rules:
- Focus on the specific task given to you.
- Use tools to gather evidence. Do not guess.
- Return a concise, well-organized summary of your findings.
- If you cannot find what was asked, say so clearly.`

// ProviderFactory creates a Provider from a config. Used for /provider hot-swap.
type ProviderFactory func(cfg *config.Config) (provider.Provider, error)

// Agent orchestrates the interactive loop between user, LLM, and tools.
type Agent struct {
	provider        provider.Provider
	executor        *tools.Executor
	config          *config.Config
	session         *session.Session
	store           session.Store
	memoryStore     session.MemoryStore
	mcpManager      *mcp.Manager
	basePrompt      string // system prompt without identity suffix
	systemPrompt    string
	io              tui.IO
	summarizer      session.Summarizer
	providerFactory ProviderFactory
	customCommands  map[string]*CustomCommand
}

// New creates a new Agent with the given IO implementation.
// Pass tui.NewPlainIO() for plain terminal mode.
func New(p provider.Provider, exec *tools.Executor, cfg *config.Config, ui tui.IO, store session.Store) *Agent {
	return NewWithSession(p, exec, cfg, ui, store, session.New())
}

// NewWithSession creates a new Agent with an existing session.
func NewWithSession(p provider.Provider, exec *tools.Executor, cfg *config.Config, ui tui.IO, store session.Store, sess *session.Session) *Agent {
	cwd, _ := os.Getwd()

	// Load modular system prompt from embedded defaults + user overrides.
	base := loadSystemPrompt(cwd)
	if cfg.SystemPrompt != "" {
		base = cfg.SystemPrompt // full override from config
	}

	// Append project context from APEXION.md / .apexion/context.md
	if ctx := loadProjectContext(cwd); ctx != "" {
		base += ctx
	}

	a := &Agent{
		provider:       p,
		executor:       exec,
		config:         cfg,
		session:        sess,
		store:          store,
		basePrompt:     base,
		io:             ui,
		summarizer:     &session.LLMSummarizer{Provider: p},
		customCommands: loadCustomCommands(cwd),
	}
	a.rebuildSystemPrompt()
	a.wireTaskTool()
	return a
}

// SetProviderFactory sets the factory function for /provider hot-swap.
func (a *Agent) SetProviderFactory(f ProviderFactory) {
	a.providerFactory = f
}

// SetMemoryStore injects the cross-session memory store and rebuilds the system prompt
// to include relevant memories.
func (a *Agent) SetMemoryStore(ms session.MemoryStore) {
	a.memoryStore = ms
	a.rebuildSystemPrompt()
}

// SetMCPManager injects the MCP manager for /mcp command and status display.
func (a *Agent) SetMCPManager(m *mcp.Manager) {
	a.mcpManager = m
}

// rebuildSystemPrompt appends a dynamic identity suffix and persistent memories to basePrompt.
// Call after changing provider, model, or memory store.
func (a *Agent) rebuildSystemPrompt() {
	model := a.config.Model
	if model == "" {
		model = a.provider.DefaultModel()
	}
	a.systemPrompt = a.basePrompt + fmt.Sprintf(
		"\n\nYou are powered by %s (provider: %s, model: %s). "+
			"When asked about your identity, state these facts. Never claim to be a different model.",
		a.config.Provider, a.config.Provider, model)

	// Inject persistent memories if available.
	if a.memoryStore != nil {
		cwd, _ := os.Getwd()
		projectTag := "project:" + filepath.Base(cwd)
		if mem := a.memoryStore.LoadForPrompt(projectTag, 2048); mem != "" {
			a.systemPrompt += "\n\n" + mem
		}
	}
}

// Run starts the interactive REPL loop.
func (a *Agent) Run(ctx context.Context) error {
	for {
		input, err := a.io.ReadInput()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		if input == "" {
			continue
		}

		// Slash commands are intercepted before sending to LLM.
		if strings.HasPrefix(input, "/") {
			handled, shouldQuit := a.handleSlashCommand(ctx, input)
			if shouldQuit {
				return nil
			}
			if handled {
				continue
			}
		}

		a.io.UserMessage(input)
		a.session.AddMessage(provider.Message{
			Role: provider.RoleUser,
			Content: []provider.Content{{
				Type: provider.ContentTypeText,
				Text: input,
			}},
		})

		if err := a.runAgentLoop(ctx); err != nil {
			if ctx.Err() != nil {
				a.io.SystemMessage("\nInterrupted.")
				_ = a.store.Save(a.session)
				return ctx.Err()
			}
			a.io.Error(err.Error())
		}
	}

	// Show file change summary on exit if any files were modified.
	if changes := a.executor.FileTracker().Summary(); changes != "" {
		a.io.SystemMessage("\n--- Session file changes ---\n" + changes)
	}

	_ = a.store.Save(a.session)
	return nil
}

// RunOnce executes a single prompt and exits (non-interactive mode).
func (a *Agent) RunOnce(ctx context.Context, prompt string) error {
	a.io.UserMessage(prompt)
	a.session.AddMessage(provider.Message{
		Role: provider.RoleUser,
		Content: []provider.Content{{
			Type: provider.ContentTypeText,
			Text: prompt,
		}},
	})
	return a.runAgentLoop(ctx)
}

// handleSlashCommand processes built-in commands.
// Returns (handled, shouldQuit).
func (a *Agent) handleSlashCommand(ctx context.Context, input string) (bool, bool) {
	// Parse command and arguments.
	parts := strings.SplitN(strings.TrimSpace(input), " ", 2)
	cmd := parts[0]
	arg := ""
	if len(parts) > 1 {
		arg = strings.TrimSpace(parts[1])
	}

	switch cmd {
	case "/quit", "/exit", "/q":
		a.io.SystemMessage("Bye.")
		_ = a.store.Save(a.session)
		return true, true
	case "/clear":
		a.session.Clear()
		a.io.SystemMessage("Session cleared.")
		return true, false
	case "/history":
		a.io.SystemMessage(formatHistory(a.session.Messages))
		return true, false
	case "/cost":
		a.io.SystemMessage(fmt.Sprintf("Tokens used: %d", a.session.TokensUsed))
		return true, false
	case "/compact":
		return a.handleCompact(ctx), false
	case "/help":
		return a.handleHelp(), false
	case "/model":
		return a.handleModel(arg), false
	case "/provider":
		return a.handleProvider(arg), false
	case "/config":
		return a.handleConfig(), false
	case "/save":
		return a.handleSave(), false
	case "/sessions":
		return a.handleSessions(), false
	case "/resume":
		return a.handleResume(arg), false
	case "/changes":
		return a.handleChanges(), false
	case "/trust":
		return a.handleTrust(arg), false
	case "/commands":
		a.io.SystemMessage(formatCommandList(a.customCommands))
		return true, false
	case "/memory":
		return a.handleMemory(arg), false
	case "/mcp":
		return a.handleMCP(ctx, arg), false
	default:
		// Check custom commands.
		name := strings.TrimPrefix(cmd, "/")
		if cc, ok := a.customCommands[name]; ok {
			return a.handleCustomCommand(ctx, cc, arg), false
		}
		return false, false
	}
}

func (a *Agent) handleCompact(ctx context.Context) bool {
	if a.summarizer == nil {
		a.io.SystemMessage("Summarizer not configured.")
		return true
	}
	before := a.session.EstimateTokens()
	summary, err := a.summarizer.Summarize(ctx, a.session.Summary, a.session.Messages)
	if err != nil {
		a.io.Error("Compact failed: " + err.Error())
		return true
	}
	a.session.Summary = summary
	a.session.Messages = session.TruncateSession(a.session.Messages, 10)
	a.session.GentleCompactDone = false
	after := a.session.EstimateTokens()
	a.io.SystemMessage(fmt.Sprintf("Compacted: %dk → %dk tokens. %d messages retained.\nSummary:\n%s",
		before/1000, after/1000, len(a.session.Messages), truncate(summary, 300)))
	return true
}

func (a *Agent) handleChanges() bool {
	summary := a.executor.FileTracker().Summary()
	if summary == "" {
		a.io.SystemMessage("No file changes recorded in this session.")
	} else {
		a.io.SystemMessage(summary)
	}
	return true
}

func (a *Agent) handleTrust(arg string) bool {
	dp, ok := a.executor.Policy().(*permission.DefaultPolicy)
	if !ok {
		a.io.SystemMessage("Approval memory not available (policy is not DefaultPolicy).")
		return true
	}

	if arg == "reset" {
		dp.ResetApprovals()
		a.io.SystemMessage("Session approvals cleared.")
		return true
	}

	summary := dp.Approvals()
	if summary == "" {
		a.io.SystemMessage("No session approvals recorded.\nApprovals are added when you confirm a tool call.")
	} else {
		a.io.SystemMessage(summary)
	}
	return true
}

func (a *Agent) handleHelp() bool {
	help := `Available commands:
  /help              Show this help message
  /model <name>      Switch model (e.g. /model claude-haiku-4-5-20251001)
  /provider <name>   Switch provider (e.g. /provider deepseek)
  /config            Show current configuration
  /compact           Manually trigger context compaction
  /changes           Show files modified in this session
  /trust             Show session-level tool approvals
  /trust reset       Clear all session approvals
  /commands           List custom commands
  /memory             List saved memories
  /memory add <text>  Save a memory (add tags with #tag)
  /memory search <q>  Search memories
  /memory delete <id> Delete a memory
  /mcp               Show MCP server connection status
  /mcp reset         Reconnect all MCP servers
  /save              Save current session to disk
  /sessions          List saved sessions
  /resume <id>       Resume a saved session (use short ID prefix)
  /history           Show message history
  /cost              Show token usage
  /clear             Clear message history
  /quit              Save and exit`

	// Append custom commands if any.
	if len(a.customCommands) > 0 {
		help += "\n\nCustom commands:"
		names := make([]string, 0, len(a.customCommands))
		for name := range a.customCommands {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			desc := a.customCommands[name].Description
			if desc == "" {
				desc = "(no description)"
			}
			help += fmt.Sprintf("\n  /%-18s %s", name, desc)
		}
	}

	a.io.SystemMessage(help)
	return true
}

func (a *Agent) handleMemory(arg string) bool {
	if a.memoryStore == nil {
		a.io.SystemMessage("Memory store not configured.")
		return true
	}

	// Parse subcommand.
	parts := strings.SplitN(arg, " ", 2)
	subcmd := ""
	subarg := ""
	if len(parts) > 0 {
		subcmd = parts[0]
	}
	if len(parts) > 1 {
		subarg = strings.TrimSpace(parts[1])
	}

	switch subcmd {
	case "add":
		if subarg == "" {
			a.io.Error("Usage: /memory add <text> (use #tag to add tags)")
			return true
		}
		content, tags := parseMemoryInput(subarg)
		m, err := a.memoryStore.Add(content, tags, "manual", a.session.ID)
		if err != nil {
			a.io.Error("Failed to save memory: " + err.Error())
			return true
		}
		a.io.SystemMessage(fmt.Sprintf("Memory saved [%s]: %s", m.ID, truncate(content, 100)))
		// Rebuild prompt to include new memory.
		a.rebuildSystemPrompt()

	case "search":
		if subarg == "" {
			a.io.Error("Usage: /memory search <query>")
			return true
		}
		memories, err := a.memoryStore.Search(subarg, 10)
		if err != nil {
			a.io.Error("Search failed: " + err.Error())
			return true
		}
		a.io.SystemMessage(formatMemories(memories, "Search results"))

	case "delete", "rm":
		if subarg == "" {
			a.io.Error("Usage: /memory delete <id>")
			return true
		}
		if err := a.memoryStore.Delete(subarg); err != nil {
			a.io.Error("Delete failed: " + err.Error())
			return true
		}
		a.io.SystemMessage(fmt.Sprintf("Memory %s deleted.", subarg))
		a.rebuildSystemPrompt()

	default:
		// List all memories.
		memories, err := a.memoryStore.List(20)
		if err != nil {
			a.io.Error("Failed to list memories: " + err.Error())
			return true
		}
		a.io.SystemMessage(formatMemories(memories, "Memories"))
	}

	return true
}

// parseMemoryInput extracts content and #tags from user input.
// e.g. "prefer snake_case #preference #style" → ("prefer snake_case", ["preference", "style"])
func parseMemoryInput(input string) (string, []string) {
	words := strings.Fields(input)
	var content []string
	var tags []string

	for _, w := range words {
		if strings.HasPrefix(w, "#") && len(w) > 1 {
			tags = append(tags, w[1:])
		} else {
			content = append(content, w)
		}
	}

	return strings.Join(content, " "), tags
}

// formatMemories formats a list of memories for display.
func formatMemories(memories []session.Memory, title string) string {
	if len(memories) == 0 {
		return "No memories found.\nUse /memory add <text> #tag to save one."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s (%d):\n", title, len(memories)))
	for _, m := range memories {
		tags := ""
		if len(m.Tags) > 0 {
			tags = " [" + strings.Join(m.Tags, ", ") + "]"
		}
		sb.WriteString(fmt.Sprintf("  %s  %s  %s%s\n",
			m.ID,
			m.CreatedAt.Format("2006-01-02"),
			truncate(m.Content, 60),
			tags,
		))
	}
	return strings.TrimRight(sb.String(), "\n")
}

// handleCustomCommand renders a custom command template and sends it as user input to the LLM.
func (a *Agent) handleCustomCommand(ctx context.Context, cmd *CustomCommand, rawArgs string) bool {
	// Check required args.
	for _, arg := range cmd.Args {
		if arg.Required && rawArgs == "" {
			a.io.Error(fmt.Sprintf("Usage: /%s <%s>\n%s", cmd.Name, arg.Name, cmd.Description))
			return true
		}
	}

	prompt, err := renderCommand(cmd, rawArgs)
	if err != nil {
		a.io.Error(err.Error())
		return true
	}

	// Show the rendered prompt as a user message and inject into the conversation.
	a.io.SystemMessage(fmt.Sprintf("[/%s] %s", cmd.Name, truncate(prompt, 200)))
	a.session.AddMessage(provider.Message{
		Role: provider.RoleUser,
		Content: []provider.Content{{
			Type: provider.ContentTypeText,
			Text: prompt,
		}},
	})

	// Run the agent loop to process the command.
	if err := a.runAgentLoop(ctx); err != nil {
		a.io.Error(err.Error())
	}
	return true
}

func (a *Agent) handleModel(name string) bool {
	if name == "" {
		a.io.SystemMessage(fmt.Sprintf("Current model: %s\nUsage: /model <name>", a.config.Model))
		return true
	}
	old := a.config.Model
	a.config.Model = name
	if old == "" {
		old = a.provider.DefaultModel()
	}
	a.rebuildSystemPrompt()
	a.io.SystemMessage(fmt.Sprintf("Model switched: %s → %s", old, name))
	return true
}

func (a *Agent) handleProvider(name string) bool {
	if name == "" {
		a.io.SystemMessage(fmt.Sprintf("Current provider: %s\nUsage: /provider <name>", a.config.Provider))
		return true
	}
	if a.providerFactory == nil {
		a.io.Error("Provider hot-swap not available.")
		return true
	}

	// Ensure the provider has an API key; prompt interactively if missing.
	pc := a.config.GetProviderConfig(name)
	needSave := false

	if pc.APIKey == "" {
		a.io.SystemMessage(fmt.Sprintf("No API key configured for %q.", name))
		a.io.SystemMessage("Enter API key:")
		key, err := a.io.ReadInput()
		if err != nil || strings.TrimSpace(key) == "" {
			a.io.Error("Cancelled — no API key provided.")
			return true
		}
		key = strings.TrimSpace(key)

		// Determine base URL: known providers get auto-filled, unknown ones need input.
		baseURL := ""
		if _, known := config.KnownProviderBaseURLs[name]; !known {
			// Also not known if user already had a base_url in config.
			if pc.BaseURL == "" {
				a.io.SystemMessage(fmt.Sprintf("No known base URL for %q.", name))
				a.io.SystemMessage("Enter base URL:")
				url, err := a.io.ReadInput()
				if err != nil || strings.TrimSpace(url) == "" {
					a.io.Error("Cancelled — no base URL provided.")
					return true
				}
				baseURL = strings.TrimSpace(url)
			}
		}

		// Write into in-memory config.
		if a.config.Providers == nil {
			a.config.Providers = make(map[string]*config.ProviderConfig)
		}
		if a.config.Providers[name] == nil {
			a.config.Providers[name] = &config.ProviderConfig{}
		}
		a.config.Providers[name].APIKey = key
		if baseURL != "" {
			a.config.Providers[name].BaseURL = baseURL
		}
		needSave = true
	}

	oldName := a.config.Provider
	a.config.Provider = name
	// Reset model so the new provider uses its default.
	a.config.Model = ""

	p, err := a.providerFactory(a.config)
	if err != nil {
		// Revert on failure.
		a.config.Provider = oldName
		a.io.Error(fmt.Sprintf("Failed to switch provider: %v", err))
		return true
	}
	a.provider = p
	a.summarizer = &session.LLMSummarizer{Provider: p}
	a.rebuildSystemPrompt()
	a.io.SystemMessage(fmt.Sprintf("Provider switched: %s → %s (model: %s)",
		oldName, name, p.DefaultModel()))

	// Persist provider switch (and any new credentials) to config file.
	// Always save so that stale global model overrides are cleared.
	pc2 := *a.config.GetProviderConfig(name)
	if err := config.SaveProviderToFile(name, pc2); err != nil {
		a.io.Error(fmt.Sprintf("Warning: failed to save config: %v", err))
	} else if needSave {
		home, _ := os.UserHomeDir()
		a.io.SystemMessage(fmt.Sprintf("Config saved to %s",
			filepath.Join(home, ".config", "apexion", "config.yaml")))
	}
	return true
}

func (a *Agent) handleConfig() bool {
	model := a.config.Model
	if model == "" {
		model = a.provider.DefaultModel()
	}
	maxIterDisplay := "unlimited"
	if a.config.MaxIterations > 0 {
		maxIterDisplay = fmt.Sprintf("%d", a.config.MaxIterations)
	}
	info := fmt.Sprintf(`Current configuration:
  Provider:       %s
  Model:          %s
  Context window: %d
  Max iterations: %s
  Permission:     %s
  Session ID:     %s
  Messages:       %d
  Tokens used:    %d`,
		a.config.Provider,
		model,
		a.provider.ContextWindow(),
		maxIterDisplay,
		a.config.Permissions.Mode,
		a.session.ID,
		len(a.session.Messages),
		a.session.TokensUsed,
	)
	a.io.SystemMessage(info)
	return true
}

func (a *Agent) handleSave() bool {
	if err := a.store.Save(a.session); err != nil {
		a.io.Error("Save failed: " + err.Error())
		return true
	}
	a.io.SystemMessage(fmt.Sprintf("Session saved: %s (%d messages)",
		a.session.ID[:8], len(a.session.Messages)))
	return true
}

func (a *Agent) handleSessions() bool {
	infos, err := a.store.List()
	if err != nil {
		a.io.Error("Failed to list sessions: " + err.Error())
		return true
	}
	if len(infos) == 0 {
		a.io.SystemMessage("No saved sessions.")
		return true
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Saved sessions (%d):\n", len(infos)))
	for i, info := range infos {
		if i >= 20 {
			sb.WriteString(fmt.Sprintf("  ... and %d more\n", len(infos)-20))
			break
		}
		sb.WriteString(fmt.Sprintf("  %s  %s  %d msgs  %d tokens\n",
			info.ID[:8],
			info.CreatedAt.Format("2006-01-02 15:04"),
			info.Messages,
			info.Tokens,
		))
	}
	sb.WriteString("Use /resume <id> to restore a session.")
	a.io.SystemMessage(sb.String())
	return true
}

func (a *Agent) handleResume(idPrefix string) bool {
	if idPrefix == "" {
		a.io.SystemMessage("Usage: /resume <session-id-prefix>")
		return true
	}

	infos, err := a.store.List()
	if err != nil {
		a.io.Error("Failed to list sessions: " + err.Error())
		return true
	}

	// Find sessions matching the prefix.
	var matches []session.SessionInfo
	for _, info := range infos {
		if strings.HasPrefix(info.ID, idPrefix) {
			matches = append(matches, info)
		}
	}

	switch len(matches) {
	case 0:
		a.io.Error(fmt.Sprintf("No session found matching prefix %q", idPrefix))
		return true
	case 1:
		// Unique match — load it.
	default:
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Ambiguous prefix %q matches %d sessions:\n", idPrefix, len(matches)))
		for _, m := range matches {
			sb.WriteString(fmt.Sprintf("  %s  %s\n", m.ID[:12], m.CreatedAt.Format("2006-01-02 15:04")))
		}
		sb.WriteString("Provide a longer prefix.")
		a.io.SystemMessage(sb.String())
		return true
	}

	loaded, err := a.store.Load(matches[0].ID)
	if err != nil {
		a.io.Error("Failed to load session: " + err.Error())
		return true
	}

	a.session = loaded
	a.io.SystemMessage(fmt.Sprintf("Resumed session %s (%d messages, %d tokens)",
		loaded.ID[:8], len(loaded.Messages), loaded.TokensUsed))
	return true
}

func formatHistory(messages []provider.Message) string {
	if len(messages) == 0 {
		return "No history."
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "\n=== History (%d messages) ===\n", len(messages))
	for i, msg := range messages {
		fmt.Fprintf(&sb, "[%d] %s:\n", i, msg.Role)
		for _, c := range msg.Content {
			switch c.Type {
			case provider.ContentTypeText:
				fmt.Fprintf(&sb, "    text: %s\n", truncate(c.Text, 100))
			case provider.ContentTypeToolUse:
				fmt.Fprintf(&sb, "    tool_use: %s(%s)\n", c.ToolName, truncate(string(c.ToolInput), 60))
			case provider.ContentTypeToolResult:
				status := "ok"
				if c.IsError {
					status = "err"
				}
				fmt.Fprintf(&sb, "    tool_result[%s]: %s\n", status, truncate(c.ToolResult, 60))
			}
		}
	}
	sb.WriteString("===")
	return sb.String()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func (a *Agent) handleMCP(ctx context.Context, arg string) bool {
	if a.mcpManager == nil {
		a.io.SystemMessage("MCP not configured. Create ~/.config/apexion/mcp.json or .apexion/mcp.json.")
		return true
	}

	switch strings.TrimSpace(arg) {
	case "reset":
		a.io.SystemMessage("Reconnecting MCP servers...")
		errs := a.mcpManager.Reset(ctx)
		if len(errs) > 0 {
			var sb strings.Builder
			sb.WriteString("MCP reconnect errors:\n")
			for _, e := range errs {
				sb.WriteString("  " + e.Error() + "\n")
			}
			a.io.SystemMessage(sb.String())
		} else {
			a.io.SystemMessage("MCP servers reconnected.")
		}

	default:
		// Show connection status
		status := a.mcpManager.Status()
		if len(status) == 0 {
			a.io.SystemMessage("No MCP servers configured.")
			return true
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("MCP servers (%d):\n", len(status)))
		names := make([]string, 0, len(status))
		for n := range status {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			sb.WriteString(fmt.Sprintf("  %-20s %s\n", n, status[n]))
		}
		sb.WriteString("\nUse /mcp reset to reconnect all servers.")
		a.io.SystemMessage(sb.String())
	}

	return true
}

// CustomCommandItems scans custom command directories and returns SlashMenuItems
// for use in the TUI autocomplete menu. Called from cmd layer before agent creation.
func CustomCommandItems(cwd string) []tui.SlashMenuItem {
	cmds := loadCustomCommands(cwd)
	if len(cmds) == 0 {
		return nil
	}
	names := make([]string, 0, len(cmds))
	for n := range cmds {
		names = append(names, n)
	}
	sort.Strings(names)
	items := make([]tui.SlashMenuItem, 0, len(cmds))
	for _, n := range names {
		desc := cmds[n].Description
		if desc == "" {
			desc = "(custom)"
		}
		items = append(items, tui.SlashMenuItem{
			Name: "/" + n,
			Desc: desc,
		})
	}
	return items
}

// wireTaskTool finds the TaskTool in the registry and injects the sub-agent runner.
func (a *Agent) wireTaskTool() {
	t, ok := a.executor.Registry().Get("task")
	if !ok {
		return
	}
	tt, ok := t.(*tools.TaskTool)
	if !ok {
		return
	}
	tt.SetRunner(a.runSubAgent)
}

// subAgentReporter is an optional interface for sending sub-agent progress to the TUI.
type subAgentReporter interface {
	ReportSubAgentProgress(tui.SubAgentProgress)
}

// runSubAgent creates and runs an ephemeral read-only sub-agent.
func (a *Agent) runSubAgent(ctx context.Context, prompt string) (string, error) {
	// If the main IO supports progress reporting, wire it up.
	var buf *tui.BufferIO
	if pr, ok := a.io.(subAgentReporter); ok {
		buf = tui.NewBufferIOWithProgress("", func(p tui.SubAgentProgress) {
			pr.ReportSubAgentProgress(p)
		})
	} else {
		buf = tui.NewBufferIO()
	}

	roRegistry := tools.ReadOnlyRegistry()
	roExecutor := tools.NewExecutor(roRegistry, permission.AllowAllPolicy{})

	subCfg := *a.config
	subCfg.MaxIterations = 0
	subCfg.SystemPrompt = subAgentSystemPrompt

	sub := &Agent{
		provider:   a.provider,
		executor:   roExecutor,
		config:     &subCfg,
		session:    session.New(),
		store:      session.NullStore{},
		basePrompt: subAgentSystemPrompt,
		io:         buf,
	}
	sub.rebuildSystemPrompt()

	err := sub.RunOnce(ctx, prompt)
	return buf.Output(), err
}
