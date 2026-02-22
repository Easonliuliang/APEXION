package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// gitBin returns the path to the git executable.
// Searches PATH first, then falls back to well-known locations.
func gitBin() string {
	if p, err := exec.LookPath("git"); err == nil {
		return p
	}
	for _, candidate := range []string{
		"/usr/bin/git",
		"/usr/local/bin/git",
		"/opt/homebrew/bin/git",
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "git" // last resort — will fail with a clear error
}

// runGit executes a git command in the given directory and returns combined output.
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, gitBin(), args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// ── git_status ────────────────────────────────────────────────────────────────

// GitStatusTool runs `git status` in the working directory.
type GitStatusTool struct{}

func (t *GitStatusTool) Name() string             { return "git_status" }
func (t *GitStatusTool) IsReadOnly() bool         { return true }
func (t *GitStatusTool) PermissionLevel() PermissionLevel { return PermissionRead }

func (t *GitStatusTool) Description() string {
	return "Show the working tree status of the git repository. " +
		"Returns staged, unstaged, and untracked file information."
}

func (t *GitStatusTool) Parameters() map[string]any {
	return map[string]any{
		"path": map[string]any{
			"type":        "string",
			"description": "Directory to run git status in (default: current directory)",
		},
	}
}

func (t *GitStatusTool) Execute(ctx context.Context, params json.RawMessage) (ToolResult, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("invalid params: %w", err)
	}

	out, err := runGit(ctx, p.Path, "status")
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("git status error: %v\n%s", err, out), IsError: true}, nil
	}
	return ToolResult{Content: out}, nil
}

// ── git_diff ──────────────────────────────────────────────────────────────────

// GitDiffTool runs `git diff` with optional ref and path filter.
type GitDiffTool struct{}

func (t *GitDiffTool) Name() string             { return "git_diff" }
func (t *GitDiffTool) IsReadOnly() bool         { return true }
func (t *GitDiffTool) PermissionLevel() PermissionLevel { return PermissionRead }

func (t *GitDiffTool) Description() string {
	return "Show git diff output. By default shows unstaged changes. " +
		"Use ref to compare against a commit/branch. Use --staged to show staged changes."
}

func (t *GitDiffTool) Parameters() map[string]any {
	return map[string]any{
		"ref": map[string]any{
			"type":        "string",
			"description": "Git ref to diff against (e.g. HEAD, main, commit hash). Omit for working tree diff.",
		},
		"staged": map[string]any{
			"type":        "boolean",
			"description": "If true, show staged (cached) changes instead of unstaged.",
		},
		"path": map[string]any{
			"type":        "string",
			"description": "Limit diff to this file or directory path.",
		},
		"dir": map[string]any{
			"type":        "string",
			"description": "Directory to run git diff in (default: current directory).",
		},
	}
}

