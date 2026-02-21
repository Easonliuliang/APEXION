package permission

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/aictl/aictl/internal/config"
)

// DefaultPolicy implements permission checks based on config.
type DefaultPolicy struct {
	mode             string
	autoApproveTools map[string]bool
	allowedCommands  []string
	deniedCommands   []string
	allowedPaths     []string

	// Session-level approval memory: once a user confirms a tool+pattern,
	// subsequent similar calls are auto-approved within the same session.
	mu        sync.RWMutex
	approvals map[string]bool // key: "tool:pattern"
}

// NewDefaultPolicy creates a policy from config.
func NewDefaultPolicy(cfg *config.PermissionConfig) *DefaultPolicy {
	approveTools := make(map[string]bool, len(cfg.AutoApproveTools))
	for _, name := range cfg.AutoApproveTools {
		approveTools[name] = true
	}
	return &DefaultPolicy{
		mode:             cfg.Mode,
		autoApproveTools: approveTools,
		allowedCommands:  cfg.AllowedCommands,
		deniedCommands:   cfg.DeniedCommands,
		allowedPaths:     cfg.AllowedPaths,
		approvals:        make(map[string]bool),
	}
}

// Check determines whether a tool call is allowed, denied, or needs confirmation.
func (p *DefaultPolicy) Check(toolName string, params json.RawMessage) Decision {
	// Denied commands override everything (even yolo mode).
	if toolName == "bash" {
		cmd := extractField(params, "command")
		if p.isCommandDenied(cmd) {
			return Deny
		}
	}

	// Path restriction for write tools (even in yolo mode).
	if toolName == "edit_file" || toolName == "write_file" {
		path := extractField(params, "file_path")
		if path != "" && !p.isPathAllowed(path) {
			return Deny
		}
	}

	// Yolo: allow everything not explicitly denied.
	if p.mode == "yolo" {
		return Allow
	}

	// Auto-approve tools (read-only tools, user-configured list).
	if p.autoApproveTools[toolName] {
		return Allow
	}

	// Auto-approve mode: check command whitelist for bash.
	if p.mode == "auto-approve" {
		if toolName == "bash" {
			cmd := extractField(params, "command")
			if p.isCommandAllowed(cmd) {
				return Allow
			}
			if p.HasApproval(toolName, params) {
				return Allow
			}
			return NeedConfirmation
		}
		// Non-bash tools in auto-approve mode.
		return Allow
	}

	// Interactive mode (default): need confirmation for non-auto-approved tools.
	if toolName == "bash" {
		cmd := extractField(params, "command")
		if p.isCommandAllowed(cmd) {
			return Allow
		}
	}

	// Check session-level approval memory before requiring confirmation.
	if p.HasApproval(toolName, params) {
		return Allow
	}

	return NeedConfirmation
}

// isCommandAllowed checks if a bash command matches any whitelist prefix.
// It enforces word boundaries (the character after the prefix must be a space
// or end-of-string) and rejects commands containing shell metacharacters that
// could be used for command injection.
func (p *DefaultPolicy) isCommandAllowed(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	if containsShellMetachar(cmd) {
		return false
	}
	for _, allowed := range p.allowedCommands {
		if strings.HasPrefix(cmd, allowed) {
			// Ensure word boundary: next char must be space or end-of-string.
			if len(cmd) == len(allowed) || cmd[len(allowed)] == ' ' {
				return true
			}
		}
	}
	return false
}

// containsShellMetachar returns true if the command contains shell
// metacharacters that could be used for command injection.
func containsShellMetachar(cmd string) bool {
	for _, meta := range []string{";", "|", "&&", "||", "$(", "`"} {
		if strings.Contains(cmd, meta) {
			return true
		}
	}
	return false
}

// isCommandDenied checks if a bash command matches any blacklist entry.
func (p *DefaultPolicy) isCommandDenied(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	for _, denied := range p.deniedCommands {
		if strings.Contains(cmd, denied) {
			return true
		}
	}
	return false
}

// isPathAllowed checks if a file path is within allowed path globs.
// Empty allowedPaths means all paths are allowed.
func (p *DefaultPolicy) isPathAllowed(path string) bool {
	if len(p.allowedPaths) == 0 {
		return true
	}
	// Normalize the path to prevent traversal attacks like ./src/../../../etc/passwd.
	path = filepath.Clean(path)
	for _, pattern := range p.allowedPaths {
		if matched, _ := filepath.Match(pattern, path); matched {
			return true
		}
		// Also try matching with ** style: check if path starts with the dir prefix.
		if strings.HasSuffix(pattern, "/**") {
			prefix := filepath.Clean(strings.TrimSuffix(pattern, "/**"))
			// Use prefix + separator to prevent "srcfoo/bar" matching "src".
			if strings.HasPrefix(path, prefix+string(filepath.Separator)) {
				return true
			}
		}
	}
	return false
}

// approvalKey generates a key for the session approval map.
// For bash: "bash:cmd_prefix" (first word of command).
// For file tools: "edit_file:/path" or "write_file:/path".
// For others: just the tool name.
func approvalKey(toolName string, params json.RawMessage) string {
	switch toolName {
	case "bash":
		cmd := strings.TrimSpace(extractField(params, "command"))
		// Use first word as the approval pattern (e.g., "go", "npm", "make").
		if i := strings.IndexByte(cmd, ' '); i > 0 {
			return "bash:" + cmd[:i]
		}
		return "bash:" + cmd
	case "edit_file", "write_file":
		path := extractField(params, "file_path")
		return toolName + ":" + path
	default:
		return toolName
	}
}

// RememberApproval stores a session-level approval for a tool+params pattern.
func (p *DefaultPolicy) RememberApproval(toolName string, params json.RawMessage) {
	key := approvalKey(toolName, params)
	p.mu.Lock()
	p.approvals[key] = true
	p.mu.Unlock()
}

// HasApproval checks if a similar tool call was previously approved in this session.
func (p *DefaultPolicy) HasApproval(toolName string, params json.RawMessage) bool {
	key := approvalKey(toolName, params)
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.approvals[key]
}

// Approvals returns a formatted list of current session approvals.
func (p *DefaultPolicy) Approvals() string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.approvals) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Session approvals (%d):\n", len(p.approvals)))
	for key := range p.approvals {
		sb.WriteString("  " + key + "\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// ResetApprovals clears all session-level approvals.
func (p *DefaultPolicy) ResetApprovals() {
	p.mu.Lock()
	p.approvals = make(map[string]bool)
	p.mu.Unlock()
}

// extractField extracts a string field from JSON params.
func extractField(params json.RawMessage, field string) string {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(params, &m); err != nil {
		return ""
	}
	raw, ok := m[field]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}
