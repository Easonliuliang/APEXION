package session

import (
	"encoding/json"
	"fmt"

	"github.com/aictl/aictl/internal/provider"
)

// Turn is a logical conversation round: user(text) + assistant(text+tool_use) + user(tool_results) + ...
// A turn starts with a user text message and includes all subsequent assistant/tool exchanges
// until the next user text message.
type Turn struct {
	Messages []provider.Message
	Tokens   int  // estimated tokens for this turn
	Complete bool // true if the turn ends with an assistant message without tool_use
}

// SplitTurns splits a flat message list into logical turns.
// Rules:
//   - A new turn starts at a user message with text content (not tool_result),
//     but only when the previous assistant message has no tool_use.
//   - All assistant↔tool_result chains are kept within the same turn.
//   - An incomplete turn (ending with tool_use awaiting results) is preserved.
func SplitTurns(messages []provider.Message) []Turn {
	if len(messages) == 0 {
		return nil
	}

	var turns []Turn
	currentStart := 0

	for i := 1; i < len(messages); i++ {
		msg := messages[i]
		if msg.Role != provider.RoleUser {
			continue
		}
		// Check if this user message has text content (not just tool_results).
		if !hasTextContent(msg) {
			continue
		}
		// Check if the previous message is an assistant without tool_use.
		prev := messages[i-1]
		if prev.Role == provider.RoleAssistant && !hasToolUse(prev) {
			// Cut here: messages[currentStart:i] form one turn.
			turn := makeTurn(messages[currentStart:i])
			turn.Complete = true
			turns = append(turns, turn)
			currentStart = i
		}
	}

	// Remaining messages form the last turn.
	if currentStart < len(messages) {
		turn := makeTurn(messages[currentStart:])
		// Check if it's complete (ends with assistant without tool_use).
		last := messages[len(messages)-1]
		turn.Complete = last.Role == provider.RoleAssistant && !hasToolUse(last)
		turns = append(turns, turn)
	}

	return turns
}

func makeTurn(msgs []provider.Message) Turn {
	return Turn{
		Messages: msgs,
		Tokens:   estimateMessagesTokens(msgs),
	}
}

func hasTextContent(msg provider.Message) bool {
	for _, c := range msg.Content {
		if c.Type == provider.ContentTypeText && c.Text != "" {
			return true
		}
	}
	return false
}

func hasToolUse(msg provider.Message) bool {
	for _, c := range msg.Content {
		if c.Type == provider.ContentTypeToolUse {
			return true
		}
	}
	return false
}

