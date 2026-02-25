package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/apexion-ai/apexion/internal/provider"
	"github.com/apexion-ai/apexion/internal/router"
)

// tryDeterministicFastpath executes one deterministic tool call before model chat
// when router confidence is high enough.
func (a *Agent) tryDeterministicFastpath(ctx context.Context, disableWebFetch bool) (ran bool, interrupted bool) {
	if !a.config.ToolRouting.Enabled || !a.config.ToolRouting.DeterministicFastpath {
		return false, false
	}
	userText, hasImage, hasToolResult := latestUserTurnContext(a.session.Messages)
	if strings.TrimSpace(userText) == "" || hasImage || hasToolResult {
		return false, false
	}

	registryTools := a.executor.Registry().All()
	candidates := make([]router.CandidateTool, 0, len(registryTools))
	for _, t := range registryTools {
		if a.planMode && !t.IsReadOnly() {
			continue
		}
		if a.promptVariant == "lite" && liteExcludedTools[t.Name()] {
			continue
		}
		if disableWebFetch && t.Name() == "web_fetch" {
			continue
		}
		candidates = append(candidates, router.CandidateTool{
			Name:        t.Name(),
			Description: t.Description(),
			ReadOnly:    t.IsReadOnly(),
		})
	}
	if len(candidates) == 0 {
		return false, false
	}

	modelImageSupported, _, model := a.imageInputSupport()
	strategy := router.NormalizeRoutingStrategy(router.RoutingStrategy(a.config.ToolRouting.Strategy))
	plan := router.Plan(router.PlanInput{
		UserText:            userText,
		HasImage:            false,
		ModelImageSupported: modelImageSupported,
		Provider:            a.config.Provider,
		Model:               model,
		Tools:               candidates,
	}, router.PlanOptions{
		MaxCandidates:         a.config.ToolRouting.MaxCandidates,
		Strategy:              strategy,
		ShadowEval:            a.config.ToolRouting.ShadowEval,
		ShadowSampleRate:      a.config.ToolRouting.ShadowSampleRate,
		DeterministicFastpath: true,
		FastpathConfidence:    a.config.ToolRouting.FastpathConfidence,
	})
	if plan.FastPath == nil {
		return false, false
	}
	if _, ok := a.executor.Registry().Get(plan.FastPath.Tool); !ok {
		return false, false
	}

	input := json.RawMessage(plan.FastPath.InputJSON)
	if len(strings.TrimSpace(plan.FastPath.InputJSON)) == 0 || !json.Valid(input) {
		return false, false
	}

	call := &provider.ToolCallRequest{
		ID:    "fastpath-1",
		Name:  plan.FastPath.Tool,
		Input: input,
	}
	if a.eventLogger != nil {
		a.eventLogger.Log(EventType("tool_fastpath"), map[string]any{
			"tool_name":    plan.FastPath.Tool,
			"task":         plan.FastPath.Task,
			"confidence":   plan.FastPath.Confidence,
			"user_text":    userText,
			"input_json":   plan.FastPath.InputJSON,
			"route_intent": plan.Intent,
		})
	}
	if a.config.ToolRouting.Debug {
		a.io.SystemMessage(fmt.Sprintf("Fastpath: %s (%.2f)", plan.FastPath.Tool, plan.FastPath.Confidence))
	}

	a.session.AddMessage(buildAssistantMessage("", []*provider.ToolCallRequest{call}))
	results, wasInterrupted, _ := a.executeSingleToolCall(ctx, call)
	a.session.AddMessage(provider.Message{
		Role:    provider.RoleUser,
		Content: results,
	})
	return true, wasInterrupted
}
