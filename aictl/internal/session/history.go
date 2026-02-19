package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// sessionDir returns the base directory for session storage.
func sessionDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "aictl", "sessions"), nil
}

// Save persists a session to disk as JSON.
func Save(s *Session) error {
	dir, err := sessionDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}

	path := filepath.Join(dir, s.ID+".json")
	return os.WriteFile(path, data, 0644)
}

// Load reads a session from disk by ID.
func Load(id string) (*Session, error) {
	dir, err := sessionDir()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		return nil, fmt.Errorf("load session %s: %w", id, err)
	}

	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}
	return &s, nil
}
