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

// TestRunner runs test commands after file modifications.
// It selects the appropriate test command based on file extension.
type TestRunner struct {
	config config.TestConfig
}

// NewTestRunner creates a new TestRunner from configuration.
// Returns nil if testing is disabled or no commands are configured.
func NewTestRunner(cfg config.TestConfig) *TestRunner {
	if !cfg.Enabled || len(cfg.Commands) == 0 {
		return nil
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	return &TestRunner{config: cfg}
}

// Run executes the test command for the given file based on its extension.
// Returns the test output, whether tests passed, and any execution error.
func (tr *TestRunner) Run(ctx context.Context, filePath string) (output string, passed bool, err error) {
	ext := filepath.Ext(filePath)
	if ext == "" {
		return "", true, nil
	}

	cmdTemplate, ok := tr.config.Commands[ext]
	if !ok {
		return "", true, nil // no test command for this extension
	}

	// Replace {{.file}} placeholder with the actual file path.
	cmdStr := strings.ReplaceAll(cmdTemplate, "{{.file}}", filePath)

	// Use a timeout to avoid hanging on test commands.
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
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
		// Non-zero exit code means tests failed.
		if combined == "" {
			combined = runErr.Error()
		}
		// Cap test output to avoid blowing up the tool result.
		if len(combined) > 8192 {
			combined = combined[:8192] + "\n[test output truncated]"
		}
		return combined, false, nil
	}

	return "", true, nil
}

// MaxRetries returns the configured maximum auto-fix attempts.
func (tr *TestRunner) MaxRetries() int {
	return tr.config.MaxRetries
}
