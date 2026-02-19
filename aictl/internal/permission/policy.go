package permission

import (
	"encoding/json"
	"strings"

	"github.com/aictl/aictl/internal/config"
)

// DefaultPolicy implements permission checks based on config.
type DefaultPolicy struct {
	AutoApprove      bool
	AutoApproveTools map[string]bool
	AllowedCommands  []string
}

// NewDefaultPolicy creates a policy from config.
func NewDefaultPolicy(cfg *config.PermissionConfig) *DefaultPolicy {
	approveTools := make(map[string]bool, len(cfg.AutoApproveTools))
	for _, name := range cfg.AutoApproveTools {
		approveTools[name] = true
	}

	autoApprove := cfg.Mode == "auto-approve" || cfg.Mode == "yolo"

	return &DefaultPolicy{
		AutoApprove:      autoApprove,
		AutoApproveTools: approveTools,
		AllowedCommands:  cfg.AllowedCommands,
	}
}

// Check determines whether a tool call is allowed.
func (p *DefaultPolicy) Check(toolName string, params json.RawMessage) Decision {
	if p.AutoApprove {
		return Allow
	}
	if p.AutoApproveTools[toolName] {
		return Allow
	}

	// For bash tool, check command whitelist
	if toolName == "bash" {
		var args struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(params, &args); err == nil {
			if p.IsCommandAllowed(args.Command) {
				return Allow
			}
		}
		return NeedConfirmation
	}

	return NeedConfirmation
}

// IsCommandAllowed checks if a bash command matches any whitelist prefix.
func (p *DefaultPolicy) IsCommandAllowed(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	for _, allowed := range p.AllowedCommands {
		if strings.HasPrefix(cmd, allowed) {
			return true
		}
	}
	return false
}
