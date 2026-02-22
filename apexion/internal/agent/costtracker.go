package agent

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// ModelPricing holds per-million-token pricing for a model.
type ModelPricing struct {
	InputPerMillion  float64
	OutputPerMillion float64
}

// TurnCost records cost data for a single LLM turn.
type TurnCost struct {
	InputTokens  int
	OutputTokens int
	Cost         float64
	Model        string
	Timestamp    time.Time
}

// CostTracker accumulates token usage and dollar cost across turns.
type CostTracker struct {
	mu          sync.Mutex
	sessionCost float64
	turns       []TurnCost
	pricing     map[string]ModelPricing
}

// NewCostTracker creates a CostTracker with default pricing and optional overrides.
func NewCostTracker(overrides map[string]ModelPricing) *CostTracker {
	pricing := DefaultPricing()
	for k, v := range overrides {
		pricing[k] = v
	}
	return &CostTracker{pricing: pricing}
}

// DefaultPricing returns built-in pricing for well-known models.
func DefaultPricing() map[string]ModelPricing {
	return map[string]ModelPricing{
		// Anthropic
		"claude-sonnet-4-20250514":  {3.0, 15.0},
		"claude-opus-4-20250514":    {15.0, 75.0},
		"claude-haiku-4-5-20251001": {0.80, 4.0},
		// OpenAI
		"gpt-4o":        {2.50, 10.0},
		"gpt-4o-mini":   {0.15, 0.60},
		"gpt-4.1":       {2.0, 8.0},
		"gpt-4.1-mini":  {0.40, 1.60},
		"gpt-4.1-nano":  {0.10, 0.40},
		"o3":            {2.0, 8.0},
		"o3-mini":       {1.10, 4.40},
		"o4-mini":       {1.10, 4.40},
		// DeepSeek
		"deepseek-chat":     {0.27, 1.10},
		"deepseek-reasoner": {0.55, 2.19},
		// Google
		"gemini-2.5-pro":   {1.25, 10.0},
		"gemini-2.5-flash": {0.15, 0.60},
		// Mistral
		"codestral-latest": {0.30, 0.90},
	}
}

// RecordTurn records token usage for a single LLM turn and returns the turn cost.
func (ct *CostTracker) RecordTurn(model string, inputTokens, outputTokens int) float64 {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	cost := ct.calculateCost(model, inputTokens, outputTokens)
	ct.sessionCost += cost
	ct.turns = append(ct.turns, TurnCost{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		Cost:         cost,
		Model:        model,
		Timestamp:    time.Now(),
	})
	return cost
}

// SessionCost returns the total session cost in dollars.
func (ct *CostTracker) SessionCost() float64 {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return ct.sessionCost
}

// Summary returns a formatted string with cost details.
func (ct *CostTracker) Summary() string {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if len(ct.turns) == 0 {
		return "No usage recorded."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Session cost: $%.4f (%d turns)\n\n", ct.sessionCost, len(ct.turns)))

	totalIn, totalOut := 0, 0
	for i, t := range ct.turns {
		totalIn += t.InputTokens
		totalOut += t.OutputTokens
		sb.WriteString(fmt.Sprintf("  Turn %d: %s  in=%d out=%d  $%.4f\n",
			i+1, t.Model, t.InputTokens, t.OutputTokens, t.Cost))
	}
	sb.WriteString(fmt.Sprintf("\nTotal tokens: %d input + %d output = %d",
		totalIn, totalOut, totalIn+totalOut))

	return sb.String()
}

// FormatCost returns a compact cost string like "$0.12" for status bar display.
func (ct *CostTracker) FormatCost() string {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	if ct.sessionCost < 0.01 {
		return fmt.Sprintf("$%.4f", ct.sessionCost)
	}
	return fmt.Sprintf("$%.2f", ct.sessionCost)
}

// calculateCost computes the dollar cost for a turn. Must be called with lock held.
func (ct *CostTracker) calculateCost(model string, inputTokens, outputTokens int) float64 {
	p, ok := ct.pricing[model]
	if !ok {
		// Try prefix matching for versioned model names (e.g. "gpt-4o-2024-08-06")
		for name, pricing := range ct.pricing {
			if strings.HasPrefix(model, name) {
				p = pricing
				ok = true
				break
			}
		}
	}
	if !ok {
		return 0 // unknown model, no pricing
	}
	return (float64(inputTokens) * p.InputPerMillion / 1_000_000) +
		(float64(outputTokens) * p.OutputPerMillion / 1_000_000)
}
