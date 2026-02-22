package cmd

import (
	"fmt"
	"os"

	"github.com/aictl/aictl/internal/config"
	"github.com/aictl/aictl/internal/provider"
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
		Use:   "aictl",
		Short: "AI-powered coding assistant",
		Long:  "aictl is an interactive AI coding agent with tool execution capabilities.",
		// Running aictl with no subcommand starts chat mode.
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
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file path (default ~/.config/aictl/config.yaml)")
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

// providerBaseURLs maps OpenAI-compatible provider names to their base URLs.
var providerBaseURLs = map[string]string{
	"openai":   "https://api.openai.com/v1",
	"deepseek": "https://api.deepseek.com",
	"minimax":  "https://api.minimax.chat/v1",
	"kimi":     "https://api.moonshot.cn/v1",
	"qwen":     "https://dashscope.aliyuncs.com/compatible-mode/v1",
	"glm":      "https://open.bigmodel.cn/api/paas/v4/",
	"doubao":   "https://ark.cn-beijing.volces.com/api/v3",
	"groq":     "https://api.groq.com/openai/v1",
}

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
				"  - run: aictl init",
			name, name,
		)
	}

	// Determine model: CLI flag > config file > provider default (set later)
	model := cfg.Model
	if pc.Model != "" && model == "" {
		model = pc.Model
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
