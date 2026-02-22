package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/apexion-ai/apexion/internal/tools"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPToolProxy 将单个 MCP 工具包装为 tools.Tool，使其可以被 agent 调用。
//
// 工具名格式：mcp__<server>__<tool>
// 例如：mcp__filesystem__read_file
type MCPToolProxy struct {
	serverName string
	tool       *mcpsdk.Tool
	manager    *Manager
	fullName   string
}

// 确保 MCPToolProxy 实现了 tools.Tool 接口。
var _ tools.Tool = (*MCPToolProxy)(nil)

func (p *MCPToolProxy) Name() string { return p.fullName }

func (p *MCPToolProxy) Description() string {
	desc := p.tool.Description
	if desc == "" {
		return fmt.Sprintf("[MCP: %s] %s", p.serverName, p.tool.Name)
	}
	return fmt.Sprintf("[MCP: %s] %s", p.serverName, desc)
}

// Parameters 从 InputSchema（any，实际为 map[string]any）中提取 properties。
func (p *MCPToolProxy) Parameters() map[string]any {
	return extractProperties(p.tool.InputSchema)
}

func (p *MCPToolProxy) Execute(ctx context.Context, params json.RawMessage) (tools.ToolResult, error) {
	// 解析 LLM 传入的参数
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

// IsReadOnly MCP 工具默认不视为只读，要求用户确认。
func (p *MCPToolProxy) IsReadOnly() bool { return false }

// PermissionLevel MCP 工具默认需要用户确认执行。
func (p *MCPToolProxy) PermissionLevel() tools.PermissionLevel {
	return tools.PermissionExecute
}

// RegisterTools 将 manager 中所有已连接 server 的工具注册到 registry。
// 返回注册的工具总数。
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

// ── Schema 转换 ───────────────────────────────────────────────────────────────

// extractProperties 从 MCP Tool.InputSchema（any）中提取 JSON Schema properties。
//
// 当 MCP 客户端从 server 接收工具时，InputSchema 是 JSON 反序列化的 map[string]any，
// 结构为 {"type":"object","properties":{...},...}。
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
