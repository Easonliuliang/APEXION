package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/apexion-ai/apexion/internal/agent"
	"github.com/apexion-ai/apexion/internal/mcp"
	"github.com/apexion-ai/apexion/internal/permission"
	"github.com/apexion-ai/apexion/internal/session"
	"github.com/apexion-ai/apexion/internal/tools"
	"github.com/apexion-ai/apexion/internal/tui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newRunCmd() *cobra.Command {
	var prompt string

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Execute a single prompt non-interactively",
		Example: `  apexion run -P "read main.go and tell me what it does"
  apexion run --prompt "list all Go files"
  echo "explain main.go" | apexion run --pipe
  apexion run -P "run tests and fix" --pipe --output-format jsonl`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Pipe mode: auto-detect from stdin or --pipe flag
			if pipeMode || !term.IsTerminal(int(os.Stdin.Fd())) {
				return runPipe(prompt)
			}
			if prompt == "" {
				return fmt.Errorf("--prompt / -P is required")
			}
			return runOnce(prompt)
		},
	}

	cmd.Flags().StringVarP(&prompt, "prompt", "P", "", "the prompt to execute")
	cmd.Flags().BoolVar(&pipeMode, "pipe", false, "pipe mode: read stdin, write stdout, auto-approve all tools")
	cmd.Flags().StringVar(&outputFormat, "output-format", "text", "output format: text or jsonl (pipe mode)")
	cmd.Flags().BoolVar(&printLast, "print-last", false, "only output the final LLM response (pipe mode)")

	return cmd
}

// runPipe executes in pipe mode: reads from stdin if no prompt, writes to stdout, auto-approves.
func runPipe(prompt string) error {
	// If no prompt flag, read from stdin.
	if prompt == "" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("failed to read stdin: %w", err)
		}
		prompt = strings.TrimSpace(string(data))
	}
	if prompt == "" {
		return fmt.Errorf("no prompt provided (use -P or pipe via stdin)")
	}

	cfg := initConfig()
	cfg.Permissions.Mode = "auto-approve" // auto-approve in pipe mode

	p, err := buildProvider(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if cfg.Model == "" {
		cfg.Model = p.DefaultModel()
	}

	registry := tools.DefaultRegistry(&tools.WebToolsConfig{
		SearchProvider: cfg.Web.SearchProvider,
		SearchAPIKey:   cfg.Web.SearchAPIKey,
	}, &tools.BashToolConfig{
		WorkDir:  cfg.Sandbox.WorkDir,
		AuditLog: cfg.Sandbox.AuditLog,
	})
	policy := permission.AllowAllPolicy{}
	executor := tools.NewExecutor(registry, policy)

	// Load hooks
	cwd, _ := os.Getwd()
	mcpCfg, _ := mcp.LoadMCPConfig(cwd)
	var mcpMgr *mcp.Manager
	if mcpCfg != nil && len(mcpCfg.MCPServers) > 0 {
		mcpMgr = mcp.NewManager(mcpCfg)
		defer mcpMgr.Close()
	}
	if hm := tools.LoadHooks(cwd); hm.HasHooks() {
		executor.SetHooks(hm)
	}

	// Linter
	if linter := tools.NewLinter(cfg.Lint); linter != nil {
		executor.SetLinter(linter)
	}

	// Test runner
	if tr := tools.NewTestRunner(cfg.Test); tr != nil {
		executor.SetTestRunner(tr)
	}

	store := session.NullStore{}
	ui := tui.NewPipeIO(outputFormat, true, printLast)

	a := agent.New(p, executor, cfg, ui, store)
	if mcpMgr != nil {
		a.SetMCPManager(mcpMgr)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	err = a.RunOnce(ctx, prompt)
	ui.Flush()
	return err
}

// runOnce executes a single prompt and exits.
func runOnce(prompt string) error {
	cfg := initConfig()

	p, err := buildProvider(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if cfg.Model == "" {
		cfg.Model = p.DefaultModel()
	}

	registry := tools.DefaultRegistry(&tools.WebToolsConfig{
		SearchProvider: cfg.Web.SearchProvider,
		SearchAPIKey:   cfg.Web.SearchAPIKey,
	}, &tools.BashToolConfig{
		WorkDir:  cfg.Sandbox.WorkDir,
		AuditLog: cfg.Sandbox.AuditLog,
	})
	policy := permission.NewDefaultPolicy(&cfg.Permissions)
	executor := tools.NewExecutor(registry, policy)

	// Load hooks from .apexion/hooks.yaml and ~/.config/apexion/hooks.yaml
	cwd, _ := os.Getwd()
	mcpCfg, _ := mcp.LoadMCPConfig(cwd)
	var mcpMgr *mcp.Manager
	if mcpCfg != nil && len(mcpCfg.MCPServers) > 0 {
		mcpMgr = mcp.NewManager(mcpCfg)
		defer mcpMgr.Close()
	}
	if hm := tools.LoadHooks(cwd); hm.HasHooks() {
		executor.SetHooks(hm)
	}

	// Linter
	if linter := tools.NewLinter(cfg.Lint); linter != nil {
		executor.SetLinter(linter)
	}

	// Test runner
	if tr := tools.NewTestRunner(cfg.Test); tr != nil {
		executor.SetTestRunner(tr)
	}

	dbPath, err := session.DefaultDBPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "session db path:", err)
		os.Exit(1)
	}
	store, err := session.NewSQLiteStore(dbPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open session store:", err)
		os.Exit(1)
	}
	defer store.Close()

	if useTUI {
		tuiCfg := tui.TUIConfig{
			Version:     displayVersion(),
			Provider:    cfg.Provider,
			Model:       cfg.Model,
			ShowWelcome: false, // run mode: no welcome page
		}

		return tui.RunTUI(tuiCfg, func(ui tui.IO, ctx context.Context) error {
			executor.SetConfirmer(ui)
			if tc, ok := ui.(tools.ToolCanceller); ok {
				executor.SetToolCanceller(tc)
			}
			a := agent.New(p, executor, cfg, ui, store)
			if mcpMgr != nil {
				a.SetMCPManager(mcpMgr)
			}
			return a.RunOnce(ctx, prompt)
		})
	}

	// Plain IO mode (default)
	ui := tui.NewPlainIO()
	executor.SetConfirmer(ui)

	a := agent.New(p, executor, cfg, ui, store)
	if mcpMgr != nil {
		a.SetMCPManager(mcpMgr)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	return a.RunOnce(ctx, prompt)
}
