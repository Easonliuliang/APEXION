package agent

import (
	"encoding/json"
	"testing"

	"github.com/apexion-ai/apexion/internal/provider"
)

func TestFailureLoopDetector_WarnAndStop(t *testing.T) {
	d := &failureLoopDetector{}
	calls := []*provider.ToolCallRequest{{
		ID:    "t1",
		Name:  "read_file",
		Input: json.RawMessage(`{"file_path":"a.go"}`),
	}}
	results := []provider.Content{{
		Type:       provider.ContentTypeToolResult,
		ToolUseID:  "t1",
		ToolResult: "error: file not found",
		IsError:    true,
	}}

	if got := d.check(calls, results); got != doomLoopNone {
		t.Fatalf("first failure should not warn, got %d", got)
	}
	if got := d.check(calls, results); got != doomLoopWarn {
		t.Fatalf("second repeated failure should warn, got %d", got)
	}
	d.check(calls, results) // third
	if got := d.check(calls, results); got != doomLoopStop {
		t.Fatalf("fourth repeated failure should stop, got %d", got)
	}
}

func TestFailureLoopDetector_ResetOnSuccess(t *testing.T) {
	d := &failureLoopDetector{}
	calls := []*provider.ToolCallRequest{{
		ID:    "t1",
		Name:  "grep",
		Input: json.RawMessage(`{"pattern":"foo"}`),
	}}
	fail := []provider.Content{{
		Type:       provider.ContentTypeToolResult,
		ToolUseID:  "t1",
		ToolResult: "error: invalid params",
		IsError:    true,
	}}
	ok := []provider.Content{{
		Type:       provider.ContentTypeToolResult,
		ToolUseID:  "t1",
		ToolResult: "found",
		IsError:    false,
	}}

	d.check(calls, fail)
	if got := d.check(calls, ok); got != doomLoopNone {
		t.Fatalf("success should reset detector, got %d", got)
	}
	if got := d.check(calls, fail); got != doomLoopNone {
		t.Fatalf("after reset, first failure should be none, got %d", got)
	}
}
