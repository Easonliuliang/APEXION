package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/apexion-ai/apexion/internal/tools"
	"github.com/apexion-ai/apexion/internal/tui"
)

// BGStatus represents the state of a background agent.
type BGStatus string

const (
	BGPending BGStatus = "pending"
	BGRunning BGStatus = "running"
	BGDone    BGStatus = "done"
	BGFailed  BGStatus = "failed"
)

// BackgroundAgent holds the state of a single background agent.
type BackgroundAgent struct {
	ID        string
	Prompt    string
	Mode      string
	Status    BGStatus
	StartedAt time.Time
	DoneAt    time.Time
	Output    string
	Error     error
	cancel    context.CancelFunc
}

// BackgroundManager manages background agent goroutines.
type BackgroundManager struct {
	mu       sync.Mutex
	agents   map[string]*BackgroundAgent
	counter  int
	maxSlots int
	io       tui.IO
}

// NewBackgroundManager creates a BackgroundManager.
func NewBackgroundManager(maxSlots int, io tui.IO) *BackgroundManager {
	if maxSlots <= 0 {
		maxSlots = 4
	}
	return &BackgroundManager{
		agents:   make(map[string]*BackgroundAgent),
		maxSlots: maxSlots,
		io:       io,
	}
}

// Launch starts a background agent and returns its ID.
// Implements tools.BackgroundLauncher interface.
func (bm *BackgroundManager) Launch(ctx context.Context, prompt, mode string, runner tools.SubAgentRunner) (string, error) {
	bm.mu.Lock()

	// Check slot limit
	running := 0
	for _, a := range bm.agents {
		if a.Status == BGRunning || a.Status == BGPending {
			running++
		}
	}
	if running >= bm.maxSlots {
		bm.mu.Unlock()
		return "", fmt.Errorf("max background slots (%d) reached; use /bg collect to free slots", bm.maxSlots)
	}

	bm.counter++
	id := fmt.Sprintf("bg-%d", bm.counter)

	bgCtx, cancel := context.WithCancel(ctx)
	agent := &BackgroundAgent{
		ID:        id,
		Prompt:    prompt,
		Mode:      mode,
		Status:    BGRunning,
		StartedAt: time.Now(),
		cancel:    cancel,
	}
	bm.agents[id] = agent
	bm.mu.Unlock()

	// Run in background goroutine
	go func() {
		output, err := runner(bgCtx, prompt, mode)

		bm.mu.Lock()
		agent.DoneAt = time.Now()
		agent.Output = output
		if err != nil {
			agent.Error = err
			agent.Status = BGFailed
		} else {
			agent.Status = BGDone
		}
		bm.mu.Unlock()

		// Notify the user
		elapsed := agent.DoneAt.Sub(agent.StartedAt).Round(time.Second)
		if err != nil {
			bm.io.SystemMessage(fmt.Sprintf("Background agent %s failed after %s: %v", id, elapsed, err))
		} else {
			bm.io.SystemMessage(fmt.Sprintf("Background agent %s completed in %s. Use /bg collect %s to view output.", id, elapsed, id))
		}
	}()

	return id, nil
}

// List returns all background agents.
func (bm *BackgroundManager) List() []*BackgroundAgent {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	agents := make([]*BackgroundAgent, 0, len(bm.agents))
	for _, a := range bm.agents {
		agents = append(agents, a)
	}
	return agents
}

// Collect retrieves the output of a completed background agent and removes it.
func (bm *BackgroundManager) Collect(id string) (string, error) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	agent, ok := bm.agents[id]
	if !ok {
		return "", fmt.Errorf("unknown background agent: %s", id)
	}

	switch agent.Status {
	case BGRunning, BGPending:
		return "", fmt.Errorf("agent %s is still running", id)
	case BGFailed:
		output := fmt.Sprintf("[Agent failed: %v]\n\n%s", agent.Error, agent.Output)
		delete(bm.agents, id)
		return output, nil
	case BGDone:
		output := agent.Output
		delete(bm.agents, id)
		return output, nil
	default:
		return "", fmt.Errorf("agent %s has unexpected status: %s", id, agent.Status)
	}
}

// CollectAll retrieves all completed agents' outputs and removes them.
func (bm *BackgroundManager) CollectAll() []BackgroundResult {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	var results []BackgroundResult
	for id, agent := range bm.agents {
		if agent.Status == BGDone || agent.Status == BGFailed {
			result := BackgroundResult{
				ID:     id,
				Output: agent.Output,
			}
			if agent.Error != nil {
				result.Error = agent.Error.Error()
			}
			results = append(results, result)
			delete(bm.agents, id)
		}
	}
	return results
}

// BackgroundResult holds the output from a collected background agent.
type BackgroundResult struct {
	ID     string
	Output string
	Error  string
}

// Cancel stops a running background agent.
func (bm *BackgroundManager) Cancel(id string) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	agent, ok := bm.agents[id]
	if !ok {
		return fmt.Errorf("unknown background agent: %s", id)
	}

	if agent.Status != BGRunning && agent.Status != BGPending {
		return fmt.Errorf("agent %s is not running (status: %s)", id, agent.Status)
	}

	agent.cancel()
	agent.Status = BGFailed
	agent.Error = fmt.Errorf("cancelled by user")
	agent.DoneAt = time.Now()
	return nil
}

// WaitAll blocks until all running agents complete.
func (bm *BackgroundManager) WaitAll(ctx context.Context) error {
	for {
		bm.mu.Lock()
		running := 0
		for _, a := range bm.agents {
			if a.Status == BGRunning || a.Status == BGPending {
				running++
			}
		}
		bm.mu.Unlock()

		if running == 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
			// poll again
		}
	}
}

// RunningCount returns the number of currently running agents.
func (bm *BackgroundManager) RunningCount() int {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	count := 0
	for _, a := range bm.agents {
		if a.Status == BGRunning || a.Status == BGPending {
			count++
		}
	}
	return count
}

// Summary returns a formatted status string for all agents.
func (bm *BackgroundManager) Summary() string {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if len(bm.agents) == 0 {
		return "No background agents."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Background agents (%d):\n", len(bm.agents)))

	for _, a := range bm.agents {
		elapsed := ""
		switch a.Status {
		case BGRunning:
			elapsed = fmt.Sprintf(" (%s)", time.Since(a.StartedAt).Round(time.Second))
		case BGDone, BGFailed:
			elapsed = fmt.Sprintf(" (%s)", a.DoneAt.Sub(a.StartedAt).Round(time.Second))
		}

		prompt := a.Prompt
		if len(prompt) > 60 {
			prompt = prompt[:57] + "..."
		}

		sb.WriteString(fmt.Sprintf("  %s  [%s]%s  %s\n", a.ID, a.Status, elapsed, prompt))

		if a.Status == BGFailed && a.Error != nil {
			sb.WriteString(fmt.Sprintf("    error: %v\n", a.Error))
		}
	}

	return strings.TrimRight(sb.String(), "\n")
}
