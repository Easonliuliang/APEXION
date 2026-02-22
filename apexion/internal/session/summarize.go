package session

import (
	"context"
	"fmt"
	"strings"

	"github.com/apexion-ai/apexion/internal/provider"
)

// Summarizer generates conversation summaries for context compaction.
type Summarizer interface {
	// Summarize generates a summary. previousSummary may be empty (first compaction).
	// Iterative: old summary + current messages → new combined summary.
	Summarize(ctx context.Context, previousSummary string, messages []provider.Message) (string, error)
}

// LLMSummarizer calls an LLM to generate summaries.
type LLMSummarizer struct {
	Provider provider.Provider
	Model    string // optional: use a cheaper model (e.g. haiku). Empty = provider default.
}

const summarizePrompt = `Summarize the conversation so far for continuity. Include:
- The user's original task and intent
- Key decisions made and rationale
- Current progress and files being worked on
- Important code changes, function names, file paths
- Remaining steps or unresolved issues
Be concise but thorough. Max 2000 tokens.`

func (s *LLMSummarizer) Summarize(ctx context.Context, previousSummary string, messages []provider.Message) (string, error) {
	// Build the summarization prompt.
	var prompt strings.Builder
	if previousSummary != "" {
		fmt.Fprintf(&prompt, "Previous conversation summary:\n%s\n\nNow summarize the above context together with the recent conversation:\n\n", previousSummary)
	}
	prompt.WriteString(summarizePrompt)

	// Build messages for the summarizer: the conversation + the summarize instruction.
	var summarizerMsgs []provider.Message

	// Include conversation messages as context.
	summarizerMsgs = append(summarizerMsgs, messages...)

	// Add the summarize instruction as a user message.
	summarizerMsgs = append(summarizerMsgs, provider.Message{
		Role: provider.RoleUser,
		Content: []provider.Content{{
			Type: provider.ContentTypeText,
			Text: prompt.String(),
		}},
	})

	model := s.Model
	if model == "" {
		model = s.Provider.DefaultModel()
	}

	req := &provider.ChatRequest{
		Model:        model,
		Messages:     summarizerMsgs,
		SystemPrompt: "You are a conversation summarizer. Produce a concise, structured summary of the conversation.",
		MaxTokens:    2048,
	}

	events, err := s.Provider.Chat(ctx, req)
	if err != nil {
		return "", fmt.Errorf("summarize LLM call failed: %w", err)
	}

	var result strings.Builder
	for event := range events {
		switch event.Type {
		case provider.EventTextDelta:
			result.WriteString(event.TextDelta)
		case provider.EventError:
			return "", fmt.Errorf("summarize stream error: %w", event.Error)
		}
	}

	summary := strings.TrimSpace(result.String())
	if summary == "" {
		return "", fmt.Errorf("summarizer returned empty summary")
	}
	return summary, nil
}
