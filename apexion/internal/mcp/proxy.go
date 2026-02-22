package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/apexion-ai/apexion/internal/tools"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPToolProxy wraps a single MCP tool as a tools.Tool so it can be called by the agent.
//
// Tool name format: mcp__<server>__<tool>
// Example: mcp__filesystem__read_file
type MCPToolProxy struct {
	serverName string
	tool       *mcpsdk.Tool
	manager    *Manager
	fullName   string
}

// Ensure MCPToolProxy implements tools.Tool.
var _ tools.Tool = (*MCPToolProxy)(nil)

func (p *MCPToolProxy) Name() string { return p.fullName }

func (p *MCPToolProxy) Description() string {
	desc := p.tool.Description
	if desc == "" {
		return fmt.Sprintf("[MCP: %s] %s", p.serverName, p.tool.Name)
	}
	return fmt.Sprintf("[MCP: %s] %s", p.serverName, desc)
}

// Parameters extracts properties from InputSchema (any, actually map[string]any).
func (p *MCPToolProxy) Parameters() map[string]any {
	return extractProperties(p.tool.InputSchema)
}

func (p *MCPToolProxy) Execute(ctx context.Context, params json.RawMessage) (tools.ToolResult, error) {
	// Parse arguments from the LLM
	var args map[string]any
	if len(params) > 0 {
		if err := json.Unmarshal(params, &args); err != nil {
			return tools.ToolResult{
				Content: fmt.Sprintf("invalid params: %v", err),
				IsError: true,
			}, nil
		}
	}
	if args == nil {
		args = map[string]any{}
	}

	output, isError, err := p.manager.CallTool(ctx, p.serverName, p.tool.Name, args)
	if err != nil {
		return tools.ToolResult{
			Content: fmt.Sprintf("mcp tool error: %v", err),
			IsError: true,
		}, nil
	}

	return tools.ToolResult{
		Content: output,
		IsError: isError,
	}, nil
}

// IsReadOnly returns false; MCP tools are not read-only by default and require confirmation.
func (p *MCPToolProxy) IsReadOnly() bool { return false }

// PermissionLevel returns PermissionExecute; MCP tools require user confirmation.
func (p *MCPToolProxy) PermissionLevel() tools.PermissionLevel {
	return tools.PermissionExecute
}

// RegisterTools registers all connected servers' tools from the manager into the registry.
// Returns the total number of tools registered.
func RegisterTools(manager *Manager, registry *tools.Registry) int {
	count := 0
	for serverName, serverTools := range manager.AllTools() {
		for _, t := range serverTools {
			proxy := &MCPToolProxy{
				serverName: serverName,
				tool:       t,
				manager:    manager,
				fullName:   fmt.Sprintf("mcp__%s__%s", serverName, t.Name),
			}
			registry.Register(proxy)
			count++
		}
	}
	return count
}

// ── Schema conversion ────────────────────────────────────────────────────────

// extractProperties extracts JSON Schema properties from MCP Tool.InputSchema (any).
//
// When the MCP client receives tools from a server, InputSchema is a JSON-deserialized
// map[string]any with structure {"type":"object","properties":{...},...}.
func extractProperties(schema any) map[string]any {
	if schema == nil {
		return map[string]any{}
	}

	m, ok := schema.(map[string]any)
	if !ok {
		return map[string]any{}
	}

	props, ok := m["properties"].(map[string]any)
	if !ok {
		return map[string]any{}
	}

	return props
}
