package tui

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
)

// RunTUI starts the bubbletea program and runs agentFn concurrently.
// It blocks until either the agent finishes or the user quits.
//
// agentFn receives a context that is cancelled when:
//   - the user presses Ctrl+C (handled as a key event in raw mode)
//   - the TUI exits for any reason
//   - an OS SIGINT/SIGTERM is received
func RunTUI(cfg TUIConfig, agentFn func(io IO, ctx context.Context) error) error {
	// This context owns the agent's lifetime. Cancelling it stops the agent.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Also handle OS signals (e.g. kill from outside, or SIGTERM from system).
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	inputCh := make(chan inputResult, 1)
	model := NewModel(inputCh, cfg)

	tuiIO := &TuiIO{
		inputCh: inputCh,
	}
	model.cancelToolFn = tuiIO.CancelRunningTool
	model.cancelLoopFn = tuiIO.CancelLoop

	p := tea.NewProgram(model)
	tuiIO.program = p

	var (
		agentErr error
		wg       sync.WaitGroup
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		agentErr = agentFn(tuiIO, ctx)
		p.Send(agentDoneMsg{err: agentErr})
	}()

	if _, err := p.Run(); err != nil {
		cancel() // ensure agent stops if TUI errors
		wg.Wait()
		return fmt.Errorf("TUI error: %w", err)
	}

	// TUI has exited (user pressed Ctrl+C or agent finished).
	// Cancel the agent context so any in-flight network/tool call stops.
	cancel()

	// Drain inputCh in case agent is blocked on ReadInput.
	select {
	case inputCh <- inputResult{err: fmt.Errorf("interrupted")}:
	default:
	}

	wg.Wait()
	return agentErr
}
