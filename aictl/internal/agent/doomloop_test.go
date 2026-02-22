package agent

import (
	"encoding/json"
	"testing"

	"github.com/aictl/aictl/internal/provider"
)

func makeCalls(names ...string) []*provider.ToolCallRequest {
	calls := make([]*provider.ToolCallRequest, len(names))
	for i, n := range names {
		calls[i] = &provider.ToolCallRequest{
			ID:    "id",
			Name:  n,
			Input: json.RawMessage(`{"key":"value"}`),
		}
	}
	return calls
}

func makeCallWithInput(name, input string) []*provider.ToolCallRequest {
	return []*provider.ToolCallRequest{{
		ID:    "id",
		Name:  name,
		Input: json.RawMessage(input),
	}}
}

func TestDoomLoop_DifferentCalls(t *testing.T) {
	d := &doomLoopDetector{}
	for i := 0; i < 10; i++ {
		action := d.check(makeCalls("tool_" + string(rune('a'+i))))
		if action != doomLoopNone {
			t.Fatalf("iteration %d: expected none, got %d", i, action)
		}
	}
}

func TestDoomLoop_WarnAt3(t *testing.T) {
	d := &doomLoopDetector{}
	calls := makeCalls("read_file")

	for i := 0; i < doomLoopWarnThreshold-1; i++ {
		if a := d.check(calls); a != doomLoopNone {
			t.Fatalf("iteration %d: expected none, got %d", i, a)
		}
	}
	if a := d.check(calls); a != doomLoopWarn {
		t.Fatalf("expected warn at threshold %d, got %d", doomLoopWarnThreshold, a)
	}
}

func TestDoomLoop_StopAt5(t *testing.T) {
	d := &doomLoopDetector{}
	calls := makeCalls("read_file")

	for i := 0; i < doomLoopStopThreshold-1; i++ {
		d.check(calls)
	}
	if a := d.check(calls); a != doomLoopStop {
		t.Fatalf("expected stop at threshold %d, got %d", doomLoopStopThreshold, a)
	}
}

func TestDoomLoop_ResetOnDifferentCall(t *testing.T) {
	d := &doomLoopDetector{}
	calls := makeCalls("read_file")

	// Build up to warn-1.
	for i := 0; i < doomLoopWarnThreshold-1; i++ {
		d.check(calls)
	}

	// Insert a different call — should reset streak.
	d.check(makeCalls("write_file"))

	// Same calls again should restart from 1.
	for i := 0; i < doomLoopWarnThreshold-1; i++ {
		if a := d.check(calls); a != doomLoopNone {
			t.Fatalf("after reset, iteration %d: expected none, got %d", i, a)
		}
	}
	if a := d.check(calls); a != doomLoopWarn {
		t.Fatal("expected warn after re-accumulating streak")
	}
}

func TestDoomLoop_SameToolDifferentInput(t *testing.T) {
	d := &doomLoopDetector{}

	for i := 0; i < 10; i++ {
		input := `{"path":"file_` + string(rune('a'+i)) + `.txt"}`
		action := d.check(makeCallWithInput("read_file", input))
		if action != doomLoopNone {
			t.Fatalf("iteration %d: expected none for different inputs, got %d", i, action)
		}
	}
}
