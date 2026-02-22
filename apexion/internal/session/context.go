package session

import (
	"encoding/json"
	"fmt"

	"github.com/apexion-ai/apexion/internal/provider"
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
			// Note: ImageData is intentionally NOT cloned during compaction.
			// Images are stripped to save memory.
		}
	}
	return result
}

// StripImageData removes image data from messages to save tokens.
// Replaces image content blocks with text placeholders.
func StripImageData(messages []provider.Message) []provider.Message {
	result := make([]provider.Message, 0, len(messages))
	for _, msg := range messages {
		needsStrip := false
		for _, c := range msg.Content {
			if c.Type == provider.ContentTypeImage && c.ImageData != "" {
				needsStrip = true
				break
			}
		}

		if !needsStrip {
			result = append(result, msg)
			continue
		}

		// Deep copy this message, stripping image data.
		newMsg := provider.Message{
			Role:    msg.Role,
			Content: make([]provider.Content, 0, len(msg.Content)),
		}
		for _, c := range msg.Content {
			if c.Type == provider.ContentTypeImage {
				// Replace image with text placeholder.
				newMsg.Content = append(newMsg.Content, provider.Content{
					Type: provider.ContentTypeText,
					Text: "[Image omitted during compaction]",
				})
			} else {
				newMsg.Content = append(newMsg.Content, c)
			}
		}
		result = append(result, newMsg)
	}
	return result
}

// MaskOldToolOutputs replaces old tool_result content with compact placeholders.
// Keeps the last keepRecent tool results intact.
// Returns a new message slice; the original is not modified.
func MaskOldToolOutputs(messages []provider.Message, keepRecent int) []provider.Message {
	// Count total tool_result blocks.
	var total int
	for _, msg := range messages {
		for _, c := range msg.Content {
			if c.Type == provider.ContentTypeToolResult {
				total++
			}
		}
	}

	maskBefore := total - keepRecent
	if maskBefore <= 0 {
		return messages // nothing to mask
	}

	// Single pass: copy messages, masking old tool results.
	result := make([]provider.Message, 0, len(messages))
	idx := 0
	for _, msg := range messages {
		// Check if any tool_result in this message needs masking.
		needsMasking := false
		for _, c := range msg.Content {
			if c.Type == provider.ContentTypeToolResult && idx < maskBefore && len(c.ToolResult) > 0 {
				needsMasking = true
			}
			if c.Type == provider.ContentTypeToolResult {
				idx++
			}
		}

		if !needsMasking {
			result = append(result, msg)
			continue
		}

		// Rewind idx to start of this message for the masking pass.
		idx -= countToolResults(msg)

		newMsg := provider.Message{
			Role:    msg.Role,
			Content: make([]provider.Content, len(msg.Content)),
		}
		for j, c := range msg.Content {
			if c.Type == provider.ContentTypeToolResult && idx < maskBefore && len(c.ToolResult) > 0 {
				newMsg.Content[j] = provider.Content{
					Type:       c.Type,
					ToolUseID:  c.ToolUseID,
					ToolResult: fmt.Sprintf("[Output omitted: %d chars]", len(c.ToolResult)),
					IsError:    c.IsError,
				}
			} else {
				newMsg.Content[j] = c
			}
			if c.Type == provider.ContentTypeToolResult {
				idx++
			}
		}
		result = append(result, newMsg)
	}

	return result
}

// ── Tool importance levels for smart compaction ──────────────────────────────

// ToolImportance represents how important a tool's output is for context.
type ToolImportance int

const (
	ToolImportanceLow    ToolImportance = iota // easily re-fetched: glob, grep, list_dir, web_search, todo_read, git_log
	ToolImportanceMedium                       // moderately important: git_status, git_diff, git_branch
	ToolImportanceHigh                         // critical: read_file, bash, edit_file, write_file, web_fetch, task
)

