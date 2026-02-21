package session

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Memory represents a single piece of cross-session knowledge.
type Memory struct {
	ID        string   `json:"id"`
	Content   string   `json:"content"`
	Tags      []string `json:"tags"`
	Source    string   `json:"source"` // "manual" | "auto"
	CreatedAt time.Time `json:"created_at"`
	SessionID string   `json:"session_id,omitempty"`
}

// MemoryStore abstracts cross-session memory persistence.
type MemoryStore interface {
	Add(content string, tags []string, source, sessionID string) (*Memory, error)
	Search(query string, limit int) ([]Memory, error)
	List(limit int) ([]Memory, error)
	Delete(id string) error
	// LoadForPrompt returns memories suitable for injection into the system prompt,
	// filtered by optional project tag, capped at maxBytes.
	LoadForPrompt(projectTag string, maxBytes int) string
	Close() error
}

// NullMemoryStore is a no-op implementation.
type NullMemoryStore struct{}

func (NullMemoryStore) Add(string, []string, string, string) (*Memory, error) { return nil, nil }
func (NullMemoryStore) Search(string, int) ([]Memory, error)                  { return nil, nil }
func (NullMemoryStore) List(int) ([]Memory, error)                            { return nil, nil }
func (NullMemoryStore) Delete(string) error                                   { return nil }
func (NullMemoryStore) LoadForPrompt(string, int) string                      { return "" }
func (NullMemoryStore) Close() error                                          { return nil }

const createMemoryTableSQL = `
CREATE TABLE IF NOT EXISTS memories (
    id         TEXT PRIMARY KEY,
    content    TEXT NOT NULL,
    tags       TEXT DEFAULT '[]',
    source     TEXT DEFAULT 'manual',
    created_at TEXT NOT NULL,
    session_id TEXT DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_memories_created_at ON memories(created_at);
`

// SQLiteMemoryStore implements MemoryStore backed by SQLite.
type SQLiteMemoryStore struct {
	db *sql.DB
}

// NewSQLiteMemoryStore creates a memory store using an existing SQLite DB connection.
// The memories table is created if it doesn't exist.
func NewSQLiteMemoryStore(db *sql.DB) (*SQLiteMemoryStore, error) {
	if _, err := db.Exec(createMemoryTableSQL); err != nil {
		return nil, fmt.Errorf("create memories table: %w", err)
	}
	return &SQLiteMemoryStore{db: db}, nil
}

func (s *SQLiteMemoryStore) Add(content string, tags []string, source, sessionID string) (*Memory, error) {
	m := &Memory{
		ID:        uuid.New().String()[:8],
		Content:   content,
		Tags:      tags,
		Source:    source,
		CreatedAt: time.Now(),
		SessionID: sessionID,
	}

	tagsJSON, _ := json.Marshal(m.Tags)

	_, err := s.db.Exec(`
		INSERT INTO memories (id, content, tags, source, created_at, session_id)
		VALUES (?, ?, ?, ?, ?, ?)`,
		m.ID, m.Content, string(tagsJSON), m.Source,
		m.CreatedAt.Format(time.RFC3339Nano), m.SessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("insert memory: %w", err)
	}
	return m, nil
}

func (s *SQLiteMemoryStore) Search(query string, limit int) ([]Memory, error) {
	if limit <= 0 {
		limit = 20
	}

	// Keyword-based search: match content or tags containing the query.
	// Uses LIKE for simplicity — no embeddings needed.
	pattern := "%" + query + "%"
	rows, err := s.db.Query(`
		SELECT id, content, tags, source, created_at, session_id
		FROM memories
		WHERE content LIKE ? OR tags LIKE ?
		ORDER BY created_at DESC
		LIMIT ?`,
		pattern, pattern, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("search memories: %w", err)
	}
	defer rows.Close()

	return scanMemories(rows)
}

func (s *SQLiteMemoryStore) List(limit int) ([]Memory, error) {
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.Query(`
		SELECT id, content, tags, source, created_at, session_id
		FROM memories
		ORDER BY created_at DESC
		LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list memories: %w", err)
	}
	defer rows.Close()

	return scanMemories(rows)
}

func (s *SQLiteMemoryStore) Delete(id string) error {
	result, err := s.db.Exec("DELETE FROM memories WHERE id = ? OR id LIKE ?", id, id+"%")
	if err != nil {
		return fmt.Errorf("delete memory: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("memory %s not found", id)
	}
	return nil
}

// LoadForPrompt returns formatted memories for system prompt injection.
// Filters by "preference" tag and optionally a project-specific tag.
// Output is capped at maxBytes.
func (s *SQLiteMemoryStore) LoadForPrompt(projectTag string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = 2048
	}

	// Load all preference memories + project-specific memories.
	rows, err := s.db.Query(`
		SELECT id, content, tags, source, created_at, session_id
		FROM memories
		WHERE tags LIKE '%"preference"%' OR tags LIKE ?
		ORDER BY created_at DESC
		LIMIT 50`,
		"%\""+projectTag+"\"%",
	)
	if err != nil {
		return ""
	}
	defer rows.Close()

	memories, err := scanMemories(rows)
	if err != nil || len(memories) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("<persistent_memory>\n")
	for _, m := range memories {
		line := fmt.Sprintf("- %s", m.Content)
		if len(m.Tags) > 0 {
			line += fmt.Sprintf(" [%s]", strings.Join(m.Tags, ", "))
		}
		line += "\n"

		if sb.Len()+len(line) > maxBytes {
			break
		}
		sb.WriteString(line)
	}
	sb.WriteString("</persistent_memory>")
	return sb.String()
}

func (s *SQLiteMemoryStore) Close() error {
	// Don't close the DB — it's shared with the session store.
	return nil
}

// scanMemories reads memory rows from a query result.
func scanMemories(rows *sql.Rows) ([]Memory, error) {
	var memories []Memory
	for rows.Next() {
		var m Memory
		var tagsJSON, createdAt string
		if err := rows.Scan(&m.ID, &m.Content, &tagsJSON, &m.Source, &createdAt, &m.SessionID); err != nil {
			return nil, fmt.Errorf("scan memory: %w", err)
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		_ = json.Unmarshal([]byte(tagsJSON), &m.Tags)
		if m.Tags == nil {
			m.Tags = []string{}
		}
		memories = append(memories, m)
	}
	return memories, rows.Err()
}
