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
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// HookEvent represents when a hook fires.
type HookEvent string

const (
	HookPreTool      HookEvent = "pre_tool"
	HookPostTool     HookEvent = "post_tool"
	HookSessionStart HookEvent = "session_start"
	HookSessionStop  HookEvent = "session_stop"
	HookNotification HookEvent = "notification"
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
		PreTool      []HookEntry `yaml:"pre_tool"`
		PostTool     []HookEntry `yaml:"post_tool"`
		SessionStart []HookEntry `yaml:"session_start"`
		SessionStop  []HookEntry `yaml:"session_stop"`
		Notification []HookEntry `yaml:"notification"`
	} `yaml:"hooks"`
}

// HookManager loads and executes hooks.
type HookManager struct {
	preHooks      []HookEntry
	postHooks     []HookEntry
	sessionStart  []HookEntry
	sessionStop   []HookEntry
	notification  []HookEntry
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

		// Lifecycle hooks don't use matcher — they always fire.
		for i := range cfg.Hooks.SessionStart {
			h := &cfg.Hooks.SessionStart[i]
			if h.Timeout <= 0 {
				h.Timeout = 10
			}
			hm.sessionStart = append(hm.sessionStart, *h)
		}
		for i := range cfg.Hooks.SessionStop {
			h := &cfg.Hooks.SessionStop[i]
			if h.Timeout <= 0 {
				h.Timeout = 10
			}
			hm.sessionStop = append(hm.sessionStop, *h)
		}
		for i := range cfg.Hooks.Notification {
			h := &cfg.Hooks.Notification[i]
			if h.Timeout <= 0 {
				h.Timeout = 10
			}
			hm.notification = append(hm.notification, *h)
		}
	}

	return hm
}

// HasHooks returns true if any hooks are configured.
func (hm *HookManager) HasHooks() bool {
	return len(hm.preHooks) > 0 || len(hm.postHooks) > 0 ||
		len(hm.sessionStart) > 0 || len(hm.sessionStop) > 0 ||
		len(hm.notification) > 0
}

// RunLifecycleHooks runs hooks for a lifecycle event (session_start, session_stop, notification).
// Data is passed as JSON on stdin. Failures are silently ignored.
func (hm *HookManager) RunLifecycleHooks(ctx context.Context, event HookEvent, data map[string]string) {
	var hooks []HookEntry
	switch event {
	case HookSessionStart:
		hooks = hm.sessionStart
	case HookSessionStop:
		hooks = hm.sessionStop
	case HookNotification:
		hooks = hm.notification
	default:
		return
	}

	for _, hook := range hooks {
		input := HookInput{
			ToolName: string(event),
		}
		if data != nil {
			raw, _ := json.Marshal(data)
			input.Params = raw
		}
		_ = runHookCommand(ctx, hook, input)
	}
}

// Summary returns a human-readable listing of all configured hooks.
func (hm *HookManager) Summary() string {
	type namedHooks struct {
		name  string
		hooks []HookEntry
	}
	groups := []namedHooks{
		{"pre_tool", hm.preHooks},
		{"post_tool", hm.postHooks},
		{"session_start", hm.sessionStart},
		{"session_stop", hm.sessionStop},
		{"notification", hm.notification},
	}

	total := 0
	for _, g := range groups {
		total += len(g.hooks)
	}
	if total == 0 {
		return "No hooks configured.\nPlace hooks.yaml in .apexion/ or ~/.config/apexion/"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Configured hooks (%d):\n", total))
	for _, g := range groups {
		if len(g.hooks) == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("\n  %s (%d):\n", g.name, len(g.hooks)))
		for _, h := range g.hooks {
			matcher := h.Matcher
			if matcher == "" {
				matcher = "*"
			}
			sb.WriteString(fmt.Sprintf("    [%s] %s (timeout: %ds)\n", matcher, h.Command, h.Timeout))
		}
	}
	return strings.TrimRight(sb.String(), "\n")
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
