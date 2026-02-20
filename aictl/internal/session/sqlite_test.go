package session

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/aictl/aictl/internal/provider"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestSaveAndLoad(t *testing.T) {
	store := newTestStore(t)

	s := &Session{
		ID:               "abc123",
		CreatedAt:        time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		TokensUsed:       100,
		PromptTokens:     60,
		CompletionTokens: 40,
		Summary:          "test summary",
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: []provider.Content{{Type: provider.ContentTypeText, Text: "hello"}}},
			{Role: provider.RoleAssistant, Content: []provider.Content{{Type: provider.ContentTypeText, Text: "hi"}}},
		},
	}

	if err := store.Save(s); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := store.Load("abc123")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.ID != s.ID {
		t.Errorf("ID = %q, want %q", loaded.ID, s.ID)
	}
	if loaded.TokensUsed != 100 {
		t.Errorf("TokensUsed = %d, want 100", loaded.TokensUsed)
	}
	if loaded.PromptTokens != 60 {
		t.Errorf("PromptTokens = %d, want 60", loaded.PromptTokens)
	}
	if loaded.CompletionTokens != 40 {
		t.Errorf("CompletionTokens = %d, want 40", loaded.CompletionTokens)
	}
	if loaded.Summary != "test summary" {
		t.Errorf("Summary = %q, want %q", loaded.Summary, "test summary")
	}
	if len(loaded.Messages) != 2 {
		t.Fatalf("Messages len = %d, want 2", len(loaded.Messages))
	}
	if loaded.Messages[0].Content[0].Text != "hello" {
		t.Errorf("first message text = %q, want %q", loaded.Messages[0].Content[0].Text, "hello")
	}
	if loaded.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be set after Save")
	}
}

func TestLoadNotFound(t *testing.T) {
	store := newTestStore(t)

	_, err := store.Load("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestListOrderedByUpdatedAt(t *testing.T) {
	store := newTestStore(t)

	// Save two sessions with different timestamps.
	s1 := &Session{ID: "older", CreatedAt: time.Now().Add(-2 * time.Hour)}
	s2 := &Session{ID: "newer", CreatedAt: time.Now().Add(-1 * time.Hour)}

	if err := store.Save(s1); err != nil {
		t.Fatal(err)
	}
	// Small delay to ensure different updated_at.
	time.Sleep(10 * time.Millisecond)
	if err := store.Save(s2); err != nil {
		t.Fatal(err)
	}

	infos, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("List len = %d, want 2", len(infos))
	}
	// Newest first.
	if infos[0].ID != "newer" {
		t.Errorf("first session = %q, want %q", infos[0].ID, "newer")
	}
	if infos[1].ID != "older" {
		t.Errorf("second session = %q, want %q", infos[1].ID, "older")
	}
}

func TestDelete(t *testing.T) {
	store := newTestStore(t)

	s := &Session{ID: "del-me", CreatedAt: time.Now()}
	if err := store.Save(s); err != nil {
		t.Fatal(err)
	}

	if err := store.Delete("del-me"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := store.Load("del-me")
	if err == nil {
		t.Fatal("expected error after delete")
	}

	// Delete nonexistent should error.
	if err := store.Delete("nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent delete")
	}
}

func TestSaveUpdatesExisting(t *testing.T) {
	store := newTestStore(t)

	s := &Session{
		ID:        "update-me",
		CreatedAt: time.Now(),
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: []provider.Content{{Type: provider.ContentTypeText, Text: "v1"}}},
		},
	}
	if err := store.Save(s); err != nil {
		t.Fatal(err)
	}

	// Update and save again.
	s.Messages = append(s.Messages, provider.Message{
		Role: provider.RoleAssistant, Content: []provider.Content{{Type: provider.ContentTypeText, Text: "v2"}},
	})
	s.TokensUsed = 50
	if err := store.Save(s); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.Load("update-me")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Messages) != 2 {
		t.Errorf("Messages len = %d, want 2", len(loaded.Messages))
	}
	if loaded.TokensUsed != 50 {
		t.Errorf("TokensUsed = %d, want 50", loaded.TokensUsed)
	}

	// List should show correct message count.
	infos, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 {
		t.Fatalf("List len = %d, want 1", len(infos))
	}
	if infos[0].Messages != 2 {
		t.Errorf("List messages = %d, want 2", infos[0].Messages)
	}
}
