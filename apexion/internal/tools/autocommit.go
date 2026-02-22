package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// AutoCommitter handles automatic git commits after file edits.
// When enabled, it commits each file modification with a descriptive message.
type AutoCommitter struct {
	enabled bool
	mu      sync.Mutex
}

// NewAutoCommitter creates a new AutoCommitter.
func NewAutoCommitter(enabled bool) *AutoCommitter {
	return &AutoCommitter{enabled: enabled}
}

// SetEnabled toggles auto-commit on or off.
func (ac *AutoCommitter) SetEnabled(enabled bool) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.enabled = enabled
}

// Enabled returns true if auto-commit is active.
func (ac *AutoCommitter) Enabled() bool {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.enabled
}

// TryCommit attempts to commit a file change after a successful tool operation.
// It runs silently in the background; errors are ignored to avoid disrupting the user.
// The commit uses --no-verify to skip pre-commit hooks (avoids loops).
func (ac *AutoCommitter) TryCommit(ctx context.Context, filePath, toolName string) {
	ac.mu.Lock()
	if !ac.enabled {
		ac.mu.Unlock()
		return
	}
	ac.mu.Unlock()

	// Use a short timeout to avoid blocking the agent loop.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Check if we're in a git repository.
	if err := runGitCmd(ctx, "rev-parse", "--is-inside-work-tree"); err != nil {
		return // not a git repo, skip silently
	}

	// Stage the specific file.
	if err := runGitCmd(ctx, "add", "--", filePath); err != nil {
		return
	}

	// Check if there are staged changes (file might not have actually changed).
	if err := runGitCmd(ctx, "diff", "--cached", "--quiet"); err == nil {
		return // no staged changes
	}

	// Commit with a descriptive message.
	filename := filepath.Base(filePath)
	msg := fmt.Sprintf("apexion: %s %s", toolName, filename)
	_ = runGitCmd(ctx, "commit", "-m", msg, "--no-verify")
}

// runGitCmd runs a git command and returns any error.
func runGitCmd(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	return cmd.Run()
}
