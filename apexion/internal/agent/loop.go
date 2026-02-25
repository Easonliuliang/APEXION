package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apexion-ai/apexion/internal/provider"
	"github.com/apexion-ai/apexion/internal/router"
	"github.com/apexion-ai/apexion/internal/session"
	"github.com/apexion-ai/apexion/internal/tools"
)

// runAgentLoop executes the core agentic loop:
//  1. Send messages to the LLM via streaming Chat()
//  2. Collect text deltas (stream to UI) and tool calls
//  3. If tool calls exist, execute them, append results to history, and loop
//  4. If no tool calls, return (wait for next user input)
//
// A per-turn child context is created so that Esc can cancel the entire turn
// (including LLM streaming) without affecting the session-level context.
func (a *Agent) runAgentLoop(ctx context.Context) error {
	maxIter := a.config.MaxIterations // 0 = unlimited
	disableWebFetchForImageTurn := shouldDisableWebFetchForImageTurn(a.session.Messages)
	if disableWebFetchForImageTurn {
		a.io.SystemMessage("Image attached: temporarily disabling web_fetch for this turn to avoid URL-fetch misrouting.")
	}

	// Per-turn context: Esc cancels this, not the session.
	turnCtx, turnCancel := context.WithCancel(ctx)
	defer turnCancel()

	// Register the turn cancel with the UI so Esc can trigger it.
	if lc, ok := a.io.(tools.LoopCanceller); ok {
		lc.SetLoopCancel(turnCancel)
		defer lc.ClearLoopCancel()
	}

	// Compute token budget.
	contextWindow := a.config.ContextWindow
	if contextWindow <= 0 {
		contextWindow = a.provider.ContextWindow()
	}
	budget := session.NewTokenBudget(contextWindow, estimateTokens(a.systemPrompt))

	doomDetector := &doomLoopDetector{}
	failDetector := &failureLoopDetector{}
	fastpathTried := false

	for iteration := 0; maxIter == 0 || iteration < maxIter; iteration++ {
		// Check if the turn was cancelled before starting an iteration.
		if turnCtx.Err() != nil {
			a.io.SystemMessage("Interrupted.")
			return nil
		}

		// Lazy MCP: connect/register only servers needed for the current turn.
		a.ensureMCPToolsForCurrentTurn(turnCtx)

		// Optional deterministic fastpath: for high-confidence tasks (e.g. symbol lookup,
		// repo overview), execute one tool directly before the model round.
		if !fastpathTried {
			fastpathTried = true
			if ran, interrupted := a.tryDeterministicFastpath(turnCtx, disableWebFetchForImageTurn && iteration == 0); ran {
				if interrupted {
					a.io.SystemMessage("Interrupted.")
					return nil
				}
				continue
			}
		}

		// Two-stage auto-compaction.
		a.maybeCompact(turnCtx, budget)

		// Generate compacted copy for sending (does not modify session).
		compacted := session.CompactHistory(a.session.Messages, budget.HistoryMax, a.session.Summary)

		sysPrompt := a.systemPrompt
		if a.planMode {
			sysPrompt += "\n\n[PLAN MODE] You are in plan mode. Analyze the request, explore the codebase " +
				"using your read-only tools, then output a detailed implementation plan. Do NOT make any changes. " +
				"Structure your plan as:\n1. Files to modify (with paths)\n2. Changes for each file\n" +
				"3. Verification steps\nThe user will review your plan and switch to execution mode."
		}

		temp, topP := modelSamplingParams(a.config.Provider)
		req := &provider.ChatRequest{
			Model:        a.config.Model,
			Messages:     compacted,
			Tools:        a.buildToolSchemas(disableWebFetchForImageTurn && iteration == 0),
			SystemPrompt: sysPrompt,
			MaxTokens:    8192,
			Temperature:  temp,
			TopP:         topP,
		}

		var textContent strings.Builder
		var toolCalls []*provider.ToolCallRequest
		var streamErr error

		// Retry loop for transient API errors.
		for attempt := range maxRetries + 1 {
			textContent.Reset()
			toolCalls = nil
			streamErr = nil

			events, err := a.provider.Chat(turnCtx, req)
			if err != nil {
				// If cancelled by user Esc, exit gracefully.
				if turnCtx.Err() != nil {
					a.io.SystemMessage("Interrupted.")
					return nil
				}
				if attempt < maxRetries && isRetryableError(err) {
					delay := retryDelay(attempt)
					a.io.SystemMessage(formatRetryMessage(attempt, maxRetries, delay, err))
					if sleepErr := sleepWithContext(turnCtx, delay); sleepErr != nil {
						a.io.SystemMessage("Interrupted.")
						return nil
					}
					continue
				}
				return fmt.Errorf("LLM call failed: %w", err)
			}

			a.io.ThinkingStart()

			receivedContent := false
			for event := range events {
				// Check for user cancellation mid-stream.
				if turnCtx.Err() != nil {
					break
				}
				switch event.Type {
				case provider.EventTextDelta:
					receivedContent = true
					a.io.TextDelta(event.TextDelta)
					textContent.WriteString(event.TextDelta)

				case provider.EventToolCallDone:
					receivedContent = true
					toolCalls = append(toolCalls, event.ToolCall)

				case provider.EventDone:
					if event.Usage != nil {
						a.session.PromptTokens = event.Usage.InputTokens
						a.session.CompletionTokens = event.Usage.OutputTokens
						a.session.TokensUsed += event.Usage.InputTokens + event.Usage.OutputTokens
						a.io.SetTokens(a.session.TokensUsed)
						a.io.SetContextInfo(a.session.PromptTokens, contextWindow)

						// Record cost if tracker is available.
						if a.costTracker != nil {
							model := a.config.Model
							if model == "" {
								model = a.provider.DefaultModel()
							}
							a.costTracker.RecordTurn(model, event.Usage.InputTokens, event.Usage.OutputTokens)
							a.io.SetCost(a.costTracker.SessionCost())
						}
					}

				case provider.EventError:
					streamErr = event.Error
				}
			}

			// If user cancelled during streaming, exit gracefully.
			if turnCtx.Err() != nil {
				full := textContent.String()
				a.io.TextDone(full)
				if full != "" {
					a.session.AddMessage(buildAssistantMessage(full, nil))
				}
				a.io.SystemMessage("Interrupted.")
				return nil
			}

			// If stream error occurred before any content, retry if possible.
			if streamErr != nil && !receivedContent && attempt < maxRetries && isRetryableError(streamErr) {
				delay := retryDelay(attempt)
				a.io.SystemMessage(formatRetryMessage(attempt, maxRetries, delay, streamErr))
				if sleepErr := sleepWithContext(turnCtx, delay); sleepErr != nil {
					a.io.SystemMessage("Interrupted.")
					return nil
				}
				continue
			}

			// Stream error after content was received — save partial content and continue.
			if streamErr != nil {
				a.io.SystemMessage(fmt.Sprintf("Connection lost: %s (partial response preserved)", truncateError(streamErr)))
				break // exit retry loop, process whatever we received
			}

			break // success
		}

		full := stripThinkTags(textContent.String())
		a.io.TextDone(full)

		// Log assistant text output.
		if a.eventLogger != nil && full != "" {
			a.eventLogger.Log(EventAssistantText, map[string]string{"text": full})
		}

		assistantMsg := buildAssistantMessage(full, toolCalls)
		a.session.AddMessage(assistantMsg)

		if len(toolCalls) == 0 {
			return nil
		}

		if maxIter > 0 && iteration == maxIter-1 {
			a.io.SystemMessage(fmt.Sprintf(
				"warning: reached max iterations (%d), stopping", maxIter))
			return nil
		}

		// Doom loop detection: catch the model issuing identical tool calls repeatedly.
		switch doomDetector.check(toolCalls) {
		case doomLoopWarn:
			warning := "You have been issuing the same tool calls repeatedly. " +
				"This looks like an infinite loop. Try a different approach or stop calling tools."
			a.io.SystemMessage("warning: possible doom loop detected — injecting hint to model")
			a.session.AddMessage(provider.Message{
				Role: provider.RoleUser,
				Content: []provider.Content{{
					Type: provider.ContentTypeText,
					Text: "[SYSTEM] " + warning,
				}},
			})
		case doomLoopStop:
			a.io.SystemMessage("error: doom loop detected — same tool calls repeated 5 times, stopping")
			return nil
		}

		toolResults, interrupted := a.executeToolCalls(turnCtx, toolCalls)
		a.session.AddMessage(provider.Message{
			Role:    provider.RoleUser,
			Content: toolResults,
		})

		switch failDetector.check(toolCalls, toolResults) {
		case doomLoopWarn:
			warning := "Repeated tool failures detected, even after repair/fallback. " +
				"Use a different tool strategy and avoid repeating the same calls."
			a.io.SystemMessage("warning: repeated tool failures detected — injecting strategy hint")
			a.session.AddMessage(provider.Message{
				Role: provider.RoleUser,
				Content: []provider.Content{{
					Type: provider.ContentTypeText,
					Text: "[SYSTEM] " + warning,
				}},
			})
		case doomLoopStop:
			a.io.SystemMessage("error: repeated tool failures detected 4 times, stopping")
			return nil
		}

		// If user interrupted during tool execution, stop the loop
		// and return to user input. The partial results are already
		// in the message history for context continuity.
		if interrupted {
			a.io.SystemMessage("Interrupted.")
			return nil
		}
	}
	return nil
}

