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
1. Tools over assumptions: Never guess or assume file contents. Always read files before discussing or modifying them.
2. Small, targeted changes: Prefer edit_file for precise edits over write_file (full rewrite). Surgical changes are safer and easier to review.
3. Verify your work: After editing a file, read the changed section to confirm correctness.
4. Action over narration: Don't explain at length what you're about to do — take action, then briefly summarize the result.
5. Ask when it matters: If a task is genuinely ambiguous, ask one focused question. Never ask multiple questions at once.
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
These rules are absolute. Violating them produces incorrect results that mislead the user.

NEVER make claims about the codebase without tool evidence gathered in this conversation.

## Forbidden patterns

1. Describing file contents without having called read_file on that file.
   Wrong: "The main function probably initializes the server..."
   Right: call read_file → then describe what you actually read.

2. Inventing file paths, function names, or package names.
   Wrong: "The config is likely in internal/config/settings.go"
   Right: call glob or grep → report what actually exists.

3. Referencing Go standard library or third-party API signatures from memory.
   Wrong: "Use os.ReadFileLines() to read lines..."
   Right: check existing usage via grep, or read the relevant source file.
   Reason: Go APIs are strict. An invented function name causes a compile error.

4. Claiming a fix works without verifying it.
   Wrong: "I've updated the function, it should work now."
   Right: call read_file to verify the edit applied correctly,
          then run the relevant test or build command to confirm.

5. Saying "the tests pass" or "the build succeeds" without running them.
   Wrong: "This change should make the tests pass."
   Right: call bash → report the actual output.

6. Summarizing tool output you did not actually receive.
   Wrong: "Running go test would show..."
   Right: run it, then report the real output.

7. Referencing a file from memory after many steps have passed.
   If more than 5 tool calls have occurred since you last read a file,
   and you need to make claims about it, re-read the relevant section first.
   Reason: files may have been edited since you last read them.

8. Filling knowledge gaps with plausible-sounding guesses.
   Wrong: [confidently stating something unverified]
   Right: "I don't know — let me check." → use a tool → report real findings.

## Required verifications

After every edit_file call:
  MUST call read_file on the modified section before proceeding.
  Confirm the change is exactly what was intended.

After fixing a bug or error:
  MUST run the relevant bash command (build, test, or the failing command).
  Do not say "fixed" until you have tool output proving it.

After searching for something that does not exist:
  Report exactly what you searched for and what was found.
  Do not suggest alternatives that you have not verified exist.

## Knowledge source discipline

Clearly distinguish between:
- Tool evidence (reliable): "read_file showed...", "grep found...", "bash output:"
- Training memory (unreliable for this project): anything you "know" without a tool call

When making factual claims about this specific codebase, always cite the tool call
that produced the evidence. If you cannot cite one, make the tool call first.

Admitting uncertainty and using a tool is always correct.
Fabricating a confident answer is always wrong.
</anti_hallucination>`

// Agent orchestrates the interactive loop between user, LLM, and tools.
type Agent struct {
	provider     provider.Provider
	executor     *tools.Executor
	config       *config.Config
	session      *session.Session
	systemPrompt string
	io           tui.IO
}

// New creates a new Agent with the given IO implementation.
// Pass tui.NewPlainIO() for plain terminal mode.
func New(p provider.Provider, exec *tools.Executor, cfg *config.Config, ui tui.IO) *Agent {
	sp := defaultSystemPrompt
	if cfg.SystemPrompt != "" {
		sp = cfg.SystemPrompt
	}

	// Append project context from AICTL.md / .aictl/context.md
	cwd, _ := os.Getwd()
	if ctx := loadProjectContext(cwd); ctx != "" {
		sp += ctx
	}

	return &Agent{
		provider:     p,
		executor:     exec,
		config:       cfg,
		session:      session.New(),
		systemPrompt: sp,
		io:           ui,
	}
}

// Run starts the interactive REPL loop.
func (a *Agent) Run(ctx context.Context) error {
	a.io.SystemMessage("aictl — type your request, /quit to exit")
	a.io.SystemMessage(strings.Repeat("-", 50))

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
			handled, shouldQuit := a.handleSlashCommand(input)
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
				_ = session.Save(a.session)
				return ctx.Err()
			}
			a.io.Error(err.Error())
		}
	}

	_ = session.Save(a.session)
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
func (a *Agent) handleSlashCommand(cmd string) (bool, bool) {
	switch cmd {
	case "/quit", "/exit", "/q":
		a.io.SystemMessage("Bye.")
		_ = session.Save(a.session)
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
	default:
		return false, false
	}
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
