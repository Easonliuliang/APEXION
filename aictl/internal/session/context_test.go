package session

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aictl/aictl/internal/provider"
)

// Helper to build messages for tests.
func userText(text string) provider.Message {
	return provider.Message{
		Role: provider.RoleUser,
		Content: []provider.Content{{
			Type: provider.ContentTypeText,
			Text: text,
		}},
	}
}

func assistantText(text string) provider.Message {
	return provider.Message{
		Role: provider.RoleAssistant,
		Content: []provider.Content{{
			Type: provider.ContentTypeText,
			Text: text,
		}},
	}
}

func assistantWithToolUse(text, toolID, toolName string) provider.Message {
	contents := []provider.Content{}
	if text != "" {
		contents = append(contents, provider.Content{
			Type: provider.ContentTypeText,
			Text: text,
		})
	}
	contents = append(contents, provider.Content{
		Type:      provider.ContentTypeToolUse,
		ToolUseID: toolID,
		ToolName:  toolName,
		ToolInput: json.RawMessage(`{"path":"test.go"}`),
	})
	return provider.Message{Role: provider.RoleAssistant, Content: contents}
}

func toolResult(toolID, result string) provider.Message {
	return provider.Message{
		Role: provider.RoleUser,
		Content: []provider.Content{{
			Type:       provider.ContentTypeToolResult,
			ToolUseID:  toolID,
			ToolResult: result,
		}},
	}
}

func multiToolResult(pairs ...string) provider.Message {
	var contents []provider.Content
	for i := 0; i < len(pairs); i += 2 {
		contents = append(contents, provider.Content{
			Type:       provider.ContentTypeToolResult,
			ToolUseID:  pairs[i],
			ToolResult: pairs[i+1],
		})
	}
	return provider.Message{Role: provider.RoleUser, Content: contents}
}

// --- SplitTurns tests ---

func TestSplitTurns_Empty(t *testing.T) {
	turns := SplitTurns(nil)
	if turns != nil {
		t.Errorf("expected nil, got %v", turns)
	}
}

func TestSplitTurns_SimpleConversation(t *testing.T) {
	msgs := []provider.Message{
		userText("hello"),
		assistantText("hi"),
	}
	turns := SplitTurns(msgs)
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	if len(turns[0].Messages) != 2 {
		t.Errorf("expected 2 messages in turn, got %d", len(turns[0].Messages))
	}
	if !turns[0].Complete {
		t.Error("expected turn to be complete")
	}
}

func TestSplitTurns_TwoRounds(t *testing.T) {
	msgs := []provider.Message{
		userText("first"),
		assistantText("response1"),
		userText("second"),
		assistantText("response2"),
	}
	turns := SplitTurns(msgs)
	if len(turns) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(turns))
	}
	if len(turns[0].Messages) != 2 {
		t.Errorf("turn 0: expected 2 messages, got %d", len(turns[0].Messages))
	}
	if len(turns[1].Messages) != 2 {
		t.Errorf("turn 1: expected 2 messages, got %d", len(turns[1].Messages))
	}
}

func TestSplitTurns_WithToolUse(t *testing.T) {
	// One turn with tool use chain.
	msgs := []provider.Message{
		userText("check git"),
		assistantWithToolUse("checking...", "t1", "git_status"),
		toolResult("t1", "clean"),
		assistantText("repo is clean"),
	}
	turns := SplitTurns(msgs)
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	if len(turns[0].Messages) != 4 {
		t.Errorf("expected 4 messages, got %d", len(turns[0].Messages))
	}
	if !turns[0].Complete {
		t.Error("expected turn to be complete")
	}
}

func TestSplitTurns_MultiToolChain(t *testing.T) {
	// One turn with multiple tool use rounds.
	msgs := []provider.Message{
		userText("fix the bug"),
		assistantWithToolUse("let me check", "t1", "read_file"),
		toolResult("t1", "file contents..."),
		assistantWithToolUse("I see the issue", "t2", "edit_file"),
		toolResult("t2", "edit applied"),
		assistantText("Done fixing!"),
	}
	turns := SplitTurns(msgs)
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn (multi-tool chain), got %d", len(turns))
	}
	if len(turns[0].Messages) != 6 {
		t.Errorf("expected 6 messages, got %d", len(turns[0].Messages))
	}
}

func TestSplitTurns_IncompleteTurn(t *testing.T) {
	// Turn ending with tool_use (no result yet).
	msgs := []provider.Message{
		userText("do something"),
		assistantWithToolUse("running", "t1", "bash"),
	}
	turns := SplitTurns(msgs)
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	if turns[0].Complete {
		t.Error("expected incomplete turn")
	}
}

