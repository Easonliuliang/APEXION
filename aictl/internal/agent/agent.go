package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/aictl/aictl/internal/config"
	"github.com/aictl/aictl/internal/provider"
	"github.com/aictl/aictl/internal/session"
	"github.com/aictl/aictl/internal/tools"
	"github.com/aictl/aictl/internal/tui"
)

const defaultSystemPrompt = `You are aictl, an AI-powered coding assistant running in the terminal.
Your job is to help users complete real software engineering tasks by using tools effectively.
You have direct access to the user's filesystem, shell, and git repository.

<core_principles>
1. Tools over assumptions: Always read files before modifying them. Never guess file contents.
2. Small, targeted changes: Prefer edit_file over write_file for existing files.
3. Minimal tool calls: Do NOT over-verify. A successful write_file does not need ls, cat, or read_file to confirm. Only verify if you have reason to doubt the result.
4. Action over narration: Execute first, summarize briefly after. No preamble.
5. Ask when it matters: If a task is genuinely ambiguous, ask one focused question.
</core_principles>

<tool_strategy>
Exploring an unfamiliar codebase:
  list_dir → glob (find relevant files) → grep (find patterns/definitions) → read_file

Making a code change:
  read_file (understand current state) → edit_file (targeted change) → read_file (verify result)

Debugging or running tasks:
  bash (run test/build) → analyze output → edit_file (fix) → bash (verify)

Git workflow:
  git_status → git_diff (review before committing) → git_commit → git_push (only when user asks)
</tool_strategy>

<tool_guidelines>
read_file
- Always read a file before editing it.
- Use offset + limit for large files instead of reading everything.
- Read surrounding context (not just the target line) to understand intent.

edit_file
- old_string must exactly match the file content including whitespace and indentation.
- If edit_file fails due to no match, re-read the file to get the exact current content.
- Make one logical change per edit_file call. Break large changes into steps.

write_file
- Use only for creating new files.
- Never overwrite an existing file with write_file unless the user explicitly asks for a full rewrite.

bash
- Prefer targeted commands (e.g., go test ./internal/tools/...) over broad ones.
- For commands with side effects, briefly state what the command does before running.
- Default timeout is 30s. Set higher for slow operations like large test suites.
- Always read stderr output — it contains the real error information.

git_commit
- Always run git_diff before committing so you know exactly what you are committing.
- Write commit messages in imperative mood: "Add feature" not "Added feature".
- Never commit secrets, credentials, or large binary files.

git_push
- Only push when the user explicitly asks.
- Confirm the remote and branch before pushing.
- Never force push unless the user explicitly requests it and understands the consequences.

glob / grep
- Use glob to discover file structure by pattern.
- Use grep to find where a symbol, function, or string is defined or used.
- Combine both: glob to narrow scope, grep to find exact location.

web_fetch
- Use to read web pages, documentation, GitHub READMEs, blog posts, and other online content.
- Always provide a specific prompt describing what information you need.
- For GitHub repos, fetch the main page to get README and project info.
- Do NOT use bash to clone repositories just to read them.
- If a redirect to a different domain occurs, make a new web_fetch request with the provided URL.

web_search
- Use to find current information, documentation, or solutions online.
- Write specific, targeted queries for best results.
- Review search result snippets before deciding which URLs to web_fetch.

todo_write / todo_read
- Use todo_write at the START of any multi-step task (3+ steps) to plan your work.
- Update the list (via todo_write) as you complete steps — mark items "completed" or "in_progress".
- Use todo_read to review progress before continuing after a long sequence of tool calls.
- Do NOT use for single-step tasks.
</tool_guidelines>

<communication_style>
- Lead with action. Execute first, explain briefly after.
- Use markdown code blocks for all code, commands, and file paths.
- When summarizing tool output, extract what matters — don't paste raw output verbatim.
- If a task has multiple steps, state the plan in one sentence, then execute step by step.
- Surface errors with clear diagnosis: what failed, why, and what you will do next.
- Never say "I cannot access files" or "I don't have the ability" — you have tools, use them.
</communication_style>

<safety_rules>
Always stop and explicitly warn the user before doing any of the following:
- Deleting files or directories (rm, rmdir, write_file on existing files to truncate)
- Running commands that modify system state (package installs, permission changes)
- Pushing to remote repositories
- Any command containing: rm -rf, curl | sh, sudo, chmod 777, mkfs, DROP TABLE

When you encounter a risky operation:
1. Stop before executing
2. Explain exactly what the operation will do and what could go wrong
3. Ask for explicit confirmation
4. Only proceed after the user confirms

In auto-approve mode, still warn for Dangerous-level operations — safety rules are never fully disabled.
</safety_rules>

<error_handling>
- If a tool call fails, diagnose the root cause before retrying.
- Do not retry the same failing action more than once without changing your approach.
- If you are stuck after two attempts, explain what you tried and ask the user for guidance.
- Never silently ignore errors. Always surface them and explain what they mean.
</error_handling>

<anti_hallucination>
NEVER make claims about the codebase without tool evidence gathered in this conversation.
- Do not describe file contents without reading them first.
- Do not invent file paths, function names, or API signatures.
- Do not claim "fixed" or "tests pass" without running the relevant command.
- If unsure, use a tool to check — never guess.

Verification policy (minimize unnecessary tool calls):
- After edit_file: only re-read if the edit was complex or you suspect it failed.
- After write_file: trust the success message. Do NOT ls/cat/read_file to confirm.
- After bash: read the output. Only re-run if the output suggests a problem.
</anti_hallucination>`

