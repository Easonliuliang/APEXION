package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/apexion-ai/apexion/internal/permission"
)

// Confirmer is a minimal interface the Executor uses for permission prompts.
// This avoids a circular import with the tui package.
type Confirmer interface {
	Confirm(name, params string, level PermissionLevel) bool
}

// ToolCanceller allows the UI layer to register/clear a cancel function
// for the currently running tool, enabling Esc-to-cancel.
type ToolCanceller interface {
	SetToolCancel(cancel context.CancelFunc)
	ClearToolCancel()
}

// LoopCanceller allows the UI layer to cancel the entire agent loop
// (e.g. Esc during LLM streaming). Per-turn, not per-session.
type LoopCanceller interface {
	SetLoopCancel(cancel context.CancelFunc)
	ClearLoopCancel()
}

// Executor handles tool execution with permission checks and timeout control.
type Executor struct {
	registry       *Registry
	confirmer      Confirmer
	policy         permission.Policy
	defaultTimeout time.Duration
	toolCanceller  ToolCanceller
	tracker        *FileTracker
	confirmMu      sync.Mutex     // serializes confirmation dialogs during parallel execution
	hooks          *HookManager   // pre/post tool hooks (nil = no hooks)
	autoCommitter  *AutoCommitter // auto-commit after file edits (nil = disabled)
	linter         *Linter        // lint after file edits (nil = disabled)
	testRunner     *TestRunner    // test after file edits (nil = disabled)
}

// NewExecutor creates a tool executor.
func NewExecutor(registry *Registry, policy permission.Policy) *Executor {
	return &Executor{
		registry:       registry,
		policy:         policy,
		defaultTimeout: 300 * time.Second,
		tracker:        NewFileTracker(),
	}
}

// FileTracker returns the executor's file change tracker.
func (e *Executor) FileTracker() *FileTracker {
	return e.tracker
}

// SetHooks injects the hook manager for pre/post tool hooks.
func (e *Executor) SetHooks(hm *HookManager) {
	e.hooks = hm
}

// SetAutoCommitter injects the auto-committer for automatic git commits after file edits.
func (e *Executor) SetAutoCommitter(ac *AutoCommitter) {
	e.autoCommitter = ac
}

// AutoCommitter returns the executor's auto-committer (may be nil).
func (e *Executor) AutoCommitter() *AutoCommitter {
	return e.autoCommitter
}

// SetLinter injects the linter for automatic linting after file edits.
func (e *Executor) SetLinter(l *Linter) {
	e.linter = l
}

// SetTestRunner injects the test runner for automatic testing after file edits.
func (e *Executor) SetTestRunner(tr *TestRunner) {
	e.testRunner = tr
}

// SetConfirmer injects the UI-layer confirmer (called after New to avoid
// circular dependencies between agent, tui, and tools packages).
func (e *Executor) SetConfirmer(c Confirmer) {
	e.confirmer = c

	// Also wire Questioner if the confirmer implements it.
	if q, ok := c.(Questioner); ok {
		if qt, ok := e.registry.Get("question"); ok {
			if qTool, ok := qt.(*QuestionTool); ok {
				qTool.SetQuestioner(q)
			}
		}
	}
}

// SetToolCanceller injects the UI-layer cancel bridge so that Esc can
// cancel the currently running tool.
func (e *Executor) SetToolCanceller(tc ToolCanceller) {
	e.toolCanceller = tc
}

// Registry returns the underlying tool registry.
func (e *Executor) Registry() *Registry {
	return e.registry
}

// Policy returns the underlying permission policy.
func (e *Executor) Policy() permission.Policy {
	return e.policy
}