// buildAssistantMessage creates a history message from the LLM response.
func buildAssistantMessage(text string, toolCalls []*provider.ToolCallRequest) provider.Message {
	var contents []provider.Content

	if text != "" {
		contents = append(contents, provider.Content{
			Type: provider.ContentTypeText,
			Text: text,
		})
	}

	for _, tc := range toolCalls {
		contents = append(contents, provider.Content{
			Type:      provider.ContentTypeToolUse,
			ToolUseID: tc.ID,
			ToolName:  tc.Name,
			ToolInput: tc.Input,
		})
	}

	return provider.Message{Role: provider.RoleAssistant, Content: contents}
}

// executeToolCalls runs tool calls and returns tool_result content blocks.
// When multiple calls are present, they execute concurrently via goroutines.
// Results are kept in the same order as the input calls.
// The second return value is true if the user interrupted (Esc) during execution.
func (a *Agent) executeToolCalls(ctx context.Context, calls []*provider.ToolCallRequest) ([]provider.Content, bool) {
	// Single call: run inline (no goroutine overhead).
	if len(calls) == 1 {
		return a.executeSingleToolCall(ctx, calls[0])
	}

	// Multiple calls: run concurrently.
	type indexedResult struct {
		contents []provider.Content // tool_result + optional image
	}

	resultSlots := make([]indexedResult, len(calls))
	var interrupted atomic.Bool
	var wg sync.WaitGroup

	for i, call := range calls {
		wg.Add(1)
		go func(idx int, c *provider.ToolCallRequest) {
			defer wg.Done()

			// If another goroutine was interrupted, skip this one.
			if interrupted.Load() {
				return
			}
			if a.eventLogger != nil {
				a.eventLogger.Log(EventToolCall, map[string]any{
					"tool_name": c.Name,
					"tool_id":   c.ID,
				})
			}

			a.io.ToolStart(c.ID, c.Name, string(c.Input))
			started := time.Now()
			result, executedName, _ := a.executeToolWithRepair(ctx, c)
			latencyMs := time.Since(started).Milliseconds()
			a.io.ToolDone(c.ID, executedName, result.Content, result.IsError)
			if a.eventLogger != nil {
				health := a.toolHealthSnapshot(executedName, time.Now())
				a.eventLogger.Log(EventToolResult, map[string]any{
					"tool_name":              c.Name,
					"executed_tool":          executedName,
					"tool_id":                c.ID,
					"is_error":               result.IsError,
					"tool_health_score":      health.Score,
					"tool_circuit_open":      health.CircuitOpen,
					"tool_cooldown_sec":      health.CooldownRemainingSec,
					"tool_successes_total":   health.Successes,
					"tool_failures_total":    health.Failures,
					"tool_exec_latency_ms":   latencyMs,
					"tool_consecutive_fails": health.ConsecutiveFails,
				})
			}

			var contents []provider.Content
			contents = append(contents, provider.Content{
				Type:       provider.ContentTypeToolResult,
				ToolUseID:  c.ID,
				ToolResult: result.Content,
				IsError:    result.IsError,
			})
			if result.ImageData != "" {
				contents = append(contents, provider.Content{
					Type:           provider.ContentTypeImage,
					ImageData:      result.ImageData,
					ImageMediaType: result.ImageMediaType,
				})
			}
			resultSlots[idx] = indexedResult{contents: contents}

			if result.UserCancelled {
				interrupted.Store(true)
			}
		}(i, call)
	}

	wg.Wait()

	// Assemble results in order, filling in cancelled placeholders for skipped calls.
	var results []provider.Content
	wasInterrupted := interrupted.Load()

	for i, call := range calls {
		slot := resultSlots[i]
		if len(slot.contents) > 0 {
			results = append(results, slot.contents...)
		} else {
			// This call was skipped due to interruption.
			results = append(results, provider.Content{
				Type:       provider.ContentTypeToolResult,
				ToolUseID:  call.ID,
				ToolResult: "[User cancelled this turn — tool was not executed. Do not retry unless the user asks.]",
				IsError:    false,
			})
		}
	}

	return results, wasInterrupted
}

