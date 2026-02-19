package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/aictl/aictl/internal/agent"
	"github.com/aictl/aictl/internal/tools"
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
	isAutoApprove := cfg.Permissions.Mode == "auto-approve"
	executor := tools.NewExecutor(registry, isAutoApprove, cfg.Permissions.AutoApproveTools)

	a := agent.New(p, executor, cfg)

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
