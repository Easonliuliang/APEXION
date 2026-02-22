package tools

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/apexion-ai/apexion/internal/config"
)

// Linter runs lint commands after file modifications.
// It selects the appropriate lint command based on file extension.
type Linter struct {
	config config.LintConfig
}

// NewLinter creates a new Linter from configuration.
// Returns nil if linting is disabled or no commands are configured.
func NewLinter(cfg config.LintConfig) *Linter {
	if !cfg.Enabled || len(cfg.Commands) == 0 {
		return nil
	}
	if cfg.MaxFixes <= 0 {
		cfg.MaxFixes = 3
	}
	return &Linter{config: cfg}
}

// Run executes the lint command for the given file based on its extension.
// Returns the lint output, whether there were errors, and any execution error.
func (l *Linter) Run(ctx context.Context, filePath string) (output string, hasErrors bool, err error) {
	ext := filepath.Ext(filePath)
	if ext == "" {
		return "", false, nil
	}

	cmdTemplate, ok := l.config.Commands[ext]
	if !ok {
		return "", false, nil // no lint command for this extension
	}

	// Replace {{.file}} placeholder with the actual file path.
	cmdStr := strings.ReplaceAll(cmdTemplate, "{{.file}}", filePath)

	// Use a timeout to avoid hanging on lint commands.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()

	// Combine stdout and stderr for the output.
	combined := strings.TrimSpace(stdout.String() + "\n" + stderr.String())
	combined = strings.TrimSpace(combined)

	if runErr != nil {
		// Non-zero exit code means lint found issues.
		if combined == "" {
			combined = runErr.Error()
		}
		// Cap lint output to avoid blowing up the tool result.
		if len(combined) > 4096 {
			combined = combined[:4096] + "\n[lint output truncated]"
		}
		return combined, true, nil
	}

	return "", false, nil
}
