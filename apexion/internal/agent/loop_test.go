package agent

import (
	"testing"

	"github.com/apexion-ai/apexion/internal/provider"
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
