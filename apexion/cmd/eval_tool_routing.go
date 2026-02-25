package cmd

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/apexion-ai/apexion/internal/router"
	"github.com/apexion-ai/apexion/internal/tools"
	"github.com/spf13/cobra"
)

func newEvalToolRoutingCmd() *cobra.Command {
	var (
		datasetPath           string
		maxCandidates         int
		jsonOutput            bool
		strict                bool
		includeSyntheticMCP   bool
		strategy              string
		shadowEval            bool
		shadowSampleRate      float64
		deterministicFastpath bool
		fastpathConfidence    float64
	)

	cmd := &cobra.Command{
		Use:   "eval-tool-routing",
		Short: "Evaluate tool router quality on an offline dataset",
		Long:  "Runs internal/router planning on a labeled dataset and reports intent/top-k/filter metrics.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ds, err := router.LoadEvalDataset(datasetPath)
			if err != nil {
				return err
			}

			reg := tools.DefaultRegistry(nil, nil)
			candidates := make([]router.CandidateTool, 0, len(reg.All())+1)
			for _, t := range reg.All() {
				candidates = append(candidates, router.CandidateTool{
					Name:        t.Name(),
					Description: t.Description(),
					ReadOnly:    t.IsReadOnly(),
				})
			}
			if includeSyntheticMCP && !hasCandidate(candidates, "mcp__minimax__understand_image") {
				candidates = append(candidates, router.CandidateTool{
					Name:        "mcp__minimax__understand_image",
					Description: "Synthetic vision bridge tool for offline routing evaluation",
					ReadOnly:    false,
				})
			}

			summary, results := router.EvaluateDataset(ds, candidates, router.EvalOptions{
				MaxCandidates:         maxCandidates,
				Strategy:              router.RoutingStrategy(strategy),
				ShadowEval:            shadowEval,
				ShadowSampleRate:      shadowSampleRate,
				DeterministicFastpath: deterministicFastpath,
				FastpathConfidence:    fastpathConfidence,
			})

			if jsonOutput {
				payload := map[string]any{
					"dataset": datasetPath,
					"version": ds.Version,
					"summary": summary,
					"results": results,
				}
				b, _ := json.MarshalIndent(payload, "", "  ")
				fmt.Println(string(b))
			} else {
				printEvalSummary(datasetPath, ds.Version, strategy, summary)
				printEvalFailures(results, 12)
			}

			if strict && summary.Fail > 0 {
				return fmt.Errorf("tool routing evaluation failed: %d/%d cases failed", summary.Fail, summary.Total)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&datasetPath, "dataset", "docs/tool-routing-eval-dataset.json", "path to evaluation dataset json")
	cmd.Flags().IntVar(&maxCandidates, "max-candidates", 0, "max primary candidates passed to router (0 = unlimited)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "print JSON output")
	cmd.Flags().BoolVar(&strict, "strict", false, "return non-zero when any case fails")
	cmd.Flags().BoolVar(&includeSyntheticMCP, "include-synthetic-mcp", true, "inject mcp__minimax__understand_image for offline vision cases")
	cmd.Flags().StringVar(&strategy, "strategy", string(router.RoutingLegacy), "routing strategy: legacy | hybrid | capability_v2")
	cmd.Flags().BoolVar(&shadowEval, "shadow-eval", false, "emit shadow route diff metrics")
	cmd.Flags().Float64Var(&shadowSampleRate, "shadow-sample-rate", 1.0, "shadow routing sample rate [0,1]")
	cmd.Flags().BoolVar(&deterministicFastpath, "deterministic-fastpath", false, "enable deterministic fastpath scoring in router eval")
	cmd.Flags().Float64Var(&fastpathConfidence, "fastpath-confidence", 0.85, "minimum confidence for fastpath emission")
	return cmd
}

func hasCandidate(cands []router.CandidateTool, name string) bool {
	for _, c := range cands {
		if c.Name == name {
			return true
		}
	}
	return false
}

func printEvalSummary(datasetPath, version, strategy string, s router.EvalSummary) {
	fmt.Printf("Tool Routing Evaluation\n")
	fmt.Printf("Dataset: %s (version=%s)\n", datasetPath, version)
	fmt.Printf("Strategy: %s\n", strategy)
	fmt.Printf("Total: %d  Pass: %d  Fail: %d\n", s.Total, s.Pass, s.Fail)
	fmt.Printf("Intent:   %d/%d (%.1f%%)\n", s.IntentCorrect, s.IntentChecks, 100*s.IntentAccuracy())
	fmt.Printf("Top hit:  %d/%d (%.1f%%)\n", s.TopCorrect, s.TopChecks, 100*s.TopHitRate())
	fmt.Printf("Contain:  %d/%d (%.1f%%)\n", s.ContainCorrect, s.ContainChecks, 100*s.ContainHitRate())
	fmt.Printf("Filtered: %d/%d (%.1f%%)\n", s.FilteredCorrect, s.FilteredChecks, 100*s.FilteredHitRate())
	if s.ShadowChecks > 0 {
		fmt.Printf("Shadow diff(top1): %d/%d\n", s.ShadowTopDiff, s.ShadowChecks)
	}
	if s.FastpathHits > 0 {
		fmt.Printf("Fastpath hits: %d/%d\n", s.FastpathHits, s.Total)
	}
}

func printEvalFailures(results []router.EvalCaseResult, maxLines int) {
	failed := make([]router.EvalCaseResult, 0, len(results))
	for _, r := range results {
		if !r.Passed {
			failed = append(failed, r)
		}
	}
	if len(failed) == 0 {
		fmt.Println("Failures: 0")
		return
	}

	fmt.Printf("Failures: %d\n", len(failed))
	for i, r := range failed {
		if i >= maxLines {
			fmt.Printf("... and %d more failures\n", len(failed)-maxLines)
			return
		}
		top := r.PrimaryTools
		if len(top) > 5 {
			top = top[:5]
		}
		fmt.Printf("- %s\n", r.ID)
		fmt.Printf("  expected intent: %s, actual: %s\n", r.ExpectedIntent, r.ActualIntent)
		fmt.Printf("  top tools: %s\n", strings.Join(top, ", "))
		if len(r.FilteredTools) > 0 {
			f := r.FilteredTools
			slices.Sort(f)
			if len(f) > 5 {
				f = f[:5]
			}
			fmt.Printf("  filtered: %s\n", strings.Join(f, ", "))
		}
		for _, fail := range r.Failures {
			fmt.Printf("  reason: %s\n", fail)
		}
	}
}
