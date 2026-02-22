package session

import (
	"crypto/rand"
	"fmt"
	"time"

	"github.com/apexion-ai/apexion/internal/provider"
)

// Session holds the conversation state for one agent session.
type Session struct {
	ID               string
	Messages         []provider.Message
	CreatedAt        time.Time
	UpdatedAt        time.Time
	TokensUsed       int    // cumulative total tokens (never reset)
	PromptTokens     int    // last API call's input tokens (for threshold checks)
	CompletionTokens int    // last API call's output tokens
	Summary           string // compaction summary (empty = not yet compacted)
	GentleCompactDone bool   // runtime-only: true after stage-1 masking (not persisted) [DEPRECATED: use GentleCompactPhase]
	GentleCompactPhase int   // runtime-only: 0=none, 1=low masked, 2=low+mid masked (not persisted)
}

// New creates a new session with a unique ID.
func New() *Session {
	now := time.Now()
	return &Session{
		ID:        newID(),
		Messages:  nil,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// AddMessage appends a message to the session history.
func (s *Session) AddMessage(msg provider.Message) {
	s.Messages = append(s.Messages, msg)
}

// Clear resets the message history and token counter.
func (s *Session) Clear() {
	s.Messages = nil
	s.TokensUsed = 0
}

// EstimateTokens returns a rough token estimate (total chars / 4).
func (s *Session) EstimateTokens() int {
	total := 0
	for _, msg := range s.Messages {
		for _, c := range msg.Content {
			total += len(c.Text)
			total += len(c.ToolResult)
			total += len(c.ToolInput)
		}
	}
	return total / 4
}
