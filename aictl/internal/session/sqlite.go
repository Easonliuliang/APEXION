package session

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/aictl/aictl/internal/provider"
	_ "modernc.org/sqlite"
)

const createTableSQL = `
CREATE TABLE IF NOT EXISTS sessions (
    id                TEXT PRIMARY KEY,
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL,
    tokens_used       INTEGER DEFAULT 0,
    prompt_tokens     INTEGER DEFAULT 0,
    completion_tokens INTEGER DEFAULT 0,
    message_count     INTEGER DEFAULT 0,
    summary           TEXT DEFAULT '',
    messages          TEXT NOT NULL DEFAULT '[]'
);
CREATE INDEX IF NOT EXISTS idx_sessions_updated_at ON sessions(updated_at);
`

// SQLiteStore implements Store backed by a SQLite database.
type SQLiteStore struct {
	db *sql.DB
}

// DefaultDBPath returns the default database path (~/.local/share/aictl/sessions.db).
func DefaultDBPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "aictl", "sessions.db"), nil
}

// NewSQLiteStore opens (or creates) a SQLite database at dbPath and ensures the schema exists.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Enable WAL mode for better concurrent read performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	if _, err := db.Exec(createTableSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("create tables: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Save(sess *Session) error {
	sess.UpdatedAt = time.Now()

	msgJSON, err := json.Marshal(sess.Messages)
	if err != nil {
		return fmt.Errorf("marshal messages: %w", err)
	}

	_, err = s.db.Exec(`
		INSERT OR REPLACE INTO sessions
			(id, created_at, updated_at, tokens_used, prompt_tokens, completion_tokens, message_count, summary, messages)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.ID,
		sess.CreatedAt.Format(time.RFC3339Nano),
		sess.UpdatedAt.Format(time.RFC3339Nano),
		sess.TokensUsed,
		sess.PromptTokens,
		sess.CompletionTokens,
		len(sess.Messages),
		sess.Summary,
		string(msgJSON),
	)
	if err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Load(id string) (*Session, error) {
	row := s.db.QueryRow(`
		SELECT id, created_at, updated_at, tokens_used, prompt_tokens, completion_tokens, summary, messages
		FROM sessions WHERE id = ?`, id)

	var sess Session
	var createdAt, updatedAt, msgJSON string
	err := row.Scan(
		&sess.ID, &createdAt, &updatedAt,
		&sess.TokensUsed, &sess.PromptTokens, &sess.CompletionTokens,
		&sess.Summary, &msgJSON,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("session %s not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("load session: %w", err)
	}

	sess.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	sess.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)

	var msgs []provider.Message
	if err := json.Unmarshal([]byte(msgJSON), &msgs); err != nil {
		return nil, fmt.Errorf("unmarshal messages: %w", err)
	}
	sess.Messages = msgs

	return &sess, nil
}

func (s *SQLiteStore) List() ([]SessionInfo, error) {
	rows, err := s.db.Query(`
		SELECT id, created_at, updated_at, message_count, tokens_used
		FROM sessions ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var infos []SessionInfo
	for rows.Next() {
		var info SessionInfo
		var createdAt, updatedAt string
		if err := rows.Scan(&info.ID, &createdAt, &updatedAt, &info.Messages, &info.Tokens); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		info.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		info.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
		infos = append(infos, info)
	}
	return infos, rows.Err()
}

func (s *SQLiteStore) Delete(id string) error {
	result, err := s.db.Exec("DELETE FROM sessions WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("session %s not found", id)
	}
	return nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}
