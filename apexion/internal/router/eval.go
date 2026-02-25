package router

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
)

// EvalCase defines one offline routing evaluation sample.
type EvalCase struct {
	ID                  string `json:"id"`
	UserText            string `json:"user_text"`
	HasImage            bool   `json:"has_image"`
	ModelImageSupported bool   `json:"model_image_supported"`

	ExpectedIntent Intent   `json:"expected_intent"`
	ExpectedTopAny []string `json:"expected_top_any,omitempty"`
	ExpectedTopK   int      `json:"expected_top_k,omitempty"`

	ExpectedMustContain []string `json:"expected_must_contain,omitempty"`
	ExpectedFiltered    []string `json:"expected_filtered,omitempty"`
}

// EvalDataset is a collection of routing evaluation cases.
type EvalDataset struct {
	Version string     `json:"version"`
	Cases   []EvalCase `json:"cases"`
}

// EvalOptions controls evaluation behavior.
type EvalOptions struct {
	MaxCandidates         int
	Strategy              RoutingStrategy
	ShadowEval            bool
	ShadowSampleRate      float64
	DeterministicFastpath bool
	FastpathConfidence    float64
}

// EvalCaseResult is the evaluated result for one sample.
type EvalCaseResult struct {
	ID     string `json:"id"`
	Passed bool   `json:"passed"`

	ExpectedIntent Intent `json:"expected_intent"`
	ActualIntent   Intent `json:"actual_intent"`

	PrimaryTools  []string `json:"primary_tools"`
	FilteredTools []string `json:"filtered_tools,omitempty"`
	ShadowTools   []string `json:"shadow_tools,omitempty"`
	FastPathTool  string   `json:"fastpath_tool,omitempty"`

	Failures []string `json:"failures,omitempty"`
}

// EvalSummary aggregates evaluation metrics.
type EvalSummary struct {
	Total int `json:"total"`
	Pass  int `json:"pass"`
	Fail  int `json:"fail"`

	IntentChecks      int `json:"intent_checks"`
	IntentCorrect     int `json:"intent_correct"`
	TopChecks         int `json:"top_checks"`
	TopCorrect        int `json:"top_correct"`
	ContainChecks     int `json:"contain_checks"`
	ContainCorrect    int `json:"contain_correct"`
	FilteredChecks    int `json:"filtered_checks"`
	FilteredCorrect   int `json:"filtered_correct"`
	MaxCandidatesUsed int `json:"max_candidates_used"`
	ShadowChecks      int `json:"shadow_checks"`
	ShadowTopDiff     int `json:"shadow_top_diff"`
	FastpathHits      int `json:"fastpath_hits"`
}

func (s EvalSummary) IntentAccuracy() float64 {
	if s.IntentChecks == 0 {
		return 0
	}
	return float64(s.IntentCorrect) / float64(s.IntentChecks)
}

func (s EvalSummary) TopHitRate() float64 {
	if s.TopChecks == 0 {
		return 0
	}
	return float64(s.TopCorrect) / float64(s.TopChecks)
}

func (s EvalSummary) ContainHitRate() float64 {
	if s.ContainChecks == 0 {
		return 0
	}
	return float64(s.ContainCorrect) / float64(s.ContainChecks)
}

func (s EvalSummary) FilteredHitRate() float64 {
	if s.FilteredChecks == 0 {
		return 0
	}
	return float64(s.FilteredCorrect) / float64(s.FilteredChecks)
}

// LoadEvalDataset reads and parses a routing evaluation dataset JSON file.
func LoadEvalDataset(path string) (*EvalDataset, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read dataset: %w", err)
	}
	var ds EvalDataset
	if err := json.Unmarshal(raw, &ds); err != nil {
		return nil, fmt.Errorf("parse dataset: %w", err)
	}
	if len(ds.Cases) == 0 {
		return nil, fmt.Errorf("dataset has no cases")
	}
	for i := range ds.Cases {
		if strings.TrimSpace(ds.Cases[i].ID) == "" {
			ds.Cases[i].ID = fmt.Sprintf("case_%d", i+1)
		}
	}
	return &ds, nil
}