func (t *GitDiffTool) Execute(ctx context.Context, params json.RawMessage) (ToolResult, error) {
	var p struct {
		Ref    string `json:"ref"`
		Staged bool   `json:"staged"`
		Path   string `json:"path"`
		Dir    string `json:"dir"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("invalid params: %w", err)
	}

	args := []string{"diff"}
	if p.Staged {
		args = append(args, "--staged")
	}
	if p.Ref != "" {
		args = append(args, p.Ref)
	}
	if p.Path != "" {
		args = append(args, "--", p.Path)
	}

	out, err := runGit(ctx, p.Dir, args...)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("git diff error: %v\n%s", err, out), IsError: true}, nil
	}

	if out == "" {
		return ToolResult{Content: "(no differences)"}, nil
	}

	// Truncate very large diffs
	const maxLines = 200
	lines := strings.Split(out, "\n")
	truncated := false
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		truncated = true
	}
	result := strings.Join(lines, "\n")
	if truncated {
		result += fmt.Sprintf("\n[Truncated: diff exceeds %d lines. Use a path filter to narrow the diff.]", maxLines)
	}

	return ToolResult{Content: result, Truncated: truncated}, nil
}

// ── git_commit ────────────────────────────────────────────────────────────────

// GitCommitTool stages specified files (or all changes) and creates a commit.
type GitCommitTool struct{}

func (t *GitCommitTool) Name() string             { return "git_commit" }
func (t *GitCommitTool) IsReadOnly() bool         { return false }
func (t *GitCommitTool) PermissionLevel() PermissionLevel { return PermissionWrite }

func (t *GitCommitTool) Description() string {
	return "Stage files and create a git commit. " +
		"Specify files to stage selectively, or leave empty to stage all changes (git add -A). " +
		"The commit message is required."
}

func (t *GitCommitTool) Parameters() map[string]any {
	return map[string]any{
		"message": map[string]any{
			"type":        "string",
			"description": "The commit message (required).",
		},
		"files": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "List of file paths to stage. If omitted, stages all changes (git add -A).",
		},
		"dir": map[string]any{
			"type":        "string",
			"description": "Directory to run git commit in (default: current directory).",
		},
	}
}

func (t *GitCommitTool) Execute(ctx context.Context, params json.RawMessage) (ToolResult, error) {
	var p struct {
		Message string   `json:"message"`
		Files   []string `json:"files"`
		Dir     string   `json:"dir"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("invalid params: %w", err)
	}
	if p.Message == "" {
		return ToolResult{Content: "commit message is required", IsError: true}, nil
	}

	// Stage files
	var addArgs []string
	if len(p.Files) > 0 {
		addArgs = append([]string{"add", "--"}, p.Files...)
	} else {
		addArgs = []string{"add", "-A"}
	}

	if out, err := runGit(ctx, p.Dir, addArgs...); err != nil {
		return ToolResult{Content: fmt.Sprintf("git add error: %v\n%s", err, out), IsError: true}, nil
	}

	// Commit
	out, err := runGit(ctx, p.Dir, "commit", "-m", p.Message)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("git commit error: %v\n%s", err, out), IsError: true}, nil
	}

	return ToolResult{Content: out}, nil
}

// ── git_push ──────────────────────────────────────────────────────────────────

// GitPushTool pushes commits to a remote.
type GitPushTool struct{}

func (t *GitPushTool) Name() string             { return "git_push" }
func (t *GitPushTool) IsReadOnly() bool         { return false }
func (t *GitPushTool) PermissionLevel() PermissionLevel { return PermissionDangerous }

func (t *GitPushTool) Description() string {
	return "Push commits to a remote git repository. " +
		"Defaults to 'origin' remote and the current branch. " +
		"WARNING: This operation affects the remote repository."
}

func (t *GitPushTool) Parameters() map[string]any {
	return map[string]any{
		"remote": map[string]any{
			"type":        "string",
			"description": "Remote name (default: origin).",
		},
		"branch": map[string]any{
			"type":        "string",
			"description": "Branch to push (default: current branch).",
		},
		"set_upstream": map[string]any{
			"type":        "boolean",
			"description": "If true, sets the upstream tracking reference (-u flag).",
		},
		"dir": map[string]any{
			"type":        "string",
			"description": "Directory to run git push in (default: current directory).",
		},
	}
}

func (t *GitPushTool) Execute(ctx context.Context, params json.RawMessage) (ToolResult, error) {
	var p struct {
		Remote      string `json:"remote"`
		Branch      string `json:"branch"`
		SetUpstream bool   `json:"set_upstream"`
		Dir         string `json:"dir"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("invalid params: %w", err)
	}

	remote := p.Remote
	if remote == "" {
		remote = "origin"
	}

	args := []string{"push"}
	if p.SetUpstream {
		args = append(args, "-u")
	}
	args = append(args, remote)
	if p.Branch != "" {
		args = append(args, p.Branch)
	}

	out, err := runGit(ctx, p.Dir, args...)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("git push error: %v\n%s", err, out), IsError: true}, nil
	}

	return ToolResult{Content: out}, nil
}
