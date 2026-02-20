package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// BashTool 执行 shell 命令
type BashTool struct{}

func (t *BashTool) Name() string        { return "bash" }
func (t *BashTool) IsReadOnly() bool     { return false }
func (t *BashTool) PermissionLevel() PermissionLevel { return PermissionExecute }

func (t *BashTool) Description() string {
	return "Execute a shell command and return its combined stdout and stderr output."
}

const (
	defaultBashTimeout = 120 * time.Second
	maxBashTimeout     = 600 * time.Second
)

func (t *BashTool) Parameters() map[string]any {
	return map[string]any{
		"command": map[string]any{
			"type":        "string",
			"description": "The shell command to execute",
		},
		"timeout": map[string]any{
			"type":        "integer",
			"description": "Timeout in seconds (default 120, max 600)",
		},
	}
}

func (t *BashTool) Execute(ctx context.Context, params json.RawMessage) (ToolResult, error) {
	var p struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("invalid params: %w", err)
	}
	if p.Command == "" {
		return ToolResult{}, fmt.Errorf("command is required")
	}

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
				Content: fmt.Sprintf("Command timed out after %dm %ds", secs/60, secs%60),
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

// shellBin returns the path to the system shell.
func shellBin() string {
	if p, err := exec.LookPath("sh"); err == nil {
		return p
	}
	for _, candidate := range []string{"/bin/sh", "/usr/bin/sh"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "sh"
}
