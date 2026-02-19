package session

import "github.com/aictl/aictl/internal/provider"

// TrimHistory trims messages when estimated tokens exceed 80% of maxTokens.
// It preserves the most recent 6 messages and removes the oldest ones first.
func TrimHistory(messages []provider.Message, maxTokens int) []provider.Message {
	if len(messages) == 0 {
		return messages
	}

	threshold := maxTokens * 80 / 100
	if estimateMessagesTokens(messages) <= threshold {
		return messages
	}

	const keepRecent = 6
	if len(messages) <= keepRecent {
		return messages
	}

	// Remove from the front (oldest) until under threshold or only keepRecent remain.
	for len(messages) > keepRecent && estimateMessagesTokens(messages) > threshold {
		messages = messages[1:]
	}
	return messages
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
