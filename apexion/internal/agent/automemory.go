package agent

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/apexion-ai/apexion/internal/provider"
	"github.com/apexion-ai/apexion/internal/session"
)

const autoMemoryPrompt = `Analyze the conversation and extract 0-3 important facts worth remembering for future sessions.
Focus on:
- User preferences (coding style, naming conventions, tooling choices)
- Project patterns (architecture decisions, file conventions)
- Important corrections the user made

Output a JSON array: [{"content": "...", "tags": ["preference"|"project"|"correction"]}]
Return empty array [] if nothing worth remembering.
Only output the JSON array, nothing else.`

// AutoMemoryExtractor analyzes conversation history and extracts
// valuable information to persist as cross-session memories.
type AutoMemoryExtractor struct {
	provider provider.Provider
	store    session.MemoryStore
	model    string
}

// NewAutoMemoryExtractor creates an extractor.
// model can be empty to use the provider's default.
func NewAutoMemoryExtractor(p provider.Provider, store session.MemoryStore, model string) *AutoMemoryExtractor {
	return &AutoMemoryExtractor{
		provider: p,
		store:    store,
		model:    model,
	}
}

// Extract analyzes the session's message history and stores extracted memories.
// Only processes if the session has more than 5 messages (short conversations
// rarely contain memorable information).
func (ame *AutoMemoryExtractor) Extract(ctx context.Context, messages []provider.Message, sessionID string) (int, error) {
	// Don't extract from short conversations.
	if len(messages) < 6 {
		return 0, nil
	}

	// Build a summary of the conversation for the LLM.
	// Only include text content from the last 20 messages to keep it manageable.
	summary := buildConversationSummary(messages, 20)
	if summary == "" {
		return 0, nil
	}

	// Call the LLM to extract memories.
	req := &provider.ChatRequest{
		Model: ame.model,
		Messages: []provider.Message{
			{
				Role: provider.RoleUser,
				Content: []provider.Content{{
					Type: provider.ContentTypeText,
					Text: "Here is a conversation between a user and an AI assistant:\n\n" +
						summary + "\n\n" + autoMemoryPrompt,
				}},
			},
		},
		SystemPrompt: "You extract factual information from conversations. Output only valid JSON.",
		MaxTokens:    1024,
	}

	events, err := ame.provider.Chat(ctx, req)
	if err != nil {
		return 0, err
	}

	var response strings.Builder
	for evt := range events {
		if evt.Type == provider.EventTextDelta {
			response.WriteString(evt.TextDelta)
		}
	}

	// Parse the JSON response.
	type memoryEntry struct {
		Content string   `json:"content"`
		Tags    []string `json:"tags"`
	}

	var entries []memoryEntry
	raw := strings.TrimSpace(response.String())
	// Try to extract JSON array even if surrounded by markdown code fences.
	if idx := strings.Index(raw, "["); idx >= 0 {
		if end := strings.LastIndex(raw, "]"); end > idx {
			raw = raw[idx : end+1]
		}
	}
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return 0, nil // parsing failed, skip silently
	}

	// Store each extracted memory, deduplicating against existing memories.
	existing, _ := ame.store.List(50)
	added := 0
	for _, entry := range entries {
		if entry.Content == "" {
			continue
		}
		// Simple deduplication: skip if content is a substring of any existing memory
		// or any existing memory is a substring of this content.
		if isDuplicate(entry.Content, existing) {
			continue
		}
		_, err := ame.store.Add(entry.Content, entry.Tags, "auto", sessionID)
		if err != nil {
			continue
		}
		added++
	}

	return added, nil
}

// isDuplicate checks if content is similar to any existing memory.
func isDuplicate(content string, existing []session.Memory) bool {
	lower := strings.ToLower(content)
	for _, m := range existing {
		existLower := strings.ToLower(m.Content)
		if lower == existLower {
			return true
		}
		if strings.Contains(lower, existLower) || strings.Contains(existLower, lower) {
			return true
		}
	}
	return false
}

// buildConversationSummary extracts text content from recent messages.
func buildConversationSummary(messages []provider.Message, maxMessages int) string {
	start := 0
	if len(messages) > maxMessages {
		start = len(messages) - maxMessages
	}

	var sb strings.Builder
	for _, msg := range messages[start:] {
		role := "User"
		if msg.Role == provider.RoleAssistant {
			role = "Assistant"
		}
		for _, c := range msg.Content {
			if c.Type == provider.ContentTypeText && c.Text != "" {
				text := c.Text
				if len(text) > 500 {
					text = text[:500] + "..."
				}
				sb.WriteString(role + ": " + text + "\n\n")
			}
		}
	}
	return sb.String()
}
