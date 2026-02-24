package agent

import (
	"context"

	"github.com/apexion-ai/apexion/internal/permission"
	"github.com/apexion-ai/apexion/internal/session"
	"github.com/apexion-ai/apexion/internal/tools"
	"github.com/apexion-ai/apexion/internal/tui"
)

const subAgentSystemPrompt = `You are a research sub-agent. Your job is to explore and gather information, then return a clear summary.

You have read-only tools: read_file, glob, grep, list_dir, repo_map, symbol_nav, doc_context, web_fetch, todo_read.
You CANNOT modify files, run commands, or make git changes.

Rules:
- Focus on the specific task given to you.
- Use tools to gather evidence. Do not guess.
- Return a concise, well-organized summary of your findings.
- If you cannot find what was asked, say so clearly.`

const planSubAgentSystemPrompt = `You are a planning sub-agent. Your job is to analyze the codebase and produce a detailed implementation plan.

You have read-only tools: read_file, glob, grep, list_dir, repo_map, symbol_nav, doc_context, web_fetch, todo_read.
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