// EvaluateDataset runs all cases through router.Plan and returns metrics.
func EvaluateDataset(ds *EvalDataset, available []CandidateTool, opts EvalOptions) (EvalSummary, []EvalCaseResult) {
	summary := EvalSummary{
		Total:             len(ds.Cases),
		MaxCandidatesUsed: opts.MaxCandidates,
	}
	results := make([]EvalCaseResult, 0, len(ds.Cases))

	for _, c := range ds.Cases {
		plan := Plan(PlanInput{
			UserText:            c.UserText,
			HasImage:            c.HasImage,
			ModelImageSupported: c.ModelImageSupported,
			Tools:               available,
		}, PlanOptions{
			MaxCandidates:         opts.MaxCandidates,
			Strategy:              opts.Strategy,
			ShadowEval:            opts.ShadowEval,
			ShadowSampleRate:      opts.ShadowSampleRate,
			DeterministicFastpath: opts.DeterministicFastpath,
			FastpathConfidence:    opts.FastpathConfidence,
		})

		r := EvalCaseResult{
			ID:             c.ID,
			ExpectedIntent: c.ExpectedIntent,
			ActualIntent:   plan.Intent,
			PrimaryTools:   plannedNames(plan.Primary),
			FilteredTools:  filteredNames(plan.Filtered),
			Passed:         true,
		}
		if plan.Shadow != nil {
			r.ShadowTools = plannedNames(plan.Shadow.Primary)
			summary.ShadowChecks++
			if topToolName(r.PrimaryTools) != topToolName(r.ShadowTools) {
				summary.ShadowTopDiff++
			}
		}
		if plan.FastPath != nil {
			r.FastPathTool = plan.FastPath.Tool
			summary.FastpathHits++
		}

		summary.IntentChecks++
		if plan.Intent == c.ExpectedIntent {
			summary.IntentCorrect++
		} else {
			r.Passed = false
			r.Failures = append(r.Failures, fmt.Sprintf("intent mismatch: want=%s got=%s", c.ExpectedIntent, plan.Intent))
		}

		if len(c.ExpectedTopAny) > 0 {
			summary.TopChecks++
			topK := c.ExpectedTopK
			if topK <= 0 {
				topK = 1
			}
			if topK > len(r.PrimaryTools) {
				topK = len(r.PrimaryTools)
			}
			topNames := r.PrimaryTools[:topK]
			if anyIn(topNames, c.ExpectedTopAny) {
				summary.TopCorrect++
			} else {
				r.Passed = false
				r.Failures = append(r.Failures, fmt.Sprintf("top-%d miss: expected one of %v, got %v", topK, c.ExpectedTopAny, topNames))
			}
		}

		if len(c.ExpectedMustContain) > 0 {
			summary.ContainChecks++
			missing := missingFrom(r.PrimaryTools, c.ExpectedMustContain)
			if len(missing) == 0 {
				summary.ContainCorrect++
			} else {
				r.Passed = false
				r.Failures = append(r.Failures, fmt.Sprintf("missing expected tools in primary: %v", missing))
			}
		}

		if len(c.ExpectedFiltered) > 0 {
			summary.FilteredChecks++
			missing := missingFrom(r.FilteredTools, c.ExpectedFiltered)
			if len(missing) == 0 {
				summary.FilteredCorrect++
			} else {
				r.Passed = false
				r.Failures = append(r.Failures, fmt.Sprintf("missing expected filtered tools: %v", missing))
			}
		}

		if r.Passed {
			summary.Pass++
		} else {
			summary.Fail++
		}
		results = append(results, r)
	}

	return summary, results
}

func plannedNames(in []PlannedTool) []string {
	out := make([]string, 0, len(in))
	for _, t := range in {
		out = append(out, t.Name)
	}
	return out
}

func filteredNames(in []FilteredTool) []string {
	out := make([]string, 0, len(in))
	for _, t := range in {
		out = append(out, t.Name)
	}
	return out
}

func anyIn(left, right []string) bool {
	for _, l := range left {
		if slices.Contains(right, l) {
			return true
		}
	}
	return false
}

func missingFrom(got, expected []string) []string {
	var missing []string
	for _, e := range expected {
		if !slices.Contains(got, e) {
			missing = append(missing, e)
		}
	}
	return missing
}

func topToolName(in []string) string {
	if len(in) == 0 {
		return ""
	}
	return in[0]
}
