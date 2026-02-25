package agent

import (
	"fmt"
	"strings"
	"time"
)

type toolHealthState struct {
	Successes        int
	Failures         int
	ConsecutiveFails int
	CooldownUntil    time.Time
	LastError        string
	UpdatedAt        time.Time
}

type toolHealthSnapshot struct {
	Score                int
	Successes            int
	Failures             int
	ConsecutiveFails     int
	CircuitOpen          bool
	CooldownRemainingSec int
}

func (a *Agent) canExecuteTool(toolName string, now time.Time) (bool, string) {
	cb := a.config.ToolRouting.CircuitBreaker
	if !cb.Enabled {
		return true, ""
	}
	if strings.TrimSpace(toolName) == "" {
		return true, ""
	}
	a.toolHealthMu.RLock()
	st, ok := a.toolHealth[toolName]
	a.toolHealthMu.RUnlock()
	if !ok || st == nil || st.CooldownUntil.IsZero() {
		return true, ""
	}
	if now.After(st.CooldownUntil) {
		return true, ""
	}
	remaining := int(time.Until(st.CooldownUntil).Seconds())
	if remaining < 1 {
		remaining = 1
	}
	return false, fmt.Sprintf("tool `%s` temporarily disabled by circuit breaker (cooldown %ds)", toolName, remaining)
}

func (a *Agent) recordToolOutcome(toolName string, isError bool, errText string, now time.Time) {
	if strings.TrimSpace(toolName) == "" {
		return
	}
	a.toolHealthMu.Lock()
	defer a.toolHealthMu.Unlock()
	if a.toolHealth == nil {
		a.toolHealth = make(map[string]*toolHealthState)
	}

	st, ok := a.toolHealth[toolName]
	if !ok || st == nil {
		st = &toolHealthState{}
		a.toolHealth[toolName] = st
	}

	st.UpdatedAt = now
	if isError {
		st.Failures++
		st.ConsecutiveFails++
		st.LastError = strings.TrimSpace(errText)
		if len(st.LastError) > 240 {
			st.LastError = st.LastError[:240]
		}

		cb := a.config.ToolRouting.CircuitBreaker
		threshold := cb.FailThreshold
		if threshold <= 0 {
			threshold = 3
		}
		cooldownSec := cb.CooldownSec
		if cooldownSec <= 0 {
			cooldownSec = 120
		}
		if cb.Enabled && st.ConsecutiveFails >= threshold {
			st.CooldownUntil = now.Add(time.Duration(cooldownSec) * time.Second)
			st.ConsecutiveFails = 0
		}
		return
	}

	st.Successes++
	st.ConsecutiveFails = 0
	st.CooldownUntil = time.Time{}
	st.LastError = ""
}

func (a *Agent) toolHealthSnapshot(toolName string, now time.Time) toolHealthSnapshot {
	out := toolHealthSnapshot{Score: 100}
	a.toolHealthMu.RLock()
	st, ok := a.toolHealth[toolName]
	a.toolHealthMu.RUnlock()
	if !ok || st == nil {
		return out
	}

	out.Successes = st.Successes
	out.Failures = st.Failures
	out.ConsecutiveFails = st.ConsecutiveFails
	total := st.Successes + st.Failures
	if total > 0 {
		out.Score = int(float64(st.Successes) * 100.0 / float64(total))
	}
	if st.ConsecutiveFails > 0 {
		out.Score -= st.ConsecutiveFails * 10
	}
	if out.Score < 0 {
		out.Score = 0
	}
	if out.Score > 100 {
		out.Score = 100
	}
	if !st.CooldownUntil.IsZero() && now.Before(st.CooldownUntil) {
		out.CircuitOpen = true
		out.CooldownRemainingSec = int(time.Until(st.CooldownUntil).Seconds())
		if out.CooldownRemainingSec < 1 {
			out.CooldownRemainingSec = 1
		}
		if out.Score > 20 {
			out.Score = 20
		}
	}
	return out
}