// executeSingleToolCall handles the simple case of a single tool call (no concurrency).
func (a *Agent) executeSingleToolCall(ctx context.Context, call *provider.ToolCallRequest) ([]provider.Content, bool) {
	if a.eventLogger != nil {
		a.eventLogger.Log(EventToolCall, map[string]any{
			"tool_name": call.Name,
			"tool_id":   call.ID,
		})
	}

	a.io.ToolStart(call.ID, call.Name, string(call.Input))
	started := time.Now()
	result, executedName, _ := a.executeToolWithRepair(ctx, call)
	latencyMs := time.Since(started).Milliseconds()
	a.io.ToolDone(call.ID, executedName, result.Content, result.IsError)

	if a.eventLogger != nil {
		health := a.toolHealthSnapshot(executedName, time.Now())
		a.eventLogger.Log(EventToolResult, map[string]any{
			"tool_name":              call.Name,
			"executed_tool":          executedName,
			"tool_id":                call.ID,
			"is_error":               result.IsError,
			"tool_health_score":      health.Score,
			"tool_circuit_open":      health.CircuitOpen,
			"tool_cooldown_sec":      health.CooldownRemainingSec,
			"tool_successes_total":   health.Successes,
			"tool_failures_total":    health.Failures,
			"tool_exec_latency_ms":   latencyMs,
			"tool_consecutive_fails": health.ConsecutiveFails,
		})
	}

	var results []provider.Content
	results = append(results, provider.Content{
		Type:       provider.ContentTypeToolResult,
		ToolUseID:  call.ID,
		ToolResult: result.Content,
		IsError:    result.IsError,
	})
	if result.ImageData != "" {
		results = append(results, provider.Content{
			Type:           provider.ContentTypeImage,
			ImageData:      result.ImageData,
			ImageMediaType: result.ImageMediaType,
		})
	}

	if result.UserCancelled {
		return results, true
	}
	return results, false
}

