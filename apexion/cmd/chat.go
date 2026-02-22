package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/apexion-ai/apexion/internal/agent"
	"github.com/apexion-ai/apexion/internal/config"
	"github.com/apexion-ai/apexion/internal/mcp"
	"github.com/apexion-ai/apexion/internal/permission"
	"github.com/apexion-ai/apexion/internal/provider"
	"github.com/apexion-ai/apexion/internal/session"
	"github.com/apexion-ai/apexion/internal/tools"
	"github.com/apexion-ai/apexion/internal/tui"
)

// runChat starts the interactive chat (REPL) mode.
func runChat() error {
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

	// MCP: load config, connect all servers, register tools
	mcpCfg, _ := mcp.LoadMCPConfig(cwd)
	var mcpMgr *mcp.Manager
	if mcpCfg != nil && len(mcpCfg.MCPServers) > 0 {
		mcpMgr = mcp.NewManager(mcpCfg)
		defer mcpMgr.Close()
		initCtx, initCancel := context.WithTimeout(context.Background(), 30*time.Second)
		errs := mcpMgr.ConnectAll(initCtx)
		initCancel()
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "[mcp] warning: %v\n", e)
		}
		n := mcp.RegisterTools(mcpMgr, registry)
		if n > 0 {
			fmt.Fprintf(os.Stderr, "[mcp] registered %d tool(s)\n", n)
		}
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

	memStore, err := session.NewSQLiteMemoryStore(store.DB())
	if err != nil {
		fmt.Fprintln(os.Stderr, "open memory store:", err)
		os.Exit(1)
	}

	// Provider factory for /provider hot-swap.
	factory := agent.ProviderFactory(func(c *config.Config) (provider.Provider, error) {
		return buildProvider(c)
	})

	if useTUI {
		// Build a temporary session to get the ID for the welcome page.
		sess := session.New()
		sessionID := sess.ID
		if len(sessionID) > 8 {
			sessionID = sessionID[:8]
		}

		tuiCfg := tui.TUIConfig{
			Version:        appVersion,
			Provider:       cfg.Provider,
			Model:          cfg.Model,
			SessionID:      sessionID,
			ShowWelcome:    true,
			CustomCommands: agent.CustomCommandItems(cwd),
		}

		// ctx is managed by RunTUI: cancelled on Ctrl+C, TUI exit, or OS signal.
		return tui.RunTUI(tuiCfg, func(ui tui.IO, ctx context.Context) error {
			executor.SetConfirmer(ui)
			if tc, ok := ui.(tools.ToolCanceller); ok {
				executor.SetToolCanceller(tc)
			}
			a := agent.NewWithSession(p, executor, cfg, ui, store, sess)
			a.SetProviderFactory(factory)
			a.SetMemoryStore(memStore)
			if mcpMgr != nil {
				a.SetMCPManager(mcpMgr)
			}
			return a.Run(ctx)
		})
	}

	// Plain IO mode (default)
	ui := tui.NewPlainIO()
	executor.SetConfirmer(ui)

	a := agent.New(p, executor, cfg, ui, store)
	a.SetProviderFactory(factory)
	a.SetMemoryStore(memStore)
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

	return a.Run(ctx)
}
