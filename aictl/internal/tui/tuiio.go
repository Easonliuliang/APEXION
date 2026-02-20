package tui

import (
	"context"
	"io"
	"sync"

	"github.com/aictl/aictl/internal/tools"
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

func (t *TuiIO) ReadInput() (string, error) {
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
	t.program.Send(userMsg{text: text})
}

func (t *TuiIO) ThinkingStart() {
	t.program.Send(thinkingStartMsg{})
}

func (t *TuiIO) TextDelta(delta string) {
	t.program.Send(textDeltaMsg{delta: delta})
}

func (t *TuiIO) TextDone(fullText string) {
	t.program.Send(textDoneMsg{fullText: fullText})
}

func (t *TuiIO) ToolStart(id, name, params string) {
	t.program.Send(toolStartMsg{id: id, name: name, params: params})
}

func (t *TuiIO) ToolDone(id, name, result string, isErr bool) {
	t.program.Send(toolDoneMsg{id: id, name: name, result: result, isErr: isErr})
}

func (t *TuiIO) Confirm(name, params string, level tools.PermissionLevel) bool {
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
	t.program.Send(systemMsg{text: text})
}

func (t *TuiIO) Error(msg string) {
	t.program.Send(errorMsg{text: msg})
}

func (t *TuiIO) SetTokens(n int) {
	t.program.Send(tokensMsg{n: n})
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