// maybeCompact runs three-stage auto-compaction if context is growing large.
//
// Phase 1 (70% threshold): Mask low-importance tool outputs (glob, grep, list_dir, etc.)
// Phase 2 (75% threshold): Mask low + medium-importance tool outputs (+ git_status, git_diff, etc.)
// Phase 3 (80% threshold): Full summarization + truncation (existing logic)
func (a *Agent) maybeCompact(ctx context.Context, budget *session.TokenBudget) {
	tokens := a.currentTokens(budget)

	// Phase 1: Mask low-importance tool outputs at 70%.
	if tokens >= budget.GentleThreshold() && a.session.GentleCompactPhase < 1 {
		before := tokens
		a.session.Messages = session.MaskOldToolOutputsSmart(a.session.Messages, 10, session.ToolImportanceLow)
		a.session.GentleCompactPhase = 1
		a.session.GentleCompactDone = true // backward compat
		after := a.currentTokens(budget)
		saved := before - after
		if saved > 0 {
			a.io.SystemMessage(fmt.Sprintf(
				"Context growing — masked low-importance tool outputs (%dk → %dk tokens, saved %dk).",
				before/1000, after/1000, saved/1000))
		}
		return // don't do multiple phases in the same iteration
	}

	// Phase 2: Mask low + medium-importance tool outputs at 75%.
	if tokens >= budget.Phase2Threshold() && a.session.GentleCompactPhase < 2 {
		before := tokens
		a.session.Messages = session.MaskOldToolOutputsSmart(a.session.Messages, 10, session.ToolImportanceMedium)
		a.session.GentleCompactPhase = 2
		after := a.currentTokens(budget)
		saved := before - after
		if saved > 0 {
			a.io.SystemMessage(fmt.Sprintf(
				"Context growing — masked medium-importance tool outputs (%dk → %dk tokens, saved %dk).",
				before/1000, after/1000, saved/1000))
		}
		return
	}

	// Phase 3: Full summarization at 80%.
	if tokens >= budget.CompactThreshold() && a.summarizer != nil {
		before := tokens
		// Also strip image data before summarization.
		a.session.Messages = session.StripImageData(a.session.Messages)
		a.io.SystemMessage("Compacting context (summarizing conversation)...")
		summary, err := a.summarizer.Summarize(ctx, a.session.Summary, a.session.Messages)
		if err != nil {
			a.io.Error("Compact failed: " + err.Error())
			return
		}
		a.session.Summary = summary
		a.session.Messages = session.TruncateSession(a.session.Messages, 10)
		a.session.GentleCompactPhase = 0 // reset for next cycle
		a.session.GentleCompactDone = false
		after := a.currentTokens(budget)
		a.io.SystemMessage(fmt.Sprintf(
			"Context compacted: %dk → %dk tokens. %d messages retained.",
			before/1000, after/1000, len(a.session.Messages)))

		if a.eventLogger != nil {
			a.eventLogger.Log(EventCompaction, map[string]any{
				"before_tokens": before,
				"after_tokens":  after,
			})
		}
	}
}

