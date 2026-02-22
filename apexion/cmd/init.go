package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Interactive configuration wizard",
		Long:  "Guides you through setting up apexion: choose a provider, enter your API key, and save the config.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit()
		},
	}
}

func runInit() error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Welcome to apexion configuration wizard!")
	fmt.Println()

	// Provider selection
	providers := []string{
		"openai", "anthropic", "deepseek", "minimax",
		"kimi", "qwen", "glm", "doubao", "groq",
	}
	fmt.Println("Available providers:")
	for i, p := range providers {
		fmt.Printf("  %d. %s\n", i+1, p)
	}
	fmt.Print("\nSelect provider (1-9) [1]: ")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	selectedIdx := 0
	if input != "" {
		n := 0
		for _, c := range input {
			if c >= '0' && c <= '9' {
				n = n*10 + int(c-'0')
			}
		}
		if n >= 1 && n <= len(providers) {
			selectedIdx = n - 1
		}
	}
	providerName := providers[selectedIdx]
	fmt.Printf("Selected: %s\n\n", providerName)

	// API key
	fmt.Printf("Enter API key for %s: ", providerName)
	apiKey, _ := reader.ReadString('\n')
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return fmt.Errorf("API key cannot be empty")
	}

	// Build config YAML
	configData := map[string]any{
		"provider": providerName,
		"providers": map[string]any{
			providerName: map[string]any{
				"api_key": apiKey,
			},
		},
		"permissions": map[string]any{
			"mode":               "interactive",
			"auto_approve_tools": []string{"read_file", "glob", "grep", "list_dir"},
		},
	}

	data, err := yaml.Marshal(configData)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	// Save
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}
	configDir := filepath.Join(home, ".config", "apexion")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	configPath := filepath.Join(configDir, "config.yaml")

	// Check if config already exists
	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("\nConfig file already exists at %s\n", configPath)
		fmt.Print("Overwrite? [y/N]: ")
		answer, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(answer)) != "y" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Printf("\nConfig saved to %s\n", configPath)
	fmt.Println("You can now run: apexion")
	return nil
}
