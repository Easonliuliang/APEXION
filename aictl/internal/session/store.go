package session

import "time"

// Store abstracts session persistence (SQLite, JSON, etc.).
type Store interface {
	Save(s *Session) error
	Load(id string) (*Session, error)
	List() ([]SessionInfo, error)
	Delete(id string) error
	Close() error
}

// SessionInfo is a lightweight summary of a saved session (for listing).
type SessionInfo struct {
	ID        string
	CreatedAt time.Time
	UpdatedAt time.Time
	Messages  int
	Tokens    int
}