// toolImportanceMap maps tool names to their importance levels.
var toolImportanceMap = map[string]ToolImportance{
	// Low: search/discovery tools — output can be re-generated easily.
	"glob":       ToolImportanceLow,
	"grep":       ToolImportanceLow,
	"list_dir":   ToolImportanceLow,
	"web_search": ToolImportanceLow,
	"todo_read":  ToolImportanceLow,
	"git_log":    ToolImportanceLow,

	// Medium: git state tools — useful context but refreshable.
	"git_status": ToolImportanceMedium,
	"git_diff":   ToolImportanceMedium,
	"git_branch": ToolImportanceMedium,

	// High: everything else — file contents, command output, modifications.
	"read_file":  ToolImportanceHigh,
	"bash":       ToolImportanceHigh,
	"edit_file":  ToolImportanceHigh,
	"write_file": ToolImportanceHigh,
	"web_fetch":  ToolImportanceHigh,
	"task":       ToolImportanceHigh,
	"question":   ToolImportanceHigh,
	"git_commit": ToolImportanceHigh,
	"git_push":   ToolImportanceHigh,
	"todo_write": ToolImportanceHigh,
}

// getToolImportance returns the importance level of a tool.
func getToolImportance(toolName string) ToolImportance {
	if imp, ok := toolImportanceMap[toolName]; ok {
		return imp
	}
	return ToolImportanceHigh // unknown tools default to high
}

// MaskOldToolOutputsSmart replaces old tool_result content with compact placeholders,
// only masking tools at or below the specified importance threshold.
// Error results (IsError=true) are never masked.
// Keeps the last keepRecent tool results intact regardless of importance.
func MaskOldToolOutputsSmart(messages []provider.Message, keepRecent int, maxImportance ToolImportance) []provider.Message {
	// Count total tool_result blocks and identify which ones to potentially mask.
	type toolResultInfo struct {
		msgIdx  int
		contIdx int
		name    string
	}
	var allToolResults []toolResultInfo
	for i, msg := range messages {
		for j, c := range msg.Content {
			if c.Type == provider.ContentTypeToolResult {
				// Find the tool name from the preceding tool_use block.
				name := findToolNameForResult(messages, i, c.ToolUseID)
				allToolResults = append(allToolResults, toolResultInfo{
					msgIdx: i, contIdx: j, name: name,
				})
			}
		}
	}

	total := len(allToolResults)
	if total <= keepRecent {
		return messages // nothing to mask
	}

	// Build a set of (msgIdx, contIdx) to mask.
	maskSet := make(map[[2]int]string) // key → placeholder text
	candidateCount := total - keepRecent
	for k := 0; k < candidateCount; k++ {
		tr := allToolResults[k]
		msg := messages[tr.msgIdx]
		c := msg.Content[tr.contIdx]

		// Never mask error results.
		if c.IsError {
			continue
		}
		// Only mask if tool importance is at or below threshold.
		if getToolImportance(tr.name) > maxImportance {
			continue
		}
		// Only mask if there's actual content to mask.
		if len(c.ToolResult) == 0 {
			continue
		}

		placeholder := fmt.Sprintf("[%s output omitted: %d chars]", tr.name, len(c.ToolResult))
		maskSet[[2]int{tr.msgIdx, tr.contIdx}] = placeholder
	}

	if len(maskSet) == 0 {
		return messages
	}

	// Build new messages with masked content.
	result := make([]provider.Message, 0, len(messages))
	for i, msg := range messages {
		needsMasking := false
		for j := range msg.Content {
			if _, ok := maskSet[[2]int{i, j}]; ok {
				needsMasking = true
				break
			}
		}

		if !needsMasking {
			result = append(result, msg)
			continue
		}

		newMsg := provider.Message{
			Role:    msg.Role,
			Content: make([]provider.Content, len(msg.Content)),
		}
		for j, c := range msg.Content {
			if placeholder, ok := maskSet[[2]int{i, j}]; ok {
				newMsg.Content[j] = provider.Content{
					Type:       c.Type,
					ToolUseID:  c.ToolUseID,
					ToolResult: placeholder,
					IsError:    c.IsError,
				}
			} else {
				newMsg.Content[j] = c
			}
		}
		result = append(result, newMsg)
	}

	return result
}

// findToolNameForResult finds the tool name associated with a tool_result by its ToolUseID.
func findToolNameForResult(messages []provider.Message, beforeMsgIdx int, toolUseID string) string {
	// Search backwards for the matching tool_use block.
	for i := beforeMsgIdx; i >= 0; i-- {
		for _, c := range messages[i].Content {
			if c.Type == provider.ContentTypeToolUse && c.ToolUseID == toolUseID {
				return c.ToolName
			}
		}
	}
	return ""
}

// countToolResults counts tool_result blocks in a message.
func countToolResults(msg provider.Message) int {
	n := 0
	for _, c := range msg.Content {
		if c.Type == provider.ContentTypeToolResult {
			n++
		}
	}
	return n
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
