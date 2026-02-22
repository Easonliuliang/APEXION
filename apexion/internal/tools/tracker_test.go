package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFileTracker_Record(t *testing.T) {
	ft := NewFileTracker()

	ft.Record("/tmp/foo.go", "created", "write_file")
	ft.Record("/tmp/bar.go", "modified", "edit_file")
	ft.Record("/tmp/foo.go", "modified", "edit_file") // update existing

	changes := ft.Changes()
	if len(changes) != 3 {
		t.Errorf("expected 3 changes, got %d", len(changes))
	}

	// Summary should deduplicate: foo.go latest is "modified", bar.go is "modified".
	summary := ft.Summary()
	if !strings.Contains(summary, "Files changed: 2") {
		t.Errorf("expected 2 unique files, got summary: %s", summary)
	}
	if !strings.Contains(summary, "/tmp/foo.go") {
		t.Error("summary missing foo.go")
	}
}

func TestFileTracker_Summary_Empty(t *testing.T) {
	ft := NewFileTracker()
	if ft.Summary() != "" {
		t.Error("expected empty summary for no changes")
	}
}

func TestFileTracker_Reset(t *testing.T) {
	ft := NewFileTracker()
	ft.Record("/tmp/foo.go", "created", "write_file")
	ft.Reset()

	if len(ft.Changes()) != 0 {
		t.Error("expected no changes after reset")
	}
	if ft.Summary() != "" {
		t.Error("expected empty summary after reset")
	}
}

func TestTrackFileChange_WriteFile(t *testing.T) {
	ft := NewFileTracker()

	params, _ := json.Marshal(map[string]string{"file_path": "/tmp/new.go"})
	trackFileChange(ft, "write_file", params, true)

	changes := ft.Changes()
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Operation != "created" {
		t.Errorf("expected 'created', got %q", changes[0].Operation)
	}

	// Existing file case.
	trackFileChange(ft, "write_file", params, false)
	changes = ft.Changes()
	if changes[1].Operation != "modified" {
		t.Errorf("expected 'modified' for existing file, got %q", changes[1].Operation)
	}
}

func TestTrackFileChange_EditFile(t *testing.T) {
	ft := NewFileTracker()

	params, _ := json.Marshal(map[string]string{"file_path": "/tmp/edit.go"})
	trackFileChange(ft, "edit_file", params, false)

	changes := ft.Changes()
	if len(changes) != 1 || changes[0].Operation != "modified" {
		t.Errorf("expected 1 'modified' change, got %+v", changes)
	}
}

func TestTrackFileChange_Bash(t *testing.T) {
	ft := NewFileTracker()

	// File-modifying command.
	params, _ := json.Marshal(map[string]string{"command": "rm -rf /tmp/old"})
	trackFileChange(ft, "bash", params, false)

	if len(ft.Changes()) != 1 {
		t.Error("expected rm command to be tracked")
	}

	// Non-modifying command.
	ft.Reset()
	params, _ = json.Marshal(map[string]string{"command": "go test ./..."})
	trackFileChange(ft, "bash", params, false)

	if len(ft.Changes()) != 0 {
		t.Error("expected go test to not be tracked")
	}
}

func TestTrackFileChange_NilTracker(t *testing.T) {
	// Should not panic.
	params, _ := json.Marshal(map[string]string{"file_path": "/tmp/foo.go"})
	trackFileChange(nil, "write_file", params, true)
}

func TestTrackFileChange_ReadOnlyTool(t *testing.T) {
	ft := NewFileTracker()

	params, _ := json.Marshal(map[string]string{"file_path": "/tmp/foo.go"})
	trackFileChange(ft, "read_file", params, false)

	if len(ft.Changes()) != 0 {
		t.Error("read_file should not be tracked")
	}
}

func TestLooksLikeFileModification(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"rm -rf /tmp", true},
		{"mv foo bar", true},
		{"cp src dst", true},
		{"mkdir -p /tmp/new", true},
		{"touch /tmp/file", true},
		{"go test ./...", false},
		{"git status", false},
		{"echo hello", false},
	}
	for _, tt := range tests {
		if got := looksLikeFileModification(tt.cmd); got != tt.want {
			t.Errorf("looksLikeFileModification(%q) = %v, want %v", tt.cmd, got, tt.want)
		}
	}
}
