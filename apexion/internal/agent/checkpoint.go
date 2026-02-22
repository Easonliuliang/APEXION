package agent

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Checkpoint represents a saved point-in-time snapshot of the working tree.
type Checkpoint struct {
	ID        string
	Label     string
	StashRef  string // git stash ref (e.g. "stash@{0}")
	Branch    string
	CreatedAt time.Time
}

// CheckpointManager creates and manages checkpoints using git stash.
type CheckpointManager struct {
	mu          sync.Mutex
	checkpoints []Checkpoint
	maxKeep     int
	counter     int
}

// NewCheckpointManager creates a new CheckpointManager.
func NewCheckpointManager(maxKeep int) *CheckpointManager {
	if maxKeep <= 0 {
		maxKeep = 10
	}
	return &CheckpointManager{maxKeep: maxKeep}
}

// Create creates a new checkpoint by using `git stash create` to create a
// stash commit without modifying the working tree, then storing the ref.
func (cm *CheckpointManager) Create(ctx context.Context, label string) (*Checkpoint, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Check if we're in a git repo.
	if err := gitExec(ctx, nil, "rev-parse", "--is-inside-work-tree"); err != nil {
		return nil, fmt.Errorf("not a git repository")
	}

	// Get current branch.
	var branchBuf bytes.Buffer
	if err := gitExec(ctx, &branchBuf, "rev-parse", "--abbrev-ref", "HEAD"); err != nil {
		return nil, fmt.Errorf("cannot determine branch: %w", err)
	}
	branch := strings.TrimSpace(branchBuf.String())

	// Use `git stash create` to create a stash commit without touching the working tree.
	// This creates a commit object that captures the current state.
	var refBuf bytes.Buffer
	if err := gitExec(ctx, &refBuf, "stash", "create"); err != nil {
		return nil, fmt.Errorf("git stash create failed: %w", err)
	}

	stashRef := strings.TrimSpace(refBuf.String())
	if stashRef == "" {
		// No changes to stash — create a checkpoint pointing to HEAD.
		var headBuf bytes.Buffer
		if err := gitExec(ctx, &headBuf, "rev-parse", "HEAD"); err != nil {
			return nil, fmt.Errorf("cannot get HEAD: %w", err)
		}
		stashRef = strings.TrimSpace(headBuf.String())
	} else {
		// Store the ref so it doesn't get garbage collected.
		msg := fmt.Sprintf("apexion-checkpoint: %s", label)
		_ = gitExec(ctx, nil, "stash", "store", "-m", msg, stashRef)
	}

	cm.counter++
	cp := Checkpoint{
		ID:        fmt.Sprintf("cp-%d", cm.counter),
		Label:     label,
		StashRef:  stashRef,
		Branch:    branch,
		CreatedAt: time.Now(),
	}

	cm.checkpoints = append(cm.checkpoints, cp)

	// Trim old checkpoints if over limit.
	if len(cm.checkpoints) > cm.maxKeep {
		cm.checkpoints = cm.checkpoints[len(cm.checkpoints)-cm.maxKeep:]
	}

	return &cp, nil
}

// Rollback restores the working tree to a checkpoint state.
// If id is empty, rolls back to the most recent checkpoint.
func (cm *CheckpointManager) Rollback(ctx context.Context, id string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if len(cm.checkpoints) == 0 {
		return fmt.Errorf("no checkpoints available")
	}

	var target *Checkpoint
	if id == "" {
		// Use most recent.
		target = &cm.checkpoints[len(cm.checkpoints)-1]
	} else {
		for i := range cm.checkpoints {
			if cm.checkpoints[i].ID == id {
				target = &cm.checkpoints[i]
				break
			}
		}
	}
	if target == nil {
		return fmt.Errorf("checkpoint %q not found", id)
	}

	// Clean the working tree and restore from the stash ref.
	// First, reset tracked files to HEAD.
	if err := gitExec(ctx, nil, "checkout", "."); err != nil {
		return fmt.Errorf("git checkout . failed: %w", err)
	}
	// Remove untracked files.
	_ = gitExec(ctx, nil, "clean", "-fd")

	// Apply the stash ref (may be a stash commit or a regular commit).
	// Use checkout-index approach for safety: try git stash apply first,
	// fall back to git checkout for regular commits.
	if err := gitExec(ctx, nil, "stash", "apply", target.StashRef); err != nil {
		// Fall back: the ref might be a regular commit (HEAD case).
		if err2 := gitExec(ctx, nil, "checkout", target.StashRef, "--", "."); err2 != nil {
			return fmt.Errorf("rollback failed: %w", err2)
		}
	}

	return nil
}

// List returns all checkpoints, most recent first.
func (cm *CheckpointManager) List() []Checkpoint {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	result := make([]Checkpoint, len(cm.checkpoints))
	// Return in reverse order (most recent first).
	for i, cp := range cm.checkpoints {
		result[len(cm.checkpoints)-1-i] = cp
	}
	return result
}

// gitExec runs a git command. If stdout is non-nil, captures output there.
func gitExec(ctx context.Context, stdout *bytes.Buffer, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	if stdout != nil {
		cmd.Stdout = stdout
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
