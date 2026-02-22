package tui

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/apexion-ai/apexion/internal/tools"
	tea "github.com/charmbracelet/bubbletea"
)

// TuiIO implements the IO interface by sending messages to a bubbletea Program.
// All methods are safe to call from any goroutine.
type TuiIO struct {
	program *tea.Program
	inputCh chan inputResult

	mu         sync.Mutex
	cancelTool context.CancelFunc
	cancelLoop context.CancelFunc
}

var _ IO = (*TuiIO)(nil)

// send is a nil-safe helper that sends a message to the bubbletea program.
// Fire-and-forget methods use this to avoid panicking when program is nil.
func (t *TuiIO) send(msg tea.Msg) {
	if t.program != nil {
		t.program.Send(msg)
	}
}

func (t *TuiIO) ReadInput() (string, error) {
	if t.program == nil {
		return "", io.EOF
	}
	// Tell the TUI to activate the text input
	t.program.Send(readInputMsg{})

	// Block until the user submits or the TUI exits
	res := <-t.inputCh
	if res.err != nil {
		return "", io.EOF
	}
	return res.text, nil
}

func (t *TuiIO) UserMessage(text string) {
	t.send(userMsg{text: text})
}

func (t *TuiIO) ThinkingStart() {
	t.send(thinkingStartMsg{})
}

func (t *TuiIO) TextDelta(delta string) {
	t.send(textDeltaMsg{delta: delta})
}

func (t *TuiIO) TextDone(fullText string) {
	t.send(textDoneMsg{fullText: fullText})
}

func (t *TuiIO) ToolStart(id, name, params string) {
	t.send(toolStartMsg{id: id, name: name, params: params})
}

func (t *TuiIO) ToolDone(id, name, result string, isErr bool) {
	t.send(toolDoneMsg{id: id, name: name, result: result, isErr: isErr})
}

func (t *TuiIO) Confirm(name, params string, level tools.PermissionLevel) bool {
	if t.program == nil {
		return false
	}
	replyCh := make(chan bool, 1)
	t.program.Send(confirmMsg{
		name:    name,
		params:  params,
		level:   level,
		replyCh: replyCh,
	})
	return <-replyCh
}

func (t *TuiIO) SystemMessage(text string) {
	t.send(systemMsg{text: text})
}

func (t *TuiIO) Error(msg string) {
	t.send(errorMsg{text: msg})
}

func (t *TuiIO) SetTokens(n int) {
	t.send(tokensMsg{n: n})
}

// --- Questioner implementation ---

func (t *TuiIO) AskQuestion(question string, options []string) (string, error) {
	if t.program == nil {
		return "", fmt.Errorf("no TUI program available")
	}
	replyCh := make(chan string, 1)
	t.program.Send(questionMsg{
		question: question,
		options:  options,
		replyCh:  replyCh,
	})
	answer, ok := <-replyCh
	if !ok {
		return "", fmt.Errorf("cancelled")
	}
	return answer, nil
}

// --- SubAgentReporter ---

// ReportSubAgentProgress sends sub-agent progress to the TUI for rendering.
func (t *TuiIO) ReportSubAgentProgress(p SubAgentProgress) {
	t.send(subAgentProgressMsg{progress: p})
}

// --- ToolCanceller implementation ---

// SetToolCancel registers the cancel function for the currently running tool.
func (t *TuiIO) SetToolCancel(cancel context.CancelFunc) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cancelTool = cancel
}

// ClearToolCancel clears the cancel function after the tool finishes.
func (t *TuiIO) ClearToolCancel() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cancelTool = nil
}

// CancelRunningTool cancels the currently running tool. Returns true if a
// tool was actually cancelled.
func (t *TuiIO) CancelRunningTool() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cancelTool != nil {
		t.cancelTool()
		t.cancelTool = nil
		return true
	}
	return false
}

// --- LoopCanceller implementation ---

// SetLoopCancel registers the per-turn cancel function for the agent loop.
func (t *TuiIO) SetLoopCancel(cancel context.CancelFunc) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cancelLoop = cancel
}

// ClearLoopCancel clears the loop cancel function when the turn ends.
func (t *TuiIO) ClearLoopCancel() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cancelLoop = nil
}

// CancelLoop cancels the entire agent loop (per-turn). Returns true if
// the loop was actually cancelled.
func (t *TuiIO) CancelLoop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cancelLoop != nil {
		t.cancelLoop()
		t.cancelLoop = nil
		return true
	}
	return false
}
