package agent

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aictl/aictl/internal/config"
	"github.com/aictl/aictl/internal/provider"
	"github.com/aictl/aictl/internal/session"
	"github.com/aictl/aictl/internal/tools"
)

const defaultSystemPrompt = `You are aictl, an AI coding assistant running in the terminal.
You help users with software engineering tasks by reading and modifying files,
running commands, and searching codebases.

Rules:
- Before making changes, read the relevant files first
- Always explain what you're about to do before calling tools
- Be concise in your explanations
- If a command might be destructive, warn the user explicitly`

// Agent orchestrates the interactive loop between user, LLM, and tools.
type Agent struct {
	provider     provider.Provider
	executor     *tools.Executor
	config       *config.Config
	session      *session.Session
	systemPrompt string
}

// New creates a new Agent.
func New(p provider.Provider, exec *tools.Executor, cfg *config.Config) *Agent {
	sp := defaultSystemPrompt
	if cfg.SystemPrompt != "" {
		sp = cfg.SystemPrompt
	}
	return &Agent{
		provider:     p,
		executor:     exec,
		config:       cfg,
		session:      session.New(),
		systemPrompt: sp,
	}
}

// Run starts the interactive REPL loop, reading from stdin.
func (a *Agent) Run(ctx context.Context) error {
	fmt.Println("aictl — type your request, /quit to exit")
	fmt.Println(strings.Repeat("-", 50))

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for {
		fmt.Print("\n> ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
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

		a.session.AddMessage(provider.Message{
			Role: provider.RoleUser,
			Content: []provider.Content{{
				Type: provider.ContentTypeText,
				Text: input,
			}},
		})

		if err := a.runAgentLoop(ctx); err != nil {
			if ctx.Err() != nil {
				fmt.Println("\nInterrupted.")
				_ = session.Save(a.session)
				return ctx.Err()
			}
			fmt.Fprintf(os.Stderr, "\nerror: %v\n", err)
		}
		fmt.Println()
	}

	_ = session.Save(a.session)
	return nil
}

// RunOnce executes a single prompt and exits (non-interactive mode).
func (a *Agent) RunOnce(ctx context.Context, prompt string) error {
	a.session.AddMessage(provider.Message{
		Role: provider.RoleUser,
		Content: []provider.Content{{
			Type: provider.ContentTypeText,
			Text: prompt,
		}},
	})

	if err := a.runAgentLoop(ctx); err != nil {
		return err
	}
	fmt.Println()
	return nil
}

// handleSlashCommand processes built-in commands.
// Returns (handled, shouldQuit).
func (a *Agent) handleSlashCommand(cmd string) (bool, bool) {
	switch cmd {
	case "/quit", "/exit", "/q":
		fmt.Println("Bye.")
		_ = session.Save(a.session)
		return true, true
	case "/clear":
		a.session.Clear()
		fmt.Println("Session cleared.")
		return true, false
	case "/history":
		printHistory(a.session.Messages)
		return true, false
	case "/cost":
		fmt.Printf("Estimated tokens used: %d\n", a.session.TokensUsed)
		return true, false
	default:
		return false, false
	}
}

func printHistory(messages []provider.Message) {
	if len(messages) == 0 {
		fmt.Println("No history.")
		return
	}
	fmt.Printf("\n=== History (%d messages) ===\n", len(messages))
	for i, msg := range messages {
		fmt.Printf("[%d] %s:\n", i, msg.Role)
		for _, c := range msg.Content {
			switch c.Type {
			case provider.ContentTypeText:
				fmt.Printf("    text: %s\n", truncate(c.Text, 100))
			case provider.ContentTypeToolUse:
				fmt.Printf("    tool_use: %s(%s)\n", c.ToolName, truncate(string(c.ToolInput), 60))
			case provider.ContentTypeToolResult:
				status := "ok"
				if c.IsError {
					status = "err"
				}
				fmt.Printf("    tool_result[%s]: %s\n", status, truncate(c.ToolResult, 60))
			}
		}
	}
	fmt.Println("===")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
