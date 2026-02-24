package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBash_NormalCommand(t *testing.T) {
	tool := &BashTool{}
	params, _ := json.Marshal(map[string]any{"command": "echo hello"})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "hello") {
		t.Fatalf("expected 'hello' in output, got: %s", result.Content)
	}
}

func TestBash_HardTimeout_KillsSleepingProcess(t *testing.T) {
	tool := &BashTool{}
	// sleep 300 should be terminated by hard timeout quickly in tests.
	params, _ := json.Marshal(map[string]any{
		"command": "echo start && sleep 300",
		"timeout": 2,
	})

	start := time.Now()
	result, err := tool.Execute(context.Background(), params)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error result for timeout")
	}
	if !strings.Contains(strings.ToLower(result.Content), "timed out") {
		t.Fatalf("expected timeout message in output, got: %s", result.Content)
	}
	if elapsed > 8*time.Second {
		t.Fatalf("timeout took too long: %v (expected <8s)", elapsed)
	}
	t.Logf("timeout fired after %v, output: %s", elapsed, result.Content)
}

func TestBash_ActiveOutput_NoIdleTimeout(t *testing.T) {
	tool := &BashTool{}
	// Produces output every second for 5 seconds — should complete successfully.
	params, _ := json.Marshal(map[string]any{
		"command": "for i in 1 2 3 4 5; do echo line$i; sleep 1; done",
		"timeout": 20,
	})

	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "line5") {
		t.Fatalf("expected 'line5' in output, got: %s", result.Content)
	}
}

func TestBash_InteractiveCommand_FailsFast(t *testing.T) {
	tool := &BashTool{}
	// Python input() with stdin=/dev/null should fail immediately with EOFError.
	params, _ := json.Marshal(map[string]any{
		"command": `python3 -c "input('prompt: ')"`,
	})

	start := time.Now()
	result, err := tool.Execute(context.Background(), params)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error for interactive command")
	}
	if !strings.Contains(result.Content, "EOF") {
		t.Fatalf("expected EOF error, got: %s", result.Content)
	}
	// Should fail in under 5 seconds (stdin is disconnected).
	if elapsed > 5*time.Second {
		t.Fatalf("interactive command took too long: %v", elapsed)
	}
}

func TestBash_ProcessGroupKill(t *testing.T) {
	tool := &BashTool{}
	// Spawn a child process that also sleeps — both should be killed.
	params, _ := json.Marshal(map[string]any{
		"command": "echo parent && (sleep 300 &) && sleep 300",
		"timeout": 2,
	})

	start := time.Now()
	result, err := tool.Execute(context.Background(), params)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error result")
	}
	if !strings.Contains(strings.ToLower(result.Content), "timed out") {
		t.Fatalf("expected timeout output, got: %s", result.Content)
	}
	if elapsed > 8*time.Second {
		t.Fatalf("took too long: %v", elapsed)
	}
	t.Logf("process group killed after %v", elapsed)
}