// currentTokens returns the current token usage estimate.
func (a *Agent) currentTokens(budget *session.TokenBudget) int {
	// Prefer actual API-reported tokens.
	if a.session.PromptTokens > 0 {
		return a.session.PromptTokens
	}
	return a.session.EstimateTokens() + estimateTokens(a.systemPrompt)
}

// stripThinkTags removes <think>...</think> blocks that some models (e.g. MiniMax, DeepSeek)
// include in their output. Handles partial tags from mid-stream disconnects.
func stripThinkTags(s string) string {
	for {
		start := strings.Index(s, "<think>")
		if start == -1 {
			break
		}
		end := strings.Index(s[start:], "</think>")
		if end == -1 {
			// Partial think block (e.g. stream cut off mid-think) — strip to end.
			s = strings.TrimSpace(s[:start])
			break
		}
		s = s[:start] + s[start+end+len("</think>"):]
	}
	return strings.TrimSpace(s)
}

// estimateTokens returns a rough token estimate for a string (chars / 4).
func estimateTokens(s string) int {
	return len(s) / 4
}

// shouldDisableWebFetchForImageTurn returns true when the latest user prompt
// in this turn contains attached images and the active provider is known to
// misroute image analysis into web_fetch URL calls.
func shouldDisableWebFetchForImageTurn(messages []provider.Message) bool {
	if len(messages) == 0 {
		return false
	}

	last := messages[len(messages)-1]
	if last.Role != provider.RoleUser {
		return false
	}

	hasImage := false
	hasToolResult := false
	for _, c := range last.Content {
		switch c.Type {
		case provider.ContentTypeImage:
			hasImage = true
		case provider.ContentTypeToolResult:
			// User role messages with tool_result are tool feedback rounds,
			// not the initial user prompt.
			hasToolResult = true
		}
	}
	return hasImage && !hasToolResult
}

