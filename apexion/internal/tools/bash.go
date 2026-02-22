package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// BashTool executes shell commands.
type BashTool struct{}

func (t *BashTool) Name() string                     { return "bash" }
func (t *BashTool) IsReadOnly() bool                 { return false }
func (t *BashTool) PermissionLevel() PermissionLevel { return PermissionExecute }

func (t *BashTool) Description() string {
	return "Execute a shell command and return its combined stdout and stderr output. " +
		"stdin is disconnected (/dev/null) — interactive commands (input(), read, etc.) will fail. " +
		"Do NOT pipe input to simulate interactivity (e.g. echo '1' | python script.py). " +
		"For long-running processes (dev servers, watchers, etc.) that never exit, " +
		"set run_in_background=true to capture initial output and let the process continue."
}

const (
	defaultBashTimeout = 120 * time.Second
	maxBashTimeout     = 600 * time.Second
	idleTimeout        = 30 * time.Second  // kill if no new output for this long
	bgWarmupTimeout    = 10 * time.Second  // background mode: return after this if still running
)

func (t *BashTool) Parameters() map[string]any {
	return map[string]any{
		"command": map[string]any{
			"type":        "string",
			"description": "The shell command to execute. stdin is /dev/null — do not use pipe to feed interactive input.",
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

	// Normal mode: blocking with timeout + idle detection
	timeout := defaultBashTimeout
	if p.Timeout > 0 {
		timeout = time.Duration(p.Timeout) * time.Second
	}
	if timeout > maxBashTimeout {
		timeout = maxBashTimeout
	}

	return t.runForeground(ctx, p.Command, timeout)
}

// runForeground executes a command with both a hard timeout and an idle-output
// timeout. If no new stdout/stderr is produced for idleTimeout, the process is
// killed early instead of waiting for the full hard timeout.
func (t *BashTool) runForeground(ctx context.Context, command string, timeout time.Duration) (ToolResult, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, shellBin(), "-c", command)
	// Explicitly close stdin so interactive commands fail fast with EOF.
	cmd.Stdin = nil
	// Create a new process group so we can kill the entire tree.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var buf safeBuffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Start(); err != nil {
		return ToolResult{
			Content: fmt.Sprintf("Failed to start: %v", err),
			IsError: true,
		}, nil
	}

	// Track last output time for idle detection.
	var lastOutputTime atomic.Int64
	lastOutputTime.Store(time.Now().UnixMilli())
	buf.onWrite = func() {
		lastOutputTime.Store(time.Now().UnixMilli())
	}

	// Wait for the process in a goroutine.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	// Idle detection loop: check every second.
	idleTicker := time.NewTicker(1 * time.Second)
	defer idleTicker.Stop()

	idledOut := false
	for {
		select {
		case err := <-done:
			// Process exited normally or with error.
			result := buf.String()
			if err != nil {
				if ctx.Err() == context.DeadlineExceeded {
					secs := int(timeout.Seconds())
					return ToolResult{
						Content: fmt.Sprintf("Command timed out after %dm%ds.\nOutput:\n%s",
							secs/60, secs%60, result),
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

		case <-idleTicker.C:
			last := time.UnixMilli(lastOutputTime.Load())
			if time.Since(last) >= idleTimeout {
				// No output for too long — kill the process group.
				idledOut = true
				killProcessGroup(cmd)
				// Let the done channel fire on the next iteration.
			}

		case <-ctx.Done():
			// Hard timeout or user cancel already handled via CommandContext,
			// but ensure the process group is also killed.
			killProcessGroup(cmd)
			// Let the done channel fire on the next iteration.
		}

		if idledOut {
			// Wait briefly for process to exit after kill.
			select {
			case <-done:
			case <-time.After(2 * time.Second):
			}
			result := buf.String()
			secs := int(idleTimeout.Seconds())
			return ToolResult{
				Content: fmt.Sprintf(
					"Command killed: no output for %ds (idle timeout). "+
						"The command may be waiting for input or sleeping. "+
						"Do NOT retry with piped stdin — interactive commands are not supported.\n"+
						"Output:\n%s", secs, result),
				IsError: true,
			}, nil
		}
	}
}

// killProcessGroup sends SIGTERM to the process group, waits briefly, then
// sends SIGKILL if the process is still alive (mirrors OpenCode TS approach).
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	// Negative PID sends signal to the entire process group.
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	time.Sleep(200 * time.Millisecond)
	// SIGKILL as fallback — ignore errors (process may have already exited).
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}

// runBackground starts the command independently, collects output for bgWarmupTimeout,
// then returns partial output if the process is still running.
// The process continues after this function returns.
func (t *BashTool) runBackground(ctx context.Context, command string) (ToolResult, error) {
	// exec.Command (not CommandContext) so the process outlives the tool call.
	cmd := exec.Command(shellBin(), "-c", command)
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

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
		killProcessGroup(cmd)
		return ToolResult{}, fmt.Errorf("cancelled")
	}
}

// safeBuffer is a bytes.Buffer safe for concurrent reads and writes,
// with an optional callback invoked on each Write.
type safeBuffer struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	onWrite func() // called after each successful write (under no lock)
}

func (b *safeBuffer) Write(p []byte) (n int, err error) {
	b.mu.Lock()
	n, err = b.buf.Write(p)
	b.mu.Unlock()
	if n > 0 && b.onWrite != nil {
		b.onWrite()
	}
	return
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Ensure safeBuffer implements io.Writer.
var _ io.Writer = (*safeBuffer)(nil)

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