func TestSplitTurns_ToolUseFollowedByNewUser(t *testing.T) {
	// Turn 1 with tool chain, then turn 2 new user input.
	msgs := []provider.Message{
		userText("first task"),
		assistantWithToolUse("checking", "t1", "read_file"),
		toolResult("t1", "contents"),
		assistantText("done with first"),
		userText("second task"),
		assistantText("ok second done"),
	}
	turns := SplitTurns(msgs)
	if len(turns) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(turns))
	}
	if len(turns[0].Messages) != 4 {
		t.Errorf("turn 0: expected 4 messages, got %d", len(turns[0].Messages))
	}
	if len(turns[1].Messages) != 2 {
		t.Errorf("turn 1: expected 2 messages, got %d", len(turns[1].Messages))
	}
}

// --- CompactHistory tests ---

func TestCompactHistory_Empty(t *testing.T) {
	result := CompactHistory(nil, 100000, "")
	if len(result) != 0 {
		t.Errorf("expected empty, got %d messages", len(result))
	}
}

func TestCompactHistory_SummaryInjection(t *testing.T) {
	msgs := []provider.Message{
		userText("hello"),
		assistantText("hi"),
	}
	result := CompactHistory(msgs, 100000, "previous context summary")
	if len(result) != 3 {
		t.Fatalf("expected 3 messages (summary + 2 original), got %d", len(result))
	}
	if result[0].Role != provider.RoleUser {
		t.Errorf("summary should be user role, got %s", result[0].Role)
	}
	if !strings.Contains(result[0].Content[0].Text, "[Previous conversation summary]") {
		t.Error("summary message should contain summary header")
	}
	if !strings.Contains(result[0].Content[0].Text, "previous context summary") {
		t.Error("summary message should contain the summary text")
	}
}

func TestCompactHistory_NoSummary(t *testing.T) {
	msgs := []provider.Message{
		userText("hello"),
		assistantText("hi"),
	}
	result := CompactHistory(msgs, 100000, "")
	if len(result) != 2 {
		t.Fatalf("expected 2 messages (no summary), got %d", len(result))
	}
}

func TestCompactHistory_ObservationMasking(t *testing.T) {
	// Create 15 tool results, older ones should be masked.
	var msgs []provider.Message
	for i := 0; i < 15; i++ {
		msgs = append(msgs,
			userText("do something"),
			assistantWithToolUse("ok", "t"+string(rune('a'+i)), "read_file"),
			toolResult("t"+string(rune('a'+i)), strings.Repeat("x", 1000)),
			assistantText("done"),
		)
	}

	// Add a final user message for context.
	msgs = append(msgs, userText("what now?"))

	result := CompactHistory(msgs, 1000000, "") // large maxTokens to avoid trimming

	// Count masked vs unmasked tool results.
	masked := 0
	unmasked := 0
	for _, msg := range result {
		for _, c := range msg.Content {
			if c.Type == provider.ContentTypeToolResult {
				if strings.Contains(c.ToolResult, "[Output omitted:") {
					masked++
				} else {
					unmasked++
				}
			}
		}
	}

	if unmasked != 10 {
		t.Errorf("expected 10 unmasked tool results, got %d", unmasked)
	}
	if masked != 5 {
		t.Errorf("expected 5 masked tool results, got %d", masked)
	}
}

func TestCompactHistory_DoesNotMutateOriginal(t *testing.T) {
	originalResult := strings.Repeat("x", 1000)
	msgs := []provider.Message{
		userText("task"),
		assistantWithToolUse("checking", "t1", "read_file"),
		toolResult("t1", originalResult),
		assistantText("done"),
	}

	// Create many more to trigger masking of t1.
	for i := 0; i < 15; i++ {
		id := "tx" + string(rune('a'+i))
		msgs = append(msgs,
			userText("more"),
			assistantWithToolUse("ok", id, "read_file"),
			toolResult(id, strings.Repeat("y", 500)),
			assistantText("done"),
		)
	}

	_ = CompactHistory(msgs, 1000000, "")

	// Original messages should not be modified.
	if msgs[2].Content[0].ToolResult != originalResult {
		t.Error("original message was mutated by CompactHistory")
	}
}

func TestCompactHistory_TurnTrimming(t *testing.T) {
	// Create many turns that exceed maxTokens.
	var msgs []provider.Message
	for i := 0; i < 20; i++ {
		msgs = append(msgs,
			userText(strings.Repeat("u", 2000)),
			assistantText(strings.Repeat("a", 2000)),
		)
	}

	// Set a small maxTokens to force trimming.
	result := CompactHistory(msgs, 5000, "")

	// Should have fewer messages than original.
	if len(result) >= len(msgs) {
		t.Errorf("expected trimming, got %d messages (original %d)", len(result), len(msgs))
	}

	// Should have at least 5 turns (10 messages).
	if len(result) < 10 {
		t.Errorf("expected at least 10 messages (5 turns), got %d", len(result))
	}
}

