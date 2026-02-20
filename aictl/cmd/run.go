package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/aictl/aictl/internal/agent"
	"github.com/aictl/aictl/internal/permission"
	"github.com/aictl/aictl/internal/session"
	"github.com/aictl/aictl/internal/tools"
	"github.com/aictl/aictl/internal/tui"
	"github.com/spf13/cobra"
)

func newRunCmd() *cobra.Command {
	var prompt string

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Execute a single prompt non-interactively",
		Example: `  aictl run -P "read main.go and tell me what it does"
  aictl run --prompt "list all Go files"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if prompt == "" {
				return fmt.Errorf("--prompt / -P is required")
			}
			return runOnce(prompt)
		},
	}

	cmd.Flags().StringVarP(&prompt, "prompt", "P", "", "the prompt to execute")
	cmd.MarkFlagRequired("prompt")

	return cmd
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

	registry := tools.DefaultRegistry()
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

	if useTUI {
		tuiCfg := tui.TUIConfig{
			Version:     appVersion,
			Provider:    cfg.Provider,
			Model:       cfg.Model,
			ShowWelcome: false, // run mode: no welcome page
		}

		return tui.RunTUI(tuiCfg, func(ui tui.IO) error {
			executor.SetConfirmer(ui)
			a := agent.New(p, executor, cfg, ui, store)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				cancel()
			}()

			return a.RunOnce(ctx, prompt)
		})
	}

	// Plain IO mode (default)
	ui := tui.NewPlainIO()
	executor.SetConfirmer(ui)

	a := agent.New(p, executor, cfg, ui, store)

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
