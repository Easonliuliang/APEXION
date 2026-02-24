package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/apexion-ai/apexion/internal/mcp"
)

const mcpLazyConnectTimeout = 8 * time.Second

// ensureMCPToolsForCurrentTurn lazily connects MCP servers relevant to the
// latest user turn and registers their tools into the live registry.
func (a *Agent) ensureMCPToolsForCurrentTurn(ctx context.Context) {
	if a.mcpManager == nil {
		return
	}
	userText, hasImage, _ := latestUserTurnContext(a.session.Messages)
	a.ensureMCPToolsForInput(ctx, userText, hasImage)
}

// ensureMCPToolsForInput lazily connects relevant MCP servers for input/image
// context. Errors are isolated and never returned to the caller.
func (a *Agent) ensureMCPToolsForInput(ctx context.Context, userText string, hasImage bool) {
	if a.mcpManager == nil {
		return
	}
	a.mcpManager.CleanupIdle()
	targets := desiredMCPServersForInput(a.mcpManager, a.config.Provider, userText, hasImage)
	if len(targets) == 0 {
		return
	}

	connectCtx, cancel := context.WithTimeout(ctx, mcpLazyConnectTimeout)
	defer cancel()

	connected, errs := a.mcpManager.EnsureConnected(connectCtx, targets)
	registered := mcp.RegisterToolsForServers(a.mcpManager, a.executor.Registry(), targets)

	if a.eventLogger != nil {
		if registered > 0 || len(connected) > 0 || len(errs) > 0 {
			a.eventLogger.Log(EventType("mcp_lazy_connect"), map[string]any{
				"targets":    targets,
				"connected":  connected,
				"registered": registered,
				"errors":     formatMCPErrorStrings(errs),
			})
		}
	}
	if a.config.ToolRouting.Debug {
		if len(errs) > 0 {
			a.io.SystemMessage(fmt.Sprintf("MCP lazy connect warning: %s", strings.Join(formatMCPErrorStrings(errs), " | ")))
		} else if len(connected) > 0 {
			a.io.SystemMessage(fmt.Sprintf("MCP lazy connect: connected=%s", strings.Join(connected, ",")))
		}
	}
}

func desiredMCPServersForInput(mgr *mcp.Manager, providerName, userText string, hasImage bool) []string {
	add := func(out []string, name string) []string {
		if !mgr.HasServer(name) {
			return out
		}
		for _, s := range out {
			if s == name {
				return out
			}
		}
		return append(out, name)
	}

	var out []string
	s := strings.ToLower(strings.TrimSpace(userText))
	tokens := tokenizeWords(s)

	if hasImage {
		// Image understanding is typically provided by MiniMax Coding Plan MCP.
		out = add(out, "minimax")
	}
	if isDocsQuery(s, tokens) {
		out = add(out, "context7")
	}
	if isGitHubQuery(s, tokens) {
		out = add(out, "github")
	}

	_ = providerName // kept for future provider-specific MCP routing.
	return out
}

func isDocsQuery(s string, tokens map[string]bool) bool {
	if strings.Contains(s, "官方文档") || strings.Contains(s, "查文档") || strings.Contains(s, "文档") || strings.Contains(s, "教程") || strings.Contains(s, "示例") || strings.Contains(s, "用法") {
		return true
	}
	if strings.Contains(s, "documentation") || strings.Contains(s, "docs") || strings.Contains(s, "official") || strings.Contains(s, "api") || strings.Contains(s, "sdk") {
		return true
	}
	return tokens["docs"] || tokens["documentation"] || tokens["official"] || tokens["api"] || tokens["sdk"] || tokens["library"]
}

func isGitHubQuery(s string, tokens map[string]bool) bool {
	if strings.Contains(s, "github.com/") || strings.Contains(s, "github ") {
		return true
	}
	if strings.Contains(s, "仓库") || strings.Contains(s, "项目地址") || strings.Contains(s, "星标") || strings.Contains(s, "点赞") {
		return true
	}
	return tokens["github"] || tokens["repo"] || tokens["repository"] || tokens["star"] || tokens["stars"] || tokens["issue"] || tokens["issues"]
}

func tokenizeWords(s string) map[string]bool {
	out := make(map[string]bool)
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return !(r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out[p] = true
	}
	return out
}

func formatMCPErrorStrings(errs []error) []string {
	if len(errs) == 0 {
		return nil
	}
	out := make([]string, 0, len(errs))
	for _, err := range errs {
		if err == nil {
			continue
		}
		out = append(out, err.Error())
	}
	return out
}
