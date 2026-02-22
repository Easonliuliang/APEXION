package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/apexion-ai/apexion/internal/config"
	"github.com/apexion-ai/apexion/internal/mcp"
	"github.com/apexion-ai/apexion/internal/permission"
	"github.com/apexion-ai/apexion/internal/provider"
	"github.com/apexion-ai/apexion/internal/repomap"
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

const planSubAgentSystemPrompt = `You are a planning sub-agent. Your job is to analyze the codebase and produce a detailed implementation plan.

You have read-only tools: read_file, glob, grep, list_dir, web_fetch, todo_read.
You CANNOT modify files, run commands, or make git changes.

Rules:
- Thoroughly explore the codebase to understand the current architecture.
- Use tools to gather evidence about existing patterns and conventions.
- Structure your plan as:
  1. Files to modify (with full paths)
  2. Specific changes for each file (describe what to add/change)
  3. Verification steps (how to test the changes)
- Be specific and actionable. Include code snippets where helpful.`

const codeSubAgentSystemPrompt = `You are a coding sub-agent. You can read, write, and edit files, and run commands.

Your job is to complete the specific coding task given to you.

Rules:
- Focus exclusively on the task described in the prompt.
- Make minimal, targeted changes. Do not refactor or modify unrelated code.
- Use edit_file for modifying existing files. Use write_file only for new files.
- Run bash commands when needed (e.g. to build, test, or verify changes).
- When finished, provide a clear summary of all changes you made.
- Do NOT create unnecessary files or add features beyond what was asked.`

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
	planMode        bool
	rules           []Rule
	skills          []SkillInfo
	hookManager     *tools.HookManager
	eventLogger     *EventLogger
	checkpointMgr   *CheckpointManager
	costTracker     *CostTracker
	repoMap         *repomap.RepoMap
	bgManager       *BackgroundManager
	architectNext   bool // next prompt uses architect mode
	architectAuto   bool // architect auto-execute
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

	// Initialize cost tracker with optional user pricing overrides.
	var costOverrides map[string]ModelPricing
	if len(cfg.CostPricing) > 0 {
		costOverrides = make(map[string]ModelPricing, len(cfg.CostPricing))
		for model, entry := range cfg.CostPricing {
			costOverrides[model] = ModelPricing{
				InputPerMillion:  entry.InputPerMillion,
				OutputPerMillion: entry.OutputPerMillion,
			}
		}
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
		rules:          loadRules(cwd),
		skills:         loadSkills(cwd),
		costTracker:    NewCostTracker(costOverrides),
	}

	// Initialize repo map (async build in background).
	if !cfg.RepoMap.Disabled {
		maxTokens := cfg.RepoMap.MaxTokens
		if maxTokens <= 0 {
			maxTokens = 4096
		}
		a.repoMap = repomap.New(cwd, maxTokens, cfg.RepoMap.Exclude)
		go a.repoMap.Build()
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

// SetHookManager injects the hook manager for lifecycle hooks and /hooks command.
func (a *Agent) SetHookManager(hm *tools.HookManager) {
	a.hookManager = hm
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

	// Inject always-active rules.
	for _, r := range a.rules {
		if len(r.PathPatterns) == 0 {
			a.systemPrompt += "\n\n<rule name=\"" + r.Name + "\">\n" + r.Content + "\n</rule>"
		}
	}

	// Inject repo map if available and built.
	if a.repoMap != nil && a.repoMap.IsBuilt() {
		if mapContent := a.repoMap.Render(0); mapContent != "" {
			a.systemPrompt += "\n\n<repo_map>\n" + mapContent + "</repo_map>"
		}
	}

	// List available skills so the LLM knows what it can load.
	if len(a.skills) > 0 {
		a.systemPrompt += "\n\nAvailable project skills (load with read_file tool when you need detailed knowledge):"
		for _, s := range a.skills {
			desc := s.Desc
			if desc == "" {
				desc = s.Name
			}
			a.systemPrompt += "\n- " + s.Path + " — " + desc
		}
	}
}

// Run starts the interactive REPL loop.
func (a *Agent) Run(ctx context.Context) error {
	// Initialize event logger.
	if el, err := NewEventLogger(a.session.ID); err == nil {
		a.eventLogger = el
		defer a.eventLogger.Close()
		a.eventLogger.Log(EventSessionStart, map[string]string{
			"session_id": a.session.ID,
		})
	}

	// Initialize checkpoint manager.
	a.checkpointMgr = NewCheckpointManager(10)

	// Initialize background agent manager.
	a.bgManager = NewBackgroundManager(4, a.io)
	a.wireBGLauncher()

	// Fire session_start hooks.
	if a.hookManager != nil {
		a.hookManager.RunLifecycleHooks(ctx, tools.HookSessionStart, map[string]string{
			"session_id": a.session.ID,
		})
	}

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

		// Check if architect mode is pending for this prompt.
		if a.architectNext {
			a.architectNext = false
			a.io.UserMessage(input)
			am := NewArchitectMode(a, a.config.Architect.ArchitectModel, a.config.Architect.CoderModel, a.architectAuto)
			if err := am.Run(ctx, input); err != nil {
				a.io.Error(err.Error())
			}
			a.architectAuto = false
			continue
		}

		a.io.UserMessage(input)
		a.session.AddMessage(provider.Message{
			Role: provider.RoleUser,
			Content: []provider.Content{{
				Type: provider.ContentTypeText,
				Text: input,
			}},
		})

		if a.eventLogger != nil {
			a.eventLogger.Log(EventUserMessage, map[string]string{"text": input})
		}

		if err := a.runAgentLoop(ctx); err != nil {
			if ctx.Err() != nil {
				a.io.SystemMessage("\nInterrupted.")
				_ = a.store.Save(a.session)
				return ctx.Err()
			}
			a.io.Error(err.Error())
		}

		// Fire notification hooks after each agent turn completes.
		if a.hookManager != nil {
			a.hookManager.RunLifecycleHooks(ctx, tools.HookNotification, map[string]string{
				"session_id": a.session.ID,
			})
		}
	}

	// Wait for background agents before exiting.
	if a.bgManager != nil && a.bgManager.RunningCount() > 0 {
		a.io.SystemMessage("Waiting for background agents to complete...")
		a.bgManager.WaitAll(ctx)
	}

	// Show file change summary on exit if any files were modified.
	if changes := a.executor.FileTracker().Summary(); changes != "" {
		a.io.SystemMessage("\n--- Session file changes ---\n" + changes)
	}

	// Fire session_stop hooks.
	if a.hookManager != nil {
		a.hookManager.RunLifecycleHooks(ctx, tools.HookSessionStop, map[string]string{
			"session_id": a.session.ID,
		})
	}

	// Auto-extract memories from the conversation.
	if a.memoryStore != nil && len(a.session.Messages) > 5 {
		extractor := NewAutoMemoryExtractor(a.provider, a.memoryStore, a.config.SubAgentModel)
		if n, err := extractor.Extract(ctx, a.session.Messages, a.session.ID); err == nil && n > 0 {
			a.io.SystemMessage(fmt.Sprintf("Auto-extracted %d memories from this session.", n))
		}
	}

	// Log session end.
	if a.eventLogger != nil {
		a.eventLogger.Log(EventSessionEnd, map[string]string{
			"session_id":  a.session.ID,
			"tokens_used": fmt.Sprintf("%d", a.session.TokensUsed),
		})
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
		if a.costTracker != nil {
			a.io.SystemMessage(a.costTracker.Summary())
		} else {
			a.io.SystemMessage(fmt.Sprintf("Tokens used: %d", a.session.TokensUsed))
		}
		return true, false
	case "/test":
		return a.handleTest(arg), false
	case "/map":
		return a.handleMap(arg), false
	case "/architect":
		return a.handleArchitect(arg), false
	case "/bg":
		return a.handleBG(arg), false
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
	case "/plan":
		a.planMode = !a.planMode
		if a.planMode {
			a.io.SystemMessage("Plan mode ON — agent will analyze and propose, not execute.")
		} else {
			a.io.SystemMessage("Plan mode OFF — agent will execute normally.")
		}
		a.io.SetPlanMode(a.planMode)
		return true, false
	case "/rules":
		return a.handleRules(), false
	case "/skills":
		return a.handleSkills(), false
	case "/audit":
		return a.handleAudit(), false
	case "/hooks":
		return a.handleHooks(), false
	case "/events":
		return a.handleEvents(arg), false
	case "/checkpoint":
		return a.handleCheckpoint(arg), false
	case "/rollback":
		return a.handleRollback(arg), false
	case "/checkpoints":
		return a.handleCheckpoints(), false
	case "/autocommit":
		a.config.AutoCommit = !a.config.AutoCommit
		if a.config.AutoCommit {
			a.io.SystemMessage("Auto-commit ON — file edits will be committed automatically.")
		} else {
			a.io.SystemMessage("Auto-commit OFF.")
		}
		return true, false
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
	a.session.GentleCompactPhase = 0
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
  /plan              Toggle plan mode (read-only analysis)
  /compact           Manually trigger context compaction
  /changes           Show files modified in this session
  /trust             Show session-level tool approvals
  /trust reset       Clear all session approvals
  /rules             List loaded rules
  /skills            List available skills
  /commands           List custom commands
  /memory             List saved memories
  /memory add <text>  Save a memory (add tags with #tag)
  /memory search <q>  Search memories
  /memory delete <id> Delete a memory
  /mcp               Show MCP server connection status
  /mcp reset         Reconnect all MCP servers
  /hooks             List configured hooks
  /autocommit        Toggle auto-commit on/off
  /checkpoint [msg]  Create a checkpoint (git stash snapshot)
  /rollback [id]     Rollback to a checkpoint
  /checkpoints       List checkpoints
  /test <file>       Run configured test command for a file
  /map               Show repository map (function/type signatures)
  /map refresh       Rebuild the repository map
  /architect         Next prompt uses architect mode (big model plans, small executes)
  /architect auto    Architect mode with auto-execution
  /bg                List background agents
  /bg collect [id]   Collect completed agent output
  /bg cancel <id>    Cancel a running background agent
  /bg wait           Wait for all background agents
  /events [n]        Show recent event log entries
  /audit             Show bash command audit log
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

func (a *Agent) handleRules() bool {
	if len(a.rules) == 0 {
		a.io.SystemMessage("No rules loaded.\nPlace .md files in .apexion/rules/ or ~/.config/apexion/rules/")
		return true
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Loaded rules (%d):\n", len(a.rules)))
	for _, r := range a.rules {
		scope := "always active"
		if len(r.PathPatterns) > 0 {
			scope = "paths: " + strings.Join(r.PathPatterns, ", ")
		}
		desc := r.Description
		if desc == "" {
			desc = "(no description)"
		}
		sb.WriteString(fmt.Sprintf("  %-20s %s  [%s]\n", r.Name, desc, scope))
		sb.WriteString(fmt.Sprintf("    source: %s\n", r.Source))
	}
	a.io.SystemMessage(strings.TrimRight(sb.String(), "\n"))
	return true
}

func (a *Agent) handleSkills() bool {
	if len(a.skills) == 0 {
		a.io.SystemMessage("No skills found.\nPlace .md files in .apexion/skills/ or ~/.config/apexion/skills/")
		return true
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Available skills (%d):\n", len(a.skills)))
	for _, s := range a.skills {
		desc := s.Desc
		if desc == "" {
			desc = "(no description)"
		}
		sb.WriteString(fmt.Sprintf("  %-20s %s\n", s.Name, desc))
		sb.WriteString(fmt.Sprintf("    path: %s\n", s.Path))
	}
	a.io.SystemMessage(strings.TrimRight(sb.String(), "\n"))
	return true
}

func (a *Agent) handleAudit() bool {
	if a.config.Sandbox.AuditLog == "" {
		a.io.SystemMessage("Audit logging not configured.\nSet sandbox.audit_log in config.yaml")
		return true
	}
	data, err := os.ReadFile(a.config.Sandbox.AuditLog)
	if err != nil {
		a.io.SystemMessage("No audit log found at " + a.config.Sandbox.AuditLog)
		return true
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	// Show last 20 lines
	if len(lines) > 20 {
		lines = lines[len(lines)-20:]
	}
	a.io.SystemMessage(fmt.Sprintf("Audit log (last %d entries):\n%s", len(lines), strings.Join(lines, "\n")))
	return true
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

func (a *Agent) handleEvents(arg string) bool {
	if a.eventLogger == nil {
		a.io.SystemMessage("Event logging not available.")
		return true
	}
	n := 20
	if arg != "" {
		if parsed, err := strconv.Atoi(arg); err == nil && parsed > 0 {
			n = parsed
		}
	}
	events, err := a.eventLogger.ReadRecent(n)
	if err != nil {
		a.io.Error("Failed to read events: " + err.Error())
		return true
	}
	a.io.SystemMessage(FormatEvents(events, "Recent events"))
	return true
}

func (a *Agent) handleCheckpoint(label string) bool {
	if a.checkpointMgr == nil {
		a.io.SystemMessage("Checkpoint system not available.")
		return true
	}
	if label == "" {
		label = "manual checkpoint"
	}
	cp, err := a.checkpointMgr.Create(context.Background(), label)
	if err != nil {
		a.io.Error("Checkpoint failed: " + err.Error())
		return true
	}
	a.io.SystemMessage(fmt.Sprintf("Checkpoint created: %s — %s", cp.ID, cp.Label))
	return true
}

func (a *Agent) handleRollback(id string) bool {
	if a.checkpointMgr == nil {
		a.io.SystemMessage("Checkpoint system not available.")
		return true
	}
	err := a.checkpointMgr.Rollback(context.Background(), id)
	if err != nil {
		a.io.Error("Rollback failed: " + err.Error())
		return true
	}
	target := id
	if target == "" {
		target = "latest"
	}
	a.io.SystemMessage(fmt.Sprintf("Rolled back to checkpoint: %s", target))
	return true
}

func (a *Agent) handleCheckpoints() bool {
	if a.checkpointMgr == nil {
		a.io.SystemMessage("Checkpoint system not available.")
		return true
	}
	list := a.checkpointMgr.List()
	if len(list) == 0 {
		a.io.SystemMessage("No checkpoints.\nUse /checkpoint [label] to create one.")
		return true
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Checkpoints (%d):\n", len(list)))
	for _, cp := range list {
		sb.WriteString(fmt.Sprintf("  %s  %s  %s\n",
			cp.ID,
			cp.CreatedAt.Format("15:04:05"),
			cp.Label,
		))
	}
	a.io.SystemMessage(strings.TrimRight(sb.String(), "\n"))
	return true
}

func (a *Agent) handleHooks() bool {
	if a.hookManager == nil {
		a.io.SystemMessage("No hooks loaded.\nPlace hooks.yaml in .apexion/ or ~/.config/apexion/")
		return true
	}
	a.io.SystemMessage(a.hookManager.Summary())
	return true
}

func (a *Agent) handleTest(arg string) bool {
	if arg == "" {
		a.io.SystemMessage("Usage: /test <file_path>\nRuns the configured test command for the file.")
		return true
	}
	tr := tools.NewTestRunner(a.config.Test)
	if tr == nil {
		a.io.SystemMessage("Test runner not configured.\nAdd test commands to config.yaml:\n  test:\n    enabled: true\n    commands:\n      \".go\": \"go test ./... -count=1\"")
		return true
	}
	output, passed, err := tr.Run(context.Background(), arg)
	if err != nil {
		a.io.Error("Test error: " + err.Error())
	} else if passed {
		a.io.SystemMessage("Tests passed.")
	} else {
		a.io.SystemMessage("Test failures:\n" + output)
	}
	return true
}

func (a *Agent) handleMap(arg string) bool {
	if a.repoMap == nil {
		a.io.SystemMessage("Repo map is disabled.\nEnable in config.yaml:\n  repo_map:\n    disabled: false")
		return true
	}

	if arg == "refresh" {
		a.io.SystemMessage("Refreshing repo map...")
		go func() {
			if err := a.repoMap.Build(); err != nil {
				a.io.Error("Repo map refresh failed: " + err.Error())
			} else {
				a.rebuildSystemPrompt()
				a.io.SystemMessage(fmt.Sprintf("Repo map refreshed: %d files, %d symbols.",
					a.repoMap.FileCount(), a.repoMap.SymbolCount()))
			}
		}()
		return true
	}

	if !a.repoMap.IsBuilt() {
		a.io.SystemMessage("Repo map is still building...")
		return true
	}

	content := a.repoMap.Render(0)
	if content == "" {
		a.io.SystemMessage("Repo map is empty (no supported files found).")
	} else {
		a.io.SystemMessage(fmt.Sprintf("Repo map (%d files, %d symbols):\n%s",
			a.repoMap.FileCount(), a.repoMap.SymbolCount(), content))
	}
	return true
}

func (a *Agent) handleArchitect(arg string) bool {
	a.architectNext = true
	a.architectAuto = strings.TrimSpace(arg) == "auto"

	if a.architectAuto {
		a.io.SystemMessage("Architect mode (auto-execute): next prompt will use dual-model planning + execution.")
	} else {
		a.io.SystemMessage("Architect mode: next prompt will be analyzed by the architect model.\nYou'll review the plan before execution.")
	}
	return true
}

func (a *Agent) handleBG(arg string) bool {
	if a.bgManager == nil {
		a.io.SystemMessage("Background agent manager not available.")
		return true
	}

	parts := strings.SplitN(strings.TrimSpace(arg), " ", 2)
	subcmd := ""
	subarg := ""
	if len(parts) > 0 {
		subcmd = parts[0]
	}
	if len(parts) > 1 {
		subarg = strings.TrimSpace(parts[1])
	}

	switch subcmd {
	case "collect":
		if subarg == "" {
			// Collect all completed
			results := a.bgManager.CollectAll()
			if len(results) == 0 {
				a.io.SystemMessage("No completed background agents to collect.")
				return true
			}
			for _, r := range results {
				if r.Error != "" {
					a.io.SystemMessage(fmt.Sprintf("[%s] Error: %s\n%s", r.ID, r.Error, r.Output))
				} else {
					a.io.SystemMessage(fmt.Sprintf("[%s] Output:\n%s", r.ID, r.Output))
				}
				// Inject into conversation
				a.session.AddMessage(provider.Message{
					Role: provider.RoleUser,
					Content: []provider.Content{{
						Type: provider.ContentTypeText,
						Text: fmt.Sprintf("[Background agent %s output]\n%s", r.ID, r.Output),
					}},
				})
			}
		} else {
			output, err := a.bgManager.Collect(subarg)
			if err != nil {
				a.io.Error(err.Error())
				return true
			}
			a.io.SystemMessage(fmt.Sprintf("[%s] Output:\n%s", subarg, output))
			a.session.AddMessage(provider.Message{
				Role: provider.RoleUser,
				Content: []provider.Content{{
					Type: provider.ContentTypeText,
					Text: fmt.Sprintf("[Background agent %s output]\n%s", subarg, output),
				}},
			})
		}

	case "cancel":
		if subarg == "" {
			a.io.Error("Usage: /bg cancel <id>")
			return true
		}
		if err := a.bgManager.Cancel(subarg); err != nil {
			a.io.Error(err.Error())
		} else {
			a.io.SystemMessage(fmt.Sprintf("Background agent %s cancelled.", subarg))
		}

	case "wait":
		a.io.SystemMessage("Waiting for all background agents...")
		a.bgManager.WaitAll(context.Background())
		a.io.SystemMessage("All background agents completed.")

	default:
		a.io.SystemMessage(a.bgManager.Summary())
	}

	return true
}

// wireBGLauncher wires the background manager to the task tool.
func (a *Agent) wireBGLauncher() {
	if a.bgManager == nil {
		return
	}
	t, ok := a.executor.Registry().Get("task")
	if !ok {
		return
	}
	tt, ok := t.(*tools.TaskTool)
	if !ok {
		return
	}
	tt.SetBGLauncher(a.bgManager)
}

// wireTaskTool finds the TaskTool in the registry and injects the sub-agent runner
// and confirmer (for code mode).
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
	// Wire confirmer for code mode confirmation (if available).
	if c, ok := a.io.(tools.Confirmer); ok {
		tt.SetConfirmer(c)
	}
}

// subAgentReporter is an optional interface for sending sub-agent progress to the TUI.
type subAgentReporter interface {
	ReportSubAgentProgress(tui.SubAgentProgress)
}

// runSubAgent creates and runs an ephemeral sub-agent.
// mode is "explore" (default), "plan", or "code".
func (a *Agent) runSubAgent(ctx context.Context, prompt string, mode string) (string, error) {
	// If the main IO supports progress reporting, wire it up.
	var buf *tui.BufferIO
	if pr, ok := a.io.(subAgentReporter); ok {
		buf = tui.NewBufferIOWithProgress("", func(p tui.SubAgentProgress) {
			pr.ReportSubAgentProgress(p)
		})
	} else {
		buf = tui.NewBufferIO()
	}

	var executor *tools.Executor
	var sysPrompt string

	switch mode {
	case "code":
		// Code sub-agent gets write permissions with AllowAll policy
		// (user already confirmed via the confirmer at task call time).
		codeRegistry := tools.CodeRegistry()
		executor = tools.NewExecutor(codeRegistry, permission.AllowAllPolicy{})
		sysPrompt = codeSubAgentSystemPrompt
	case "plan":
		roRegistry := tools.ReadOnlyRegistry()
		executor = tools.NewExecutor(roRegistry, permission.AllowAllPolicy{})
		sysPrompt = planSubAgentSystemPrompt
	default: // "explore"
		roRegistry := tools.ReadOnlyRegistry()
		executor = tools.NewExecutor(roRegistry, permission.AllowAllPolicy{})
		sysPrompt = subAgentSystemPrompt
	}

	subCfg := *a.config
	subCfg.MaxIterations = 0
	subCfg.SystemPrompt = sysPrompt
	// Use dedicated sub-agent model if configured.
	if a.config.SubAgentModel != "" {
		subCfg.Model = a.config.SubAgentModel
	}

	sub := &Agent{
		provider:   a.provider,
		executor:   executor,
		config:     &subCfg,
		session:    session.New(),
		store:      session.NullStore{},
		basePrompt: sysPrompt,
		io:         buf,
	}
	sub.rebuildSystemPrompt()

	err := sub.RunOnce(ctx, prompt)
	return buf.Output(), err
}
