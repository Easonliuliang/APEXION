package agent

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	"github.com/aictl/aictl/internal/provider"
)

// doomLoopAction is the action recommended by the doom loop detector.
type doomLoopAction int

const (
	doomLoopNone doomLoopAction = iota
	doomLoopWarn
	doomLoopStop
)

const (
	doomLoopWarnThreshold = 3
	doomLoopStopThreshold = 5
)

// doomLoopDetector tracks consecutive identical tool call batches
// to detect infinite loops where the model keeps issuing the same calls.
type doomLoopDetector struct {
	lastSig string
	streak  int
}

// check evaluates a batch of tool calls and returns the recommended action.
// It resets the streak whenever the batch signature changes.
func (d *doomLoopDetector) check(calls []*provider.ToolCallRequest) doomLoopAction {
	sig := batchSignature(calls)
	if sig == d.lastSig {
		d.streak++
	} else {
		d.lastSig = sig
		d.streak = 1
	}

	switch {
	case d.streak >= doomLoopStopThreshold:
		return doomLoopStop
	case d.streak >= doomLoopWarnThreshold:
		return doomLoopWarn
	default:
		return doomLoopNone
	}
}

// batchSignature produces a deterministic hash for a set of tool calls
// based on their names and inputs. The calls are sorted by name+input
// so that ordering doesn't affect the signature.
func batchSignature(calls []*provider.ToolCallRequest) string {
	parts := make([]string, len(calls))
	for i, c := range calls {
		parts[i] = c.Name + ":" + string(c.Input)
	}
	sort.Strings(parts)
	h := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return fmt.Sprintf("%x", h)
}
