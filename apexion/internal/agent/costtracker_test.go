package agent

import (
	"strings"
	"testing"
)

func TestDefaultPricing(t *testing.T) {
	pricing := DefaultPricing()
	if len(pricing) == 0 {
		t.Fatal("default pricing should not be empty")
	}
	for _, model := range []string{"claude-sonnet-4-20250514", "gpt-4o", "deepseek-chat"} {
		if _, ok := pricing[model]; !ok {
			t.Errorf("expected pricing for %q", model)
		}
	}
	for model, p := range pricing {
		if p.InputPerMillion <= 0 || p.OutputPerMillion <= 0 {
			t.Errorf("model %q has non-positive pricing: in=%f out=%f",
				model, p.InputPerMillion, p.OutputPerMillion)
		}
	}
}

func TestRecordTurn(t *testing.T) {
	ct := NewCostTracker(nil)
	cost := ct.RecordTurn("deepseek-chat", 1000, 500)
	if cost <= 0 {
		t.Fatal("expected positive cost for known model")
	}
	if ct.SessionCost() != cost {
		t.Fatalf("session cost %f != turn cost %f", ct.SessionCost(), cost)
	}
	// deepseek-chat: input=0.27/M, output=1.10/M
	expected := (1000.0*0.27 + 500.0*1.10) / 1_000_000
	if cost != expected {
		t.Fatalf("cost %f != expected %f", cost, expected)
	}
}

func TestRecordTurnUnknownModel(t *testing.T) {
	ct := NewCostTracker(nil)
	cost := ct.RecordTurn("unknown-model-xyz", 1000, 500)
	if cost != 0 {
		t.Fatalf("expected 0 cost for unknown model, got %f", cost)
	}
	if ct.SessionCost() != 0 {
		t.Fatalf("expected 0 session cost, got %f", ct.SessionCost())
	}
}

func TestRecordTurnWithOverrides(t *testing.T) {
	overrides := map[string]ModelPricing{
		"my-custom-model": {InputPerMillion: 10.0, OutputPerMillion: 20.0},
	}
	ct := NewCostTracker(overrides)
	cost := ct.RecordTurn("my-custom-model", 1_000_000, 500_000)
	expected := 10.0 + (500_000.0 * 20.0 / 1_000_000)
	if cost != expected {
		t.Fatalf("cost %f != expected %f", cost, expected)
	}
}

func TestRecordTurnPrefixMatch(t *testing.T) {
	ct := NewCostTracker(nil)
	// "gpt-4o-2024-08-06" should prefix-match "gpt-4o"
	cost := ct.RecordTurn("gpt-4o-2024-08-06", 1000, 500)
	if cost <= 0 {
		t.Fatal("expected positive cost via prefix match")
	}
}

func TestFormatCost(t *testing.T) {
	ct := NewCostTracker(nil)
	// No usage — small value format
	if got := ct.FormatCost(); got != "$0.0000" {
		t.Fatalf("expected $0.0000, got %s", got)
	}
	// Small cost (<$0.01) — 4 decimal places
	ct.RecordTurn("deepseek-chat", 1000, 500)
	fc := ct.FormatCost()
	if !strings.HasPrefix(fc, "$0.") {
		t.Fatalf("expected small cost format, got %s", fc)
	}
	// Large cost (>=$0.01) — 2 decimal places
	ct2 := NewCostTracker(nil)
	ct2.RecordTurn("claude-opus-4-20250514", 1_000_000, 100_000)
	fc2 := ct2.FormatCost()
	if !strings.HasPrefix(fc2, "$") {
		t.Fatalf("expected dollar prefix, got %s", fc2)
	}
	if strings.Count(fc2, ".") != 1 {
		t.Fatalf("expected one decimal point, got %s", fc2)
	}
}

func TestSummaryEmpty(t *testing.T) {
	ct := NewCostTracker(nil)
	if s := ct.Summary(); s != "No usage recorded." {
		t.Fatalf("expected 'No usage recorded.', got %q", s)
	}
}

func TestSummary(t *testing.T) {
	ct := NewCostTracker(nil)
	ct.RecordTurn("deepseek-chat", 1000, 500)
	ct.RecordTurn("gpt-4o", 2000, 1000)

	s := ct.Summary()
	if !strings.Contains(s, "Session cost:") {
		t.Fatal("summary should contain 'Session cost:'")
	}
	if !strings.Contains(s, "2 turns") {
		t.Fatal("summary should mention 2 turns")
	}
	if !strings.Contains(s, "Turn 1:") || !strings.Contains(s, "Turn 2:") {
		t.Fatal("summary should contain per-turn details")
	}
	if !strings.Contains(s, "deepseek-chat") {
		t.Fatal("summary should mention model names")
	}
	if !strings.Contains(s, "Total tokens:") {
		t.Fatal("summary should contain total tokens")
	}
	if !strings.Contains(s, "3000 input") {
		t.Fatal("summary should show total input tokens")
	}
}
