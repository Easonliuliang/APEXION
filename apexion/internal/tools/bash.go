package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
)

// BashTool 执行 shell 命令
type BashTool struct{}

func (t *BashTool) Name() string                     { return "bash" }
func (t *BashTool) IsReadOnly() bool                 { return false }
func (t *BashTool) PermissionLevel() PermissionLevel { return PermissionExecute }

func (t *BashTool) Description() string {
	return "Execute a shell command and return its combined stdout and stderr output. " +
		"For long-running processes (dev servers, watchers, etc.) that never exit, " +
		"set run_in_background=true to capture initial output and let the process continue."
}

const (
	defaultBashTimeout = 120 * time.Second
	maxBashTimeout     = 600 * time.Second
	bgWarmupTimeout    = 10 * time.Second // background mode: return after this if still running
)

func (t *BashTool) Parameters() map[string]any {
	return map[string]any{
		"command": map[string]any{
			"type":        "string",
			"description": "The shell command to execute",
		},
		"timeout": map[string]any{
			"type":        "integer",
			"description": "Timeout in seconds (default 120, max 600). Only applies when run_in_background=false.",
		},
		"run_in_background": map[string]any{
			"type":        "boolean",
			"description": "Run command in background. Use for dev servers, build watchers, or any process that does not exit on its own. Returns initial output after 10 seconds.",
		},
	}
}

func (t *BashTool) Execute(ctx context.Context, params json.RawMessage) (ToolResult, error) {
	var p struct {
		Command         string `json:"command"`
		Timeout         int    `json:"timeout"`
		RunInBackground bool   `json:"run_in_background"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("invalid params: %w", err)
	}
	if p.Command == "" {
		return ToolResult{}, fmt.Errorf("command is required")
	}

	if p.RunInBackground {
		return t.runBackground(ctx, p.Command)
	}

	// Normal mode: blocking with timeout
	timeout := defaultBashTimeout
	if p.Timeout > 0 {
		timeout = time.Duration(p.Timeout) * time.Second
	}
	if timeout > maxBashTimeout {
		timeout = maxBashTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, shellBin(), "-c", p.Command)
	out, err := cmd.CombinedOutput()
	result := string(out)

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			secs := int(timeout.Seconds())
			return ToolResult{
				Content: fmt.Sprintf("Command timed out after %dm%ds\nOutput:\n%s", secs/60, secs%60, result),
				IsError: true,
			}, nil
		}
		if ctx.Err() == context.Canceled {
			return ToolResult{}, fmt.Errorf("cancelled")
		}
		return ToolResult{
			Content: fmt.Sprintf("Exit error: %v\nOutput:\n%s", err, result),
			IsError: true,
		}, nil
	}

	return ToolResult{Content: result}, nil
}

// runBackground starts the command independently, collects output for bgWarmupTimeout,
// then returns partial output if the process is still running.
// The process continues after this function returns.
func (t *BashTool) runBackground(ctx context.Context, command string) (ToolResult, error) {
	// exec.Command (not CommandContext) so the process outlives the tool call
	cmd := exec.Command(shellBin(), "-c", command)

	var buf safeBuffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Start(); err != nil {
		return ToolResult{
			Content: fmt.Sprintf("Failed to start: %v", err),
			IsError: true,
		}, nil
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	warmup := time.NewTimer(bgWarmupTimeout)
	defer warmup.Stop()

	select {
	case err := <-done:
		// Exited before warmup — return full output
		result := buf.String()
		if err != nil {
			return ToolResult{
				Content: fmt.Sprintf("Exit error: %v\nOutput:\n%s", err, result),
				IsError: true,
			}, nil
		}
		return ToolResult{Content: result}, nil

	case <-warmup.C:
		// Still running — return partial output, let process continue
		partial := buf.String()
		note := fmt.Sprintf(
			"\n\n(Process still running in background, PID: %d. Output above captured during first %ds.)",
			cmd.Process.Pid, int(bgWarmupTimeout.Seconds()))
		return ToolResult{Content: partial + note}, nil

	case <-ctx.Done():
		cmd.Process.Kill()
		return ToolResult{}, fmt.Errorf("cancelled")
	}
}

// safeBuffer is a bytes.Buffer safe for concurrent reads and writes.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// shellBin returns the user's preferred shell, falling back to bash then sh.
func shellBin() string {
	if s := os.Getenv("SHELL"); s != "" {
		if _, err := os.Stat(s); err == nil {
			return s
		}
	}
	if p, err := exec.LookPath("bash"); err == nil {
		return p
	}
	return "sh"
}
