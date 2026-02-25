package agent

import (
	"testing"

	"github.com/apexion-ai/apexion/internal/provider"
	"github.com/apexion-ai/apexion/internal/router"
	"github.com/apexion-ai/apexion/internal/session"
)

func TestShouldDisableWebFetchForImageTurn(t *testing.T) {
	msgs := []provider.Message{
		{
			Role: provider.RoleUser,
			Content: []provider.Content{
				{Type: provider.ContentTypeText, Text: "Please look at this image."},
				{Type: provider.ContentTypeImage, ImageData: "abc", ImageMediaType: "image/png"},
			},
		},
	}

	if !shouldDisableWebFetchForImageTurn(msgs) {
		t.Fatal("expected web_fetch to be disabled for image turn")
	}
}

func TestShouldDisableWebFetchForImageTurnFalseForToolResultRounds(t *testing.T) {
	msgs := []provider.Message{
		{
			Role: provider.RoleUser,
			Content: []provider.Content{
				{Type: provider.ContentTypeToolResult, ToolUseID: "t1", ToolResult: "ok"},
				{Type: provider.ContentTypeImage, ImageData: "abc", ImageMediaType: "image/png"},
			},
		},
	}

	if shouldDisableWebFetchForImageTurn(msgs) {
		t.Fatal("did not expect web_fetch to be disabled for tool_result rounds")
	}
}

func TestLatestUserTurnContext(t *testing.T) {
	msgs := []provider.Message{
		{
			Role: provider.RoleAssistant,
			Content: []provider.Content{
				{Type: provider.ContentTypeText, Text: "working on it"},
			},
		},
		{
			Role: provider.RoleUser,
			Content: []provider.Content{
				{Type: provider.ContentTypeText, Text: "Please inspect this module."},
				{Type: provider.ContentTypeImage, ImageData: "abc", ImageMediaType: "image/png"},
			},
		},
	}

	text, hasImage, hasToolResult := latestUserTurnContext(msgs)
	if text != "Please inspect this module." {
		t.Fatalf("unexpected text: %q", text)
	}
	if !hasImage {
		t.Fatal("expected hasImage=true")
	}
	if hasToolResult {
		t.Fatal("expected hasToolResult=false")
	}
}

func TestLatestUserTurnContextToolResultRound(t *testing.T) {
	msgs := []provider.Message{
		{
			Role: provider.RoleUser,
			Content: []provider.Content{
				{Type: provider.ContentTypeToolResult, ToolUseID: "t1", ToolResult: "ok"},
			},
		},
	}

	_, _, hasToolResult := latestUserTurnContext(msgs)
	if !hasToolResult {
		t.Fatal("expected hasToolResult=true")
	}
}

func TestBlockByFirstStepPolicy(t *testing.T) {
	a := &Agent{
		firstStepAllowed: map[string]bool{
			"repo_map":  true,
			"read_file": true,
		},
	}

	if msg, blocked := a.blockByFirstStepPolicy("repo_map"); blocked || msg != "" {
		t.Fatalf("expected repo_map allowed, got blocked=%v msg=%q", blocked, msg)
	}
	if msg, blocked := a.blockByFirstStepPolicy("glob"); !blocked || msg == "" {
		t.Fatalf("expected glob blocked, got blocked=%v msg=%q", blocked, msg)
	}
}

func TestFirstStepPolicyDebugRequiresToolRetry(t *testing.T) {
	a := &Agent{
		firstStepAllowed: make(map[string]bool),
	}
	a.setFirstStepPolicy(router.IntentDebug, []string{"symbol_nav", "grep"})

	if !a.shouldRetryNoToolFirstStep() {
		t.Fatal("expected debug first-step policy to require tool retry")
	}

	a.markFirstStepRetry()
	names := a.retryPrimaryTools()
	if len(names) != 2 || names[0] != "symbol_nav" || names[1] != "grep" {
		t.Fatalf("unexpected retry primary tools: %#v", names)
	}
}

func TestFirstStepPolicyNonDebugNoForcedRetry(t *testing.T) {
	a := &Agent{
		firstStepAllowed: make(map[string]bool),
	}
	a.setFirstStepPolicy(router.IntentCodebase, []string{"repo_map", "read_file"})

	if a.shouldRetryNoToolFirstStep() {
		t.Fatal("did not expect non-debug first-step policy to force retry")
	}
}

func TestShouldRetryNoToolFirstStepByIntentFallback(t *testing.T) {
	a := &Agent{
		firstStepAllowed: make(map[string]bool),
		session: &session.Session{
			Messages: []provider.Message{
				{
					Role: provider.RoleUser,
					Content: []provider.Content{
						{Type: provider.ContentTypeText, Text: "这个模块报 panic，帮我定位根因"},
					},
				},
			},
		},
	}

	if !a.shouldRetryNoToolFirstStep() {
		t.Fatal("expected debug intent fallback to require a retry")
	}
}
