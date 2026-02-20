package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/aictl/aictl/internal/agent"
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

	registry := tools.DefaultRegistry()
	isAutoApprove := cfg.Permissions.Mode == "auto-approve"
	executor := tools.NewExecutor(registry, isAutoApprove, cfg.Permissions.AutoApproveTools)

	if useTUI {
		return tui.RunTUI(func(ui tui.IO) error {
			executor.SetConfirmer(ui)
			a := agent.New(p, executor, cfg, ui)

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

	a := agent.New(p, executor, cfg, ui)

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
