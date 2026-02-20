package session

// TokenBudget manages context window allocation for a model.
type TokenBudget struct {
	ContextWindow int // total context window of the model
	SystemPrompt  int // estimated system prompt tokens
	OutputReserve int // reserved for output, default 8192
	HistoryMax    int // max tokens for conversation history
}

// NewTokenBudget creates a TokenBudget based on the model's context window
// and estimated system prompt size.
// HistoryMax is set to 65% of ContextWindow (aligned with DESIGN.md).
func NewTokenBudget(contextWindow, systemPromptTokens int) *TokenBudget {
	outputReserve := 8192
	historyMax := contextWindow * 65 / 100
	return &TokenBudget{
		ContextWindow: contextWindow,
		SystemPrompt:  systemPromptTokens,
		OutputReserve: outputReserve,
		HistoryMax:    historyMax,
	}
}

// CompactThreshold returns the prompt token count at which compaction should trigger.
// Set at 80% of ContextWindow.
func (b *TokenBudget) CompactThreshold() int {
	return b.ContextWindow * 80 / 100
}