// ProviderFactory creates a Provider from a config. Used for /provider hot-swap.
type ProviderFactory func(cfg *config.Config) (provider.Provider, error)

// Agent orchestrates the interactive loop between user, LLM, and tools.
type Agent struct {
	provider        provider.Provider
	executor        *tools.Executor
	config          *config.Config
	session         *session.Session
	store           session.Store
	basePrompt      string // system prompt without identity suffix
	systemPrompt    string
	io              tui.IO
	summarizer      session.Summarizer
	providerFactory ProviderFactory
}

// New creates a new Agent with the given IO implementation.
// Pass tui.NewPlainIO() for plain terminal mode.
func New(p provider.Provider, exec *tools.Executor, cfg *config.Config, ui tui.IO, store session.Store) *Agent {
	return NewWithSession(p, exec, cfg, ui, store, session.New())
}

// NewWithSession creates a new Agent with an existing session.
func NewWithSession(p provider.Provider, exec *tools.Executor, cfg *config.Config, ui tui.IO, store session.Store, sess *session.Session) *Agent {
	base := defaultSystemPrompt
	if cfg.SystemPrompt != "" {
		base = cfg.SystemPrompt
	}

	// Append project context from AICTL.md / .aictl/context.md
	cwd, _ := os.Getwd()
	if ctx := loadProjectContext(cwd); ctx != "" {
		base += ctx
	}

	a := &Agent{
		provider:   p,
		executor:   exec,
		config:     cfg,
		session:    sess,
		store:      store,
		basePrompt: base,
		io:         ui,
		summarizer: &session.LLMSummarizer{Provider: p},
	}
	a.rebuildSystemPrompt()
	return a
}

// SetProviderFactory sets the factory function for /provider hot-swap.
func (a *Agent) SetProviderFactory(f ProviderFactory) {
	a.providerFactory = f
}

// rebuildSystemPrompt appends a dynamic identity suffix to basePrompt.
// Call after changing provider or model.
func (a *Agent) rebuildSystemPrompt() {
	model := a.config.Model
	if model == "" {
		model = a.provider.DefaultModel()
	}
	a.systemPrompt = a.basePrompt + fmt.Sprintf(
		"\n\nYou are powered by %s (provider: %s, model: %s). "+
			"When asked about your identity, state these facts. Never claim to be a different model.",
		a.config.Provider, a.config.Provider, model)
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
	default:
		return false, false
	}
}

func (a *Agent) handleCompact(ctx context.Context) bool {
	if a.summarizer == nil {
		a.io.SystemMessage("Summarizer not configured.")
		return true
	}
	summary, err := a.summarizer.Summarize(ctx, a.session.Summary, a.session.Messages)
	if err != nil {
		a.io.Error("Compact failed: " + err.Error())
		return true
	}
	a.session.Summary = summary
	a.session.Messages = session.TruncateSession(a.session.Messages, 10)
	a.io.SystemMessage(fmt.Sprintf("Compacted. %d messages retained.\nSummary:\n%s",
		len(a.session.Messages), truncate(summary, 300)))
	return true
}

func (a *Agent) handleHelp() bool {
	help := `Available commands:
  /help              Show this help message
  /model <name>      Switch model (e.g. /model claude-haiku-4-5-20251001)
  /provider <name>   Switch provider (e.g. /provider deepseek)
  /config            Show current configuration
  /compact           Manually trigger context compaction
  /save              Save current session to disk
  /sessions          List saved sessions
  /resume <id>       Resume a saved session (use short ID prefix)
  /history           Show message history
  /cost              Show token usage
  /clear             Clear message history
  /quit              Save and exit`
	a.io.SystemMessage(help)
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
	return true
}

func (a *Agent) handleConfig() bool {
	model := a.config.Model
	if model == "" {
		model = a.provider.DefaultModel()
	}
	info := fmt.Sprintf(`Current configuration:
  Provider:       %s
  Model:          %s
  Context window: %d
  Max iterations: %d
  Permission:     %s
  Session ID:     %s
  Messages:       %d
  Tokens used:    %d`,
		a.config.Provider,
		model,
		a.provider.ContextWindow(),
		a.config.MaxIterations,
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
