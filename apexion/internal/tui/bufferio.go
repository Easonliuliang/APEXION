package tui

import (
	"io"
	"strings"
	"sync"

	"github.com/apexion-ai/apexion/internal/tools"
)

// SubAgentProgress holds real-time progress info from a running sub-agent.
type SubAgentProgress struct {
	TaskID    string // tool call ID of the parent task tool
	ToolName  string // sub-agent's current/last tool
	ToolCount int    // total tool calls so far
	Done      bool   // true when the tool call finished
}

// BufferIO is a silent IO implementation that captures LLM text output
// without rendering to any terminal. Used by sub-agents.
type BufferIO struct {
	mu         sync.Mutex
	buf        strings.Builder
	taskID     string
	toolCount  int
	onProgress func(SubAgentProgress)
}

var _ IO = (*BufferIO)(nil)

// NewBufferIO creates a new BufferIO.
func NewBufferIO() *BufferIO {
	return &BufferIO{}
}

// NewBufferIOWithProgress creates a BufferIO that reports tool events
// back to the main TUI via the onProgress callback.
func NewBufferIOWithProgress(taskID string, onProgress func(SubAgentProgress)) *BufferIO {
	return &BufferIO{
		taskID:     taskID,
		onProgress: onProgress,
	}
}

// Output returns all captured text output.
func (b *BufferIO) Output() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *BufferIO) ReadInput() (string, error) { return "", io.EOF }
func (b *BufferIO) UserMessage(_ string)        {}
func (b *BufferIO) ThinkingStart()              {}

func (b *BufferIO) TextDelta(delta string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.WriteString(delta)
}

func (b *BufferIO) TextDone(_ string) {}

func (b *BufferIO) ToolStart(_, name, _ string) {
	if b.onProgress != nil {
		b.onProgress(SubAgentProgress{
			TaskID:    b.taskID,
			ToolName:  name,
			ToolCount: b.toolCount,
		})
	}
}

func (b *BufferIO) ToolDone(_, _, _ string, _ bool) {
	b.mu.Lock()
	b.toolCount++
	count := b.toolCount
	b.mu.Unlock()

	if b.onProgress != nil {
		b.onProgress(SubAgentProgress{
			TaskID:    b.taskID,
			ToolCount: count,
			Done:      true,
		})
	}
}

func (b *BufferIO) Confirm(_ string, _ string, _ tools.PermissionLevel) bool { return true }
func (b *BufferIO) SystemMessage(_ string)                                    {}
func (b *BufferIO) Error(_ string)                                            {}
func (b *BufferIO) SetTokens(_ int)                                           {}
