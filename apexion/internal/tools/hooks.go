package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

// HookEvent represents when a hook fires.
type HookEvent string

const (
	HookPreTool  HookEvent = "pre_tool"
	HookPostTool HookEvent = "post_tool"
)

// HookEntry is a single hook definition from configuration.
type HookEntry struct {
	Matcher string `yaml:"matcher"`          // regex pattern to match tool names
	Command string `yaml:"command"`          // shell command to execute
	Timeout int    `yaml:"timeout"`          // timeout in seconds (default 10)
	re      *regexp.Regexp                   // compiled matcher
}

// HooksConfig represents the hooks configuration file.
type HooksConfig struct {
	Hooks struct {
		PreTool  []HookEntry `yaml:"pre_tool"`
		PostTool []HookEntry `yaml:"post_tool"`
	} `yaml:"hooks"`
}

// HookManager loads and executes hooks.
type HookManager struct {
	preHooks  []HookEntry
	postHooks []HookEntry
}

// LoadHooks loads hook configuration from .apexion/hooks.yaml and ~/.config/apexion/hooks.yaml.
// Project-level hooks take precedence (loaded first), then global hooks.
func LoadHooks(cwd string) *HookManager {
	hm := &HookManager{}

	paths := []string{
		filepath.Join(cwd, ".apexion", "hooks.yaml"),
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".config", "apexion", "hooks.yaml"))
	}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var cfg HooksConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			continue
		}

		for i := range cfg.Hooks.PreTool {
			h := &cfg.Hooks.PreTool[i]
			if h.Timeout <= 0 {
				h.Timeout = 10
			}
			if re, err := regexp.Compile(h.Matcher); err == nil {
				h.re = re
				hm.preHooks = append(hm.preHooks, *h)
			}
		}
		for i := range cfg.Hooks.PostTool {
			h := &cfg.Hooks.PostTool[i]
			if h.Timeout <= 0 {
				h.Timeout = 10
			}
			if re, err := regexp.Compile(h.Matcher); err == nil {
				h.re = re
				hm.postHooks = append(hm.postHooks, *h)
			}
		}
	}

	return hm
}

// HasHooks returns true if any hooks are configured.
func (hm *HookManager) HasHooks() bool {
	return len(hm.preHooks) > 0 || len(hm.postHooks) > 0
}

// HookInput is the JSON payload sent to hook stdin.
type HookInput struct {
	ToolName string          `json:"tool_name"`
	Params   json.RawMessage `json:"params"`
	Result   string          `json:"result,omitempty"`  // only for post hooks
	IsError  bool            `json:"is_error,omitempty"` // only for post hooks
}

// HookResult contains the result of running a hook.
type HookResult struct {
	Blocked bool   // true if exit code 2 (block the tool call)
	Message string // stderr output (used as error message when blocked)
}

// RunPreHooks executes all matching pre_tool hooks.
// Returns a HookResult indicating if the tool call should be blocked.
func (hm *HookManager) RunPreHooks(ctx context.Context, toolName string, params json.RawMessage) HookResult {
	for _, hook := range hm.preHooks {
		if hook.re == nil || !hook.re.MatchString(toolName) {
			continue
		}

		input := HookInput{
			ToolName: toolName,
			Params:   params,
		}
		result := runHookCommand(ctx, hook, input)
		if result.Blocked {
			return result
		}
	}
	return HookResult{}
}

// RunPostHooks executes all matching post_tool hooks.
// Post-hook failures are silently ignored.
func (hm *HookManager) RunPostHooks(ctx context.Context, toolName string, params json.RawMessage, toolResult string, isError bool) {
	for _, hook := range hm.postHooks {
		if hook.re == nil || !hook.re.MatchString(toolName) {
			continue
		}

		input := HookInput{
			ToolName: toolName,
			Params:   params,
			Result:   toolResult,
			IsError:  isError,
		}
		_ = runHookCommand(ctx, hook, input) // silently ignore post-hook failures
	}
}

// runHookCommand executes a single hook command.
func runHookCommand(ctx context.Context, hook HookEntry, input HookInput) HookResult {
	timeout := time.Duration(hook.Timeout) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", hook.Command)

	// Send input as JSON on stdin.
	inputJSON, _ := json.Marshal(input)
	cmd.Stdin = bytes.NewReader(inputJSON)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 2 {
				msg := stderr.String()
				if msg == "" {
					msg = fmt.Sprintf("hook blocked tool call: %s", hook.Command)
				}
				return HookResult{Blocked: true, Message: msg}
			}
		}
		// Non-blocking failure — return without blocking.
	}

	return HookResult{}
}
