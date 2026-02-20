package tui

import (
	"io"
	"strings"
	"sync"

	"github.com/aictl/aictl/internal/tools"
)

// BufferIO is a silent IO implementation that captures LLM text output
// without rendering to any terminal. Used by sub-agents.
type BufferIO struct {
	mu  sync.Mutex
	buf strings.Builder
}

var _ IO = (*BufferIO)(nil)

// NewBufferIO creates a new BufferIO.
func NewBufferIO() *BufferIO {
	return &BufferIO{}
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

func (b *BufferIO) ToolStart(_, _, _ string)          {}
func (b *BufferIO) ToolDone(_, _, _ string, _ bool)   {}
func (b *BufferIO) Confirm(_ string, _ string, _ tools.PermissionLevel) bool { return true }
func (b *BufferIO) SystemMessage(_ string)             {}
func (b *BufferIO) Error(_ string)                     {}
func (b *BufferIO) SetTokens(_ int)                    {}
