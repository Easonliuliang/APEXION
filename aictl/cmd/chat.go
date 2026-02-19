package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/aictl/aictl/internal/agent"
	"github.com/aictl/aictl/internal/tools"
)

// runChat starts the interactive chat (REPL) mode.
func runChat() error {
	cfg := initConfig()

	p, err := buildProvider(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Use provider default model if none specified
	if cfg.Model == "" {
		cfg.Model = p.DefaultModel()
	}

	registry := tools.DefaultRegistry()
	isAutoApprove := cfg.Permissions.Mode == "auto-approve"
	executor := tools.NewExecutor(registry, isAutoApprove, cfg.Permissions.AutoApproveTools)

	a := agent.New(p, executor, cfg)

	// Graceful shutdown on SIGINT/SIGTERM
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
