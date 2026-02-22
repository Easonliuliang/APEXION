package session

import (
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSQLiteMemoryStore_AddAndList(t *testing.T) {
	db := openTestDB(t)
	ms, err := NewSQLiteMemoryStore(db)
	if err != nil {
		t.Fatal(err)
	}

	m, err := ms.Add("prefer snake_case", []string{"preference", "style"}, "manual", "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if m.ID == "" {
		t.Error("expected non-empty ID")
	}
	if m.Content != "prefer snake_case" {
		t.Errorf("Content = %q, want %q", m.Content, "prefer snake_case")
	}
	if len(m.Tags) != 2 || m.Tags[0] != "preference" {
		t.Errorf("Tags = %v, want [preference, style]", m.Tags)
	}

	// Add another.
	_, err = ms.Add("use Go 1.22+", []string{"preference"}, "manual", "sess-1")
	if err != nil {
		t.Fatal(err)
	}

	// List.
	all, err := ms.List(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("List returned %d, want 2", len(all))
	}
	// Most recent first.
	if all[0].Content != "use Go 1.22+" {
		t.Errorf("most recent = %q, want %q", all[0].Content, "use Go 1.22+")
	}
}

func TestSQLiteMemoryStore_Search(t *testing.T) {
	db := openTestDB(t)
	ms, err := NewSQLiteMemoryStore(db)
	if err != nil {
		t.Fatal(err)
	}

	ms.Add("prefer snake_case for Go", []string{"preference"}, "manual", "")
	ms.Add("use pytest for Python tests", []string{"tool"}, "manual", "")
	ms.Add("project uses React 18", []string{"project:myapp"}, "manual", "")

	// Search by content.
	results, err := ms.Search("snake_case", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("search 'snake_case' returned %d, want 1", len(results))
	}

	// Search by tag.
	results, err = ms.Search("preference", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("search 'preference' returned %d, want 1", len(results))
	}

	// Search with no matches.
	results, err = ms.Search("nonexistent", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("search 'nonexistent' returned %d, want 0", len(results))
	}
}

func TestSQLiteMemoryStore_Delete(t *testing.T) {
	db := openTestDB(t)
	ms, err := NewSQLiteMemoryStore(db)
	if err != nil {
		t.Fatal(err)
	}

	m, _ := ms.Add("to be deleted", nil, "manual", "")

	err = ms.Delete(m.ID)
	if err != nil {
		t.Fatal(err)
	}

	all, _ := ms.List(10)
	if len(all) != 0 {
		t.Errorf("expected 0 after delete, got %d", len(all))
	}
}

func TestSQLiteMemoryStore_DeleteNotFound(t *testing.T) {
	db := openTestDB(t)
	ms, err := NewSQLiteMemoryStore(db)
	if err != nil {
		t.Fatal(err)
	}

	err = ms.Delete("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent ID")
	}
}

func TestSQLiteMemoryStore_LoadForPrompt(t *testing.T) {
	db := openTestDB(t)
	ms, err := NewSQLiteMemoryStore(db)
	if err != nil {
		t.Fatal(err)
	}

	ms.Add("prefer concise code", []string{"preference"}, "manual", "")
	ms.Add("project uses React", []string{"project:myapp"}, "manual", "")
	ms.Add("unrelated tool note", []string{"tool"}, "manual", "")

	// Load for "project:myapp" — should get preference + project memories.
	prompt := ms.LoadForPrompt("project:myapp", 2048)
	if !strings.Contains(prompt, "<persistent_memory>") {
		t.Errorf("expected <persistent_memory> tag, got %q", prompt)
	}
	if !strings.Contains(prompt, "prefer concise code") {
		t.Errorf("expected preference memory, got %q", prompt)
	}
	if !strings.Contains(prompt, "project uses React") {
		t.Errorf("expected project memory, got %q", prompt)
	}
	// Tool-only memory should NOT be included.
	if strings.Contains(prompt, "unrelated tool note") {
		t.Errorf("should not include non-preference/non-project memory, got %q", prompt)
	}
}

func TestSQLiteMemoryStore_LoadForPrompt_Empty(t *testing.T) {
	db := openTestDB(t)
	ms, err := NewSQLiteMemoryStore(db)
	if err != nil {
		t.Fatal(err)
	}

	prompt := ms.LoadForPrompt("project:x", 2048)
	if prompt != "" {
		t.Errorf("expected empty prompt for empty store, got %q", prompt)
	}
}

func TestSQLiteMemoryStore_LoadForPrompt_MaxBytes(t *testing.T) {
	db := openTestDB(t)
	ms, err := NewSQLiteMemoryStore(db)
	if err != nil {
		t.Fatal(err)
	}

	// Add many preference memories.
	for i := 0; i < 20; i++ {
		ms.Add(strings.Repeat("x", 100), []string{"preference"}, "manual", "")
	}

	prompt := ms.LoadForPrompt("", 500)
	if len(prompt) > 600 { // some slack for tags
		t.Errorf("prompt should be capped near 500 bytes, got %d", len(prompt))
	}
}

func TestNullMemoryStore(t *testing.T) {
	var ms NullMemoryStore

	m, err := ms.Add("test", nil, "manual", "")
	if err != nil || m != nil {
		t.Errorf("NullMemoryStore.Add should return nil, nil")
	}

	results, err := ms.Search("test", 10)
	if err != nil || results != nil {
		t.Errorf("NullMemoryStore.Search should return nil, nil")
	}

	prompt := ms.LoadForPrompt("", 2048)
	if prompt != "" {
		t.Errorf("NullMemoryStore.LoadForPrompt should return empty")
	}
}
