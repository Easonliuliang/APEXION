package cmd

import (
	"fmt"
	"os"

	"github.com/apexion-ai/apexion/internal/config"
	"github.com/apexion-ai/apexion/internal/provider"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	cfgFile      string
	autoApprove  bool
	modelFlag    string
	providerFlag string
	maxTurnsFlag int
	useTUI       bool
	pipeMode     bool
	outputFormat string
	printLast    bool

	// Package-level version info, set by Execute().
	appVersion string
	appCommit  string
	appDate    string
)

// Execute is the main entry point called from main.go.
func Execute(version, commit, date string) {
	appVersion = version
	appCommit = commit
	appDate = date

	rootCmd := &cobra.Command{
		Use:   "apexion",
		Short: "AI-powered coding assistant",
		Long:  "apexion is an interactive AI coding agent with tool execution capabilities.",
		// Running apexion with no subcommand starts chat mode.
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Default TUI on when stdout is a terminal and --tui was not explicitly set.
			if !cmd.Root().PersistentFlags().Changed("tui") && term.IsTerminal(int(os.Stdout.Fd())) {
				useTUI = true
			}
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChat()
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// Global flags
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file path (default ~/.config/apexion/config.yaml)")
	rootCmd.PersistentFlags().BoolVar(&autoApprove, "auto-approve", false, "skip all tool execution confirmations")
	rootCmd.PersistentFlags().StringVarP(&modelFlag, "model", "m", "", "override model")
	rootCmd.PersistentFlags().StringVarP(&providerFlag, "provider", "p", "", "override provider")
	rootCmd.PersistentFlags().IntVar(&maxTurnsFlag, "max-turns", 0, "max agent loop iterations (0=unlimited)")
	rootCmd.PersistentFlags().BoolVar(&useTUI, "tui", false, "use bubbletea TUI mode (default: auto-detect terminal)")

	// Subcommands
	rootCmd.AddCommand(newRunCmd())
	rootCmd.AddCommand(newVersionCmd(version, commit, date))
	rootCmd.AddCommand(newInitCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// displayVersion returns a formatted version string for the TUI welcome page,
// e.g. "v0.3.1 (abc1234)".
func displayVersion() string {
	v := "v" + appVersion
	if appCommit != "" && appCommit != "none" {
		v += " (" + appCommit + ")"
	}
	return v
}

// initConfig loads configuration, applying CLI flag overrides.
func initConfig() *config.Config {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// CLI flags override config values
	if providerFlag != "" {
		cfg.Provider = providerFlag
	}
	if modelFlag != "" {
		cfg.Model = modelFlag
	}
	if autoApprove {
		cfg.Permissions.Mode = "auto-approve"
	}
	if maxTurnsFlag > 0 {
		cfg.MaxIterations = maxTurnsFlag
	}

	return cfg
}

// providerBaseURLs references the canonical map in the config package.
var providerBaseURLs = config.KnownProviderBaseURLs

// buildProvider creates a Provider instance based on configuration.
func buildProvider(cfg *config.Config) (provider.Provider, error) {
	name := cfg.Provider
	pc := cfg.GetProviderConfig(name)

	apiKey := pc.APIKey
	if apiKey == "" {
		return nil, fmt.Errorf(
			"API key not configured for provider %q.\n"+
				"Set it via:\n"+
				"  - config file: providers.%s.api_key\n"+
				"  - environment: LLM_API_KEY\n"+
				"  - run: apexion init",
			name, name,
		)
	}

	// Determine model: CLI flag > config file > provider defaults YAML
	model := cfg.Model
	if pc.Model != "" && model == "" {
		model = pc.Model
	}
	if model == "" {
		if m, ok := config.KnownProviderModels[name]; ok {
			model = m
		}
	}

	switch name {
	case "anthropic":
		p := provider.NewAnthropicProvider(apiKey, model)
		return p, nil
	default:
		// All other providers use OpenAI-compatible API
		baseURL := pc.BaseURL
		if baseURL == "" {
			if u, ok := providerBaseURLs[name]; ok {
				baseURL = u
			} else {
				return nil, fmt.Errorf("unknown provider %q; set providers.%s.base_url in config", name, name)
			}
		}
		p := provider.NewOpenAIProvider(apiKey, baseURL, model)
		return p, nil
	}
}