// Execute runs a single tool call.
func (e *Executor) Execute(ctx context.Context, name string, params json.RawMessage) ToolResult {
	tool, ok := e.registry.Get(name)
	if !ok {
		return ToolResult{Content: fmt.Sprintf("unknown tool: %s", name), IsError: true}
	}

	// Check if the loop context was already cancelled (user pressed Esc
	// during streaming before we even got to tool execution).
	if ctx.Err() == context.Canceled {
		return ToolResult{
			Content:       "[User cancelled — tool was not executed]",
			IsError:       false,
			UserCancelled: true,
		}
	}

	// Permission check via policy.
	decision := e.policy.Check(name, params)
	switch decision {
	case permission.Deny:
		// Policy denial — NOT user cancellation. The LLM should see the
		// reason and adjust its approach. Loop continues.
		return ToolResult{Content: "Blocked: tool execution denied by policy", IsError: true}
	case permission.NeedConfirmation:
		if e.confirmer != nil {
			// Serialize confirmation prompts so only one dialog is shown at a time.
			e.confirmMu.Lock()
			approved := e.confirmer.Confirm(name, string(params), tool.PermissionLevel())
			e.confirmMu.Unlock()

			if !approved {
				// User pressed Esc on confirmation — this IS user cancellation.
				// Stop the loop, return to user input.
				return ToolResult{
					Content:       "[User cancelled — tool was not executed]",
					IsError:       false,
					UserCancelled: true,
				}
			}
			// Remember this approval for the session so similar calls auto-approve.
			if dp, ok := e.policy.(*permission.DefaultPolicy); ok {
				dp.RememberApproval(name, params)
			}
		}
	case permission.Allow:
		// proceed
	}

	// Run pre-tool hooks.
	if e.hooks != nil {
		hookResult := e.hooks.RunPreHooks(ctx, name, params)
		if hookResult.Blocked {
			return ToolResult{
				Content: fmt.Sprintf("Blocked by hook: %s", hookResult.Message),
				IsError: true,
			}
		}
	}

	ctx, cancel := context.WithTimeout(ctx, e.defaultTimeout)
	defer cancel()

	// Wrap with a separate cancel so the UI can cancel this specific tool.
	ctx, toolCancel := context.WithCancel(ctx)
	defer toolCancel()
	if e.toolCanceller != nil {
		e.toolCanceller.SetToolCancel(toolCancel)
		defer e.toolCanceller.ClearToolCancel()
	}

	// For write_file, check existence before execution to distinguish create vs modify.
	isNewFile := false
	if name == "write_file" {
		var p struct {
			FilePath string `json:"file_path"`
		}
		if json.Unmarshal(params, &p) == nil && p.FilePath != "" {
			if _, err := os.Stat(p.FilePath); os.IsNotExist(err) {
				isNewFile = true
			}
		}
	}

	result, err := tool.Execute(ctx, params)
	if err != nil {
		if ctx.Err() == context.Canceled {
			// User pressed Esc during tool execution.
			return ToolResult{
				Content:       "[User cancelled — tool was not executed]",
				IsError:       false,
				UserCancelled: true,
			}
		}
		return ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}

	// Track file changes on successful write operations.
	if !result.IsError {
		trackFileChange(e.tracker, name, params, isNewFile)
	}

	// Run linter on successful file write/edit operations.
	if !result.IsError && e.linter != nil && (name == "write_file" || name == "edit_file") {
		filePath := extractFilePath(name, params)
		if filePath != "" {
			if lintOutput, hasErrors, lintErr := e.linter.Run(ctx, filePath); lintErr == nil && hasErrors {
				result.Content += "\n\n[Lint errors]\n" + lintOutput
			}
		}
	}

	// Run tests on successful file write/edit operations (after lint, before auto-commit).
	if !result.IsError && e.testRunner != nil && (name == "write_file" || name == "edit_file") {
		filePath := extractFilePath(name, params)
		if filePath != "" {
			if testOutput, passed, testErr := e.testRunner.Run(ctx, filePath); testErr == nil && !passed {
				result.Content += "\n\n[Test failures]\n" + testOutput +
					"\n\nFix the test failures in the code you just edited."
			}
		}
	}

	// Auto-commit on successful file write/edit operations.
	if !result.IsError && e.autoCommitter != nil && (name == "write_file" || name == "edit_file") {
		filePath := extractFilePath(name, params)
		if filePath != "" {
			e.autoCommitter.TryCommit(ctx, filePath, name)
		}
	}

	limit := toolOutputLimit(name)
	if len(result.Content) > limit {
		result.Content = truncateHeadTail(result.Content, limit)
		result.Truncated = true
	}

	// Run post-tool hooks (failures are silently ignored).
	if e.hooks != nil {
		e.hooks.RunPostHooks(ctx, name, params, result.Content, result.IsError)
	}

	return result
}

// toolOutputLimit returns the output byte limit for a given tool.
func toolOutputLimit(name string) int {
	switch name {
	case "read_file", "grep", "bash", "web_fetch", "web_search", "repo_map", "symbol_nav", "doc_context":
		return 32 * 1024 // 32KB ~8K tokens
	case "git_diff", "git_status", "git_log", "git_branch", "list_dir", "glob":
		return 16 * 1024 // 16KB
	default: // edit_file, write_file, git_commit, git_push, etc.
		return 4 * 1024 // 4KB
	}
}

// extractFilePath extracts the file_path parameter from write_file/edit_file params.
func extractFilePath(toolName string, params json.RawMessage) string {
	var p struct {
		FilePath string `json:"file_path"`
	}
	if json.Unmarshal(params, &p) == nil {
		return p.FilePath
	}
	return ""
}

// truncateHeadTail keeps the head (60%) and tail (40%) of a string,
// omitting the middle. Tail content (errors, final results) is often more important.
func truncateHeadTail(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	head := maxLen * 3 / 5 // 60%
	tail := maxLen * 2 / 5 // 40%
	omitted := len(s) - head - tail
	return s[:head] + fmt.Sprintf("\n\n[...%d chars omitted...]\n\n", omitted) + s[len(s)-tail:]
}
