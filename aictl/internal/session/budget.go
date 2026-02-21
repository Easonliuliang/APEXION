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

// GentleThreshold returns the token count at which gentle compaction (tool output masking)
// should trigger. Set at 70% of ContextWindow.
func (b *TokenBudget) GentleThreshold() int {
	return b.ContextWindow * 70 / 100
}

// CompactThreshold returns the prompt token count at which full compaction
// (summarization + truncation) should trigger. Set at 80% of ContextWindow.
func (b *TokenBudget) CompactThreshold() int {
	return b.ContextWindow * 80 / 100
}
