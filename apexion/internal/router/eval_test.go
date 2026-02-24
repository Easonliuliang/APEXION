package router

import "testing"

func TestEvaluateDataset(t *testing.T) {
	ds := &EvalDataset{
		Version: "test",
		Cases: []EvalCase{
			{
				ID:             "research_docs",
				UserText:       "find latest official docs for go context package",
				ExpectedIntent: IntentResearch,
				ExpectedTopAny: []string{"doc_context"},
				ExpectedTopK:   1,
			},
			{
				ID:                  "vision_minimax_bridge",
				UserText:            "please analyze this screenshot",
				HasImage:            true,
				ModelImageSupported: false,
				ExpectedIntent:      IntentVision,
				ExpectedTopAny:      []string{"mcp__minimax__understand_image"},
				ExpectedTopK:        1,
				ExpectedFiltered:    []string{"web_fetch"},
			},
		},
	}

	tools := []CandidateTool{
		{Name: "doc_context", ReadOnly: true},
		{Name: "web_search", ReadOnly: true},
		{Name: "web_fetch", ReadOnly: true},
		{Name: "repo_map", ReadOnly: true},
		{Name: "symbol_nav", ReadOnly: true},
		{Name: "mcp__minimax__understand_image", ReadOnly: false},
	}

	summary, results := EvaluateDataset(ds, tools, EvalOptions{})
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if summary.Fail != 0 {
		t.Fatalf("expected no failures, got %+v", summary)
	}
	if summary.IntentCorrect != 2 {
		t.Fatalf("expected intent correct=2, got %+v", summary)
	}
}
