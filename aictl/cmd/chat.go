package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/aictl/aictl/internal/agent"
	"github.com/aictl/aictl/internal/config"
	"github.com/aictl/aictl/internal/permission"
	"github.com/aictl/aictl/internal/provider"
	"github.com/aictl/aictl/internal/session"
	"github.com/aictl/aictl/internal/tools"
	"github.com/aictl/aictl/internal/tui"
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
	})
	policy := permission.NewDefaultPolicy(&cfg.Permissions)
	executor := tools.NewExecutor(registry, policy)

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
			Version:     appVersion,
			Provider:    cfg.Provider,
			Model:       cfg.Model,
			SessionID:   sessionID,
			ShowWelcome: true,
		}

		return tui.RunTUI(tuiCfg, func(ui tui.IO) error {
			executor.SetConfirmer(ui)
			if tc, ok := ui.(tools.ToolCanceller); ok {
				executor.SetToolCanceller(tc)
			}
			a := agent.NewWithSession(p, executor, cfg, ui, store, sess)
			a.SetProviderFactory(factory)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				cancel()
			}()

			return a.Run(ctx)
		})
	}

	// Plain IO mode (default)
	ui := tui.NewPlainIO()
	executor.SetConfirmer(ui)

	a := agent.New(p, executor, cfg, ui, store)
	a.SetProviderFactory(factory)

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