// liteExcludedTools lists tools excluded in "lite" prompt variant.
// Weaker models tend to misuse these, wasting tokens.
var liteExcludedTools = map[string]bool{
	"todo_write": true,
	"todo_read":  true,
}

// buildToolSchemas converts the executor's registry tools into provider.ToolSchema.
// When plan mode is active, only read-only tools are included.
// When lite prompt variant is active, todo tools are excluded.
// When disableWebFetch is true, web_fetch is omitted for this turn.
func (a *Agent) buildToolSchemas(disableWebFetch bool) []provider.ToolSchema {
	registryTools := a.executor.Registry().All()
	schemas := make([]provider.ToolSchema, 0, len(registryTools))
	candidates := make([]router.CandidateTool, 0, len(registryTools))
	schemaByName := make(map[string]provider.ToolSchema, len(registryTools))
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
		schema := provider.ToolSchema{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		}
		schemas = append(schemas, schema)
		schemaByName[schema.Name] = schema
		candidates = append(candidates, router.CandidateTool{
			Name:        t.Name(),
			Description: t.Description(),
			ReadOnly:    t.IsReadOnly(),
		})
	}
	if !a.config.ToolRouting.Enabled || len(schemas) == 0 {
		return schemas
	}

	userText, hasImage, hasToolResult := latestUserTurnContext(a.session.Messages)
	if hasToolResult {
		// Keep full tool set on tool_result rounds to avoid breaking multi-step chains.
		return schemas
	}

	modelImageSupported, _, model := a.imageInputSupport()
	strategy := router.NormalizeRoutingStrategy(router.RoutingStrategy(a.config.ToolRouting.Strategy))
	plan := router.Plan(router.PlanInput{
		UserText:            userText,
		HasImage:            hasImage,
		ModelImageSupported: modelImageSupported,
		Provider:            a.config.Provider,
		Model:               model,
		Tools:               candidates,
	}, router.PlanOptions{
		MaxCandidates:         a.config.ToolRouting.MaxCandidates,
		Strategy:              strategy,
		ShadowEval:            a.config.ToolRouting.ShadowEval,
		ShadowSampleRate:      a.config.ToolRouting.ShadowSampleRate,
		DeterministicFastpath: a.config.ToolRouting.DeterministicFastpath,
		FastpathConfidence:    a.config.ToolRouting.FastpathConfidence,
	})

	if len(plan.Primary) == 0 {
		return schemas
	}

	ordered := make([]provider.ToolSchema, 0, len(schemas))
	seen := make(map[string]bool, len(schemas))
	primaryNames := make([]string, 0, len(plan.Primary))
	for _, p := range plan.Primary {
		if s, ok := schemaByName[p.Name]; ok {
			ordered = append(ordered, s)
			seen[p.Name] = true
			primaryNames = append(primaryNames, p.Name)
		}
	}
	// When MaxCandidates is enabled, enforce a hard top-N gate so the model
	// cannot bypass routing by selecting low-priority primitive tools.
	if a.config.ToolRouting.MaxCandidates <= 0 {
		for _, name := range plan.Fallback {
			if seen[name] {
				continue
			}
			if s, ok := schemaByName[name]; ok {
				ordered = append(ordered, s)
				seen[name] = true
			}
		}
		// Safety net: keep any tool that was not explicitly listed.
		for _, s := range schemas {
			if !seen[s.Name] {
				ordered = append(ordered, s)
			}
		}
	}

	if a.eventLogger != nil {
		eligible := len(candidates) - len(plan.Filtered)
		if eligible < 0 {
			eligible = 0
		}
		payload := map[string]any{
			"strategy":              strategy,
			"intent":                plan.Intent,
			"has_image":             hasImage,
			"model_image_supported": modelImageSupported,
			"eligible_tools":        eligible,
			"primary_tools":         primaryNames,
			"fallback_tools":        plan.Fallback,
			"filtered_tools":        plan.Filtered,
		}
		if plan.FastPath != nil {
			payload["fastpath_used"] = true
			payload["fastpath_tool"] = plan.FastPath.Tool
			payload["fastpath_task"] = plan.FastPath.Task
			payload["fastpath_confidence"] = plan.FastPath.Confidence
		}
		if plan.Shadow != nil {
			shadowNames := make([]string, 0, len(plan.Shadow.Primary))
			for _, s := range plan.Shadow.Primary {
				shadowNames = append(shadowNames, s.Name)
			}
			payload["shadow_strategy"] = plan.Shadow.Strategy
			payload["shadow_primary_tools"] = shadowNames
			payload["shadow_fallback_tools"] = plan.Shadow.Fallback
			payload["shadow_filtered_tools"] = plan.Shadow.Filtered
		}
		a.eventLogger.Log(EventToolRoute, payload)
	}
	if a.config.ToolRouting.Debug {
		msg := fmt.Sprintf("Tool router: strategy=%s intent=%s primary=%s", strategy, plan.Intent, strings.Join(primaryNames, ", "))
		if plan.FastPath != nil {
			msg = fmt.Sprintf("%s | fastpath=%s(%.2f)", msg, plan.FastPath.Tool, plan.FastPath.Confidence)
		}
		if plan.Shadow != nil {
			shadowNames := make([]string, 0, len(plan.Shadow.Primary))
			for _, s := range plan.Shadow.Primary {
				shadowNames = append(shadowNames, s.Name)
			}
			msg = fmt.Sprintf("%s | shadow=%s", msg, strings.Join(shadowNames, ", "))
		}
		a.io.SystemMessage(msg)
	}

	return ordered
}

