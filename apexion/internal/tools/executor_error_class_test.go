package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type timeoutTestTool struct{}

func (t *timeoutTestTool) Name() string                     { return "timeout_test" }
func (t *timeoutTestTool) Description() string              { return "timeout test tool" }
func (t *timeoutTestTool) Parameters() map[string]any       { return map[string]any{} }
func (t *timeoutTestTool) IsReadOnly() bool                 { return true }
func (t *timeoutTestTool) PermissionLevel() PermissionLevel { return PermissionRead }
func (t *timeoutTestTool) Execute(ctx context.Context, _ json.RawMessage) (ToolResult, error) {
	<-ctx.Done()
	return ToolResult{}, ctx.Err()
}

func TestExecutorClassifiesTimeout(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&timeoutTestTool{})

	exec := NewExecutor(reg, &allowAllPolicy{})
	exec.defaultTimeout = 20 * time.Millisecond

	result := exec.Execute(context.Background(), "timeout_test", json.RawMessage(`{}`))
	if !result.IsError {
		t.Fatalf("expected error result, got %+v", result)
	}
	if result.ErrorClass != "timeout" {
		t.Fatalf("expected timeout error class, got %q (content=%q)", result.ErrorClass, result.Content)
	}
}
