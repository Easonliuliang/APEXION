package tui

import (
	"fmt"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
)

// RunTUI starts the bubbletea program in alt-screen mode and runs agentFn
// concurrently. It blocks until either the agent finishes or the user quits.
func RunTUI(cfg TUIConfig, agentFn func(io IO) error) error {
	inputCh := make(chan inputResult, 1)
	model := NewModel(inputCh, cfg)

	p := tea.NewProgram(model, tea.WithAltScreen())
	model.program = p

	tuiIO := &TuiIO{
		program: p,
		inputCh: inputCh,
	}

	var (
		agentErr error
		wg       sync.WaitGroup
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		agentErr = agentFn(tuiIO)
		// Signal the TUI that the agent is done
		p.Send(agentDoneMsg{err: agentErr})
	}()

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	// Wait for the agent goroutine to finish after TUI exits
	wg.Wait()

	return agentErr
}
