package agent

import (
	"testing"
	"time"

	"github.com/apexion-ai/apexion/internal/config"
)

func TestCircuitBreakerOpensAfterThreshold(t *testing.T) {
	a := &Agent{
		config:     config.DefaultConfig(),
		toolHealth: make(map[string]*toolHealthState),
	}
	base := time.Now()
	for i := 0; i < 3; i++ {
		a.recordToolOutcome("doc_context", true, "upstream error", base.Add(time.Duration(i)*time.Second))
	}

	ok, _ := a.canExecuteTool("doc_context", base.Add(4*time.Second))
	if ok {
		t.Fatal("expected circuit breaker to block tool after threshold failures")
	}
	snap := a.toolHealthSnapshot("doc_context", base.Add(4*time.Second))
	if !snap.CircuitOpen {
		t.Fatal("expected circuit_open=true in health snapshot")
	}
}

func TestCircuitBreakerRecoversAfterCooldown(t *testing.T) {
	a := &Agent{
		config:     config.DefaultConfig(),
		toolHealth: make(map[string]*toolHealthState),
	}
	base := time.Now()
	for i := 0; i < 3; i++ {
		a.recordToolOutcome("symbol_nav", true, "temporary error", base.Add(time.Duration(i)*time.Second))
	}

	ok, _ := a.canExecuteTool("symbol_nav", base.Add(130*time.Second))
	if !ok {
		t.Fatal("expected tool to recover after cooldown period")
	}
}