// latestUserTurnContext returns the latest user text and whether that message
// contains image or tool_result blocks.
func latestUserTurnContext(messages []provider.Message) (text string, hasImage bool, hasToolResult bool) {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role != provider.RoleUser {
			continue
		}
		var parts []string
		for _, c := range msg.Content {
			switch c.Type {
			case provider.ContentTypeText:
				if t := strings.TrimSpace(c.Text); t != "" {
					parts = append(parts, t)
				}
			case provider.ContentTypeImage:
				hasImage = true
			case provider.ContentTypeToolResult:
				hasToolResult = true
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n")), hasImage, hasToolResult
	}
	return "", false, false
}

// modelSamplingParams returns the recommended Temperature and TopP for a provider.
// Returns nil for parameters that should use the API default.
func modelSamplingParams(providerName string) (temp *float64, topP *float64) {
	switch providerName {
	case "minimax":
		return ptrFloat64(0.7), ptrFloat64(0.95)
	case "kimi":
		return ptrFloat64(0.6), ptrFloat64(0.95)
	case "qwen":
		return ptrFloat64(0.55), nil
	case "glm":
		return ptrFloat64(0.7), ptrFloat64(0.95)
	case "doubao":
		return ptrFloat64(0.3), ptrFloat64(0.9)
	default:
		return nil, nil // deepseek, openai, anthropic, groq — use API defaults
	}
}

func ptrFloat64(v float64) *float64 { return &v }
