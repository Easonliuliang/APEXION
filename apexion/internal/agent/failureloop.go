package agent

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	"github.com/apexion-ai/apexion/internal/provider"
)

const (
	failureLoopWarnThreshold = 2
	failureLoopStopThreshold = 4
)

// failureLoopDetector tracks repeated tool batches that fail completely.
type failureLoopDetector struct {
	lastSig string
	streak  int
}

// check returns an action only when all tool results in this batch are errors.
func (d *failureLoopDetector) check(calls []*provider.ToolCallRequest, results []provider.Content) doomLoopAction {
	if !allToolResultsFailed(results) {
		d.lastSig = ""
		d.streak = 0
		return doomLoopNone
	}

	sig := failedBatchSignature(calls)
	if sig == "" {
		return doomLoopNone
	}
	if sig == d.lastSig {
		d.streak++
	} else {
		d.lastSig = sig
		d.streak = 1
	}

	switch {
	case d.streak >= failureLoopStopThreshold:
		return doomLoopStop
	case d.streak >= failureLoopWarnThreshold:
		return doomLoopWarn
	default:
		return doomLoopNone
	}
}

func allToolResultsFailed(results []provider.Content) bool {
	total := 0
	failed := 0
	for _, c := range results {
		if c.Type != provider.ContentTypeToolResult {
			continue
		}
		total++
		if c.IsError {
			failed++
		}
	}
	return total > 0 && total == failed
}

func failedBatchSignature(calls []*provider.ToolCallRequest) string {
	if len(calls) == 0 {
		return ""
	}
	parts := make([]string, len(calls))
	for i, c := range calls {
		parts[i] = c.Name + ":" + string(c.Input)
	}
	sort.Strings(parts)
	h := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return fmt.Sprintf("%x", h)
}