func TestCompactHistory_PreservesToolUsePairs(t *testing.T) {
	// After compaction, every tool_use should have a matching tool_result.
	var msgs []provider.Message
	for i := 0; i < 15; i++ {
		id := "t" + string(rune('a'+i))
		msgs = append(msgs,
			userText("task"),
			assistantWithToolUse("ok", id, "read_file"),
			toolResult(id, strings.Repeat("x", 500)),
			assistantText("done"),
		)
	}

	result := CompactHistory(msgs, 5000, "")

	// Collect tool_use IDs and tool_result IDs.
	useIDs := map[string]bool{}
	resultIDs := map[string]bool{}
	for _, msg := range result {
		for _, c := range msg.Content {
			if c.Type == provider.ContentTypeToolUse {
				useIDs[c.ToolUseID] = true
			}
			if c.Type == provider.ContentTypeToolResult {
				resultIDs[c.ToolUseID] = true
			}
		}
	}

	// Every tool_use should have a result.
	for id := range useIDs {
		if !resultIDs[id] {
			t.Errorf("tool_use %s has no matching tool_result after compaction", id)
		}
	}
	// Every tool_result should have a use.
	for id := range resultIDs {
		if !useIDs[id] {
			t.Errorf("tool_result %s has no matching tool_use after compaction", id)
		}
	}
}

// --- TruncateSession tests ---

func TestTruncateSession_NoTruncation(t *testing.T) {
	msgs := []provider.Message{
		userText("hello"),
		assistantText("hi"),
	}
	result := TruncateSession(msgs, 10)
	if len(result) != 2 {
		t.Errorf("expected 2 messages (no truncation), got %d", len(result))
	}
}

func TestTruncateSession_Truncates(t *testing.T) {
	var msgs []provider.Message
	for i := 0; i < 20; i++ {
		msgs = append(msgs,
			userText("question"),
			assistantText("answer"),
		)
	}

	result := TruncateSession(msgs, 5)
	// 5 turns * 2 messages = 10 messages.
	if len(result) != 10 {
		t.Errorf("expected 10 messages (5 turns), got %d", len(result))
	}

	// Verify the kept messages are from the last 5 turns.
	if result[0].Content[0].Text != "question" {
		t.Error("first kept message should be user text")
	}
}

func TestTruncateSession_DeepCopy(t *testing.T) {
	original := strings.Repeat("data", 1000)
	msgs := []provider.Message{
		userText("old"),
		assistantText("old response"),
		userText("keep1"),
		assistantText("keep1 response"),
		userText("keep2"),
		assistantText(original),
	}

	result := TruncateSession(msgs, 2)

	// Modify original to verify deep copy.
	msgs[5].Content[0].Text = "MODIFIED"

	// Result should not be affected.
	found := false
	for _, msg := range result {
		for _, c := range msg.Content {
			if c.Text == original {
				found = true
			}
			if c.Text == "MODIFIED" {
				t.Error("deep copy failed: result was affected by original mutation")
			}
		}
	}
	if !found {
		t.Error("expected to find original text in deep-copied result")
	}
}

func TestTruncateSession_PreservesToolChains(t *testing.T) {
	msgs := []provider.Message{
		// Turn 1 (old, will be truncated).
		userText("old task"),
		assistantWithToolUse("checking", "t1", "read_file"),
		toolResult("t1", "old content"),
		assistantText("done old"),
		// Turn 2 (keep).
		userText("new task"),
		assistantWithToolUse("checking", "t2", "read_file"),
		toolResult("t2", "new content"),
		assistantText("done new"),
	}

	result := TruncateSession(msgs, 1)

	// Should keep only the last turn (4 messages).
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}

	// Verify tool_use/tool_result pair is intact.
	hasUse := false
	hasResult := false
	for _, msg := range result {
		for _, c := range msg.Content {
			if c.Type == provider.ContentTypeToolUse && c.ToolUseID == "t2" {
				hasUse = true
			}
			if c.Type == provider.ContentTypeToolResult && c.ToolUseID == "t2" {
				hasResult = true
			}
		}
	}
	if !hasUse || !hasResult {
		t.Error("tool_use/tool_result pair for t2 should be preserved")
	}
}

// --- Budget tests ---

func TestNewTokenBudget(t *testing.T) {
	b := NewTokenBudget(200000, 5000)
	if b.ContextWindow != 200000 {
		t.Errorf("expected context window 200000, got %d", b.ContextWindow)
	}
	if b.HistoryMax != 130000 {
		t.Errorf("expected history max 130000, got %d", b.HistoryMax)
	}
	if b.OutputReserve != 8192 {
		t.Errorf("expected output reserve 8192, got %d", b.OutputReserve)
	}
	if b.CompactThreshold() != 160000 {
		t.Errorf("expected compact threshold 160000, got %d", b.CompactThreshold())
	}
}