// CompactHistory generates a compacted copy of messages suitable for sending to the LLM.
// It does NOT modify the original messages slice.
// Three phases are applied in order:
//
//	Phase A: Summary injection (if summary is non-empty)
//	Phase B: Observation masking (replace old tool_result content with placeholder)
//	Phase C: Turn-level trimming (remove oldest turns if still over maxTokens)
func CompactHistory(messages []provider.Message, maxTokens int, summary string) []provider.Message {
	if len(messages) == 0 {
		return messages
	}

	// Phase A: Summary injection
	var result []provider.Message
	if summary != "" {
		result = append(result, provider.Message{
			Role: provider.RoleUser,
			Content: []provider.Content{{
				Type: provider.ContentTypeText,
				Text: "[Previous conversation summary]\n\n" + summary,
			}},
		})
	}

	// Phase B: Observation masking
	// Count total tool_result blocks to determine which ones to mask.
	var toolResultCount int
	for _, msg := range messages {
		for _, c := range msg.Content {
			if c.Type == provider.ContentTypeToolResult {
				toolResultCount++
			}
		}
	}

	const keepRecentToolResults = 10
	// We want to keep the last N tool_results intact, mask older ones.
	maskBefore := toolResultCount - keepRecentToolResults
	if maskBefore < 0 {
		maskBefore = 0
	}

	toolResultIdx := 0
	for _, msg := range messages {
		needsCopy := false
		for _, c := range msg.Content {
			if c.Type == provider.ContentTypeToolResult && toolResultIdx < maskBefore && len(c.ToolResult) > 0 {
				needsCopy = true
				break
			}
			if c.Type == provider.ContentTypeToolResult {
				toolResultIdx++
			}
		}

		if !needsCopy {
			// Reset the counter properly for the counting we did above.
			result = append(result, msg)
			continue
		}

		// Create a masked copy of this message.
		maskedMsg := provider.Message{Role: msg.Role}
		maskedMsg.Content = make([]provider.Content, len(msg.Content))
		copy(maskedMsg.Content, msg.Content)
		result = append(result, maskedMsg)
	}

	// Redo masking properly with a single pass.
	result = result[:0]
	if summary != "" {
		result = append(result, provider.Message{
			Role: provider.RoleUser,
			Content: []provider.Content{{
				Type: provider.ContentTypeText,
				Text: "[Previous conversation summary]\n\n" + summary,
			}},
		})
	}

	toolResultIdx = 0
	for _, msg := range messages {
		needsMasking := false
		for _, c := range msg.Content {
			if c.Type == provider.ContentTypeToolResult {
				if toolResultIdx < maskBefore && len(c.ToolResult) > 0 {
					needsMasking = true
				}
			}
		}

		if !needsMasking {
			result = append(result, msg)
			// Count tool_results in this message.
			for _, c := range msg.Content {
				if c.Type == provider.ContentTypeToolResult {
					toolResultIdx++
				}
			}
			continue
		}

		// Deep copy this message with masked tool_results.
		newMsg := provider.Message{
			Role:    msg.Role,
			Content: make([]provider.Content, len(msg.Content)),
		}
		for j, c := range msg.Content {
			if c.Type == provider.ContentTypeToolResult && toolResultIdx < maskBefore && len(c.ToolResult) > 0 {
				newMsg.Content[j] = provider.Content{
					Type:       c.Type,
					ToolUseID:  c.ToolUseID,
					ToolResult: fmt.Sprintf("[Output omitted: %d chars]", len(c.ToolResult)),
					IsError:    c.IsError,
				}
				toolResultIdx++
			} else {
				newMsg.Content[j] = c
				if c.Type == provider.ContentTypeToolResult {
					toolResultIdx++
				}
			}
		}
		result = append(result, newMsg)
	}

	// Phase C: Turn-level trimming (if still over maxTokens)
	if estimateMessagesTokens(result) > maxTokens {
		turns := SplitTurns(result)
		const minKeepTurns = 5

		// If summary was injected, the first "turn" contains the summary message.
		// We should preserve it.
		summaryTurns := 0
		if summary != "" && len(turns) > 0 {
			summaryTurns = 1
		}

		// Remove oldest turns (after summary) until under budget or at minimum.
		for len(turns) > minKeepTurns+summaryTurns && estimateTurnsTokens(turns) > maxTokens {
			// Remove the oldest non-summary turn.
			if summaryTurns > 0 {
				turns = append(turns[:1], turns[2:]...)
			} else {
				turns = turns[1:]
			}
		}

		result = flattenTurns(turns)
	}

	return result
}

// TruncateSession truncates the session messages after summarization.
// It keeps only the most recent keepTurns complete turns.
// Returns a deep-copied slice so old message data can be GC'd.
func TruncateSession(messages []provider.Message, keepTurns int) []provider.Message {
	turns := SplitTurns(messages)
	if len(turns) <= keepTurns {
		return messages
	}

	// Keep only the last keepTurns turns.
	kept := turns[len(turns)-keepTurns:]
	var msgs []provider.Message
	for _, t := range kept {
		msgs = append(msgs, t.Messages...)
	}

	// Deep copy to release old heap data.
	return cloneMessages(msgs)
}

// cloneMessages deep-copies a message slice so GC can reclaim old string/[]byte heap data.
// Go struct copy is shallow — string and []byte heap pointers still reference original data.
// Explicit reallocation is needed to let GC collect old data.
func cloneMessages(msgs []provider.Message) []provider.Message {
	result := make([]provider.Message, len(msgs))
	for i, msg := range msgs {
		result[i] = provider.Message{
			Role:    provider.Role([]byte(msg.Role)),
			Content: make([]provider.Content, len(msg.Content)),
		}
		for j, c := range msg.Content {
			result[i].Content[j] = provider.Content{
				Type:       c.Type,
				Text:       string([]byte(c.Text)),
				ToolUseID:  string([]byte(c.ToolUseID)),
				ToolName:   string([]byte(c.ToolName)),
				ToolResult: string([]byte(c.ToolResult)),
				IsError:    c.IsError,
			}
			if len(c.ToolInput) > 0 {
				result[i].Content[j].ToolInput = append(json.RawMessage{}, c.ToolInput...)
			}
		}
	}
	return result
}

func estimateMessagesTokens(messages []provider.Message) int {
	total := 0
	for _, msg := range messages {
		for _, c := range msg.Content {
			total += len(c.Text)
			total += len(c.ToolResult)
			total += len(c.ToolInput)
		}
	}
	return total / 4
}

func estimateTurnsTokens(turns []Turn) int {
	total := 0
	for _, t := range turns {
		total += t.Tokens
	}
	return total
}

func flattenTurns(turns []Turn) []provider.Message {
	var result []provider.Message
	for _, t := range turns {
		result = append(result, t.Messages...)
	}
	return result
}
