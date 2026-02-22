package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// FileChange records a single file operation made during a session.
type FileChange struct {
	Path      string
	Operation string // "created", "modified", "deleted"
	Tool      string // which tool made the change
	At        time.Time
}

// FileTracker accumulates file changes during a session.
type FileTracker struct {
	mu      sync.Mutex
	changes []FileChange
	seen    map[string]string // path -> latest operation
}

// NewFileTracker creates a new FileTracker.
func NewFileTracker() *FileTracker {
	return &FileTracker{
		seen: make(map[string]string),
	}
}

// Record adds a file change. Deduplicates by path (keeps latest operation).
func (ft *FileTracker) Record(path, operation, tool string) {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	ft.changes = append(ft.changes, FileChange{
		Path:      path,
		Operation: operation,
		Tool:      tool,
		At:        time.Now(),
	})
	ft.seen[path] = operation
}

// Changes returns all recorded changes.
func (ft *FileTracker) Changes() []FileChange {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	cp := make([]FileChange, len(ft.changes))
	copy(cp, ft.changes)
	return cp
}

// Summary returns a formatted summary of unique file changes.
// Returns empty string if no changes were recorded.
func (ft *FileTracker) Summary() string {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	if len(ft.seen) == 0 {
		return ""
	}

	// Group by operation.
	groups := map[string][]string{}
	for path, op := range ft.seen {
		groups[op] = append(groups[op], path)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Files changed: %d\n", len(ft.seen)))

	order := []string{"created", "modified", "deleted"}
	for _, op := range order {
		paths := groups[op]
		if len(paths) == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("\n  %s (%d):\n", op, len(paths)))
		for _, p := range paths {
			sb.WriteString("    " + p + "\n")
		}
	}

	return strings.TrimRight(sb.String(), "\n")
}

// Reset clears all tracked changes.
func (ft *FileTracker) Reset() {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.changes = nil
	ft.seen = make(map[string]string)
}

// trackFileChange extracts file path info from tool params and records changes.
func trackFileChange(tracker *FileTracker, toolName string, params json.RawMessage, isNewFile bool) {
	if tracker == nil {
		return
	}

	switch toolName {
	case "write_file":
		var p struct {
			FilePath string `json:"file_path"`
		}
		if json.Unmarshal(params, &p) == nil && p.FilePath != "" {
			op := "modified"
			if isNewFile {
				op = "created"
			}
			tracker.Record(p.FilePath, op, toolName)
		}

	case "edit_file":
		var p struct {
			FilePath string `json:"file_path"`
		}
		if json.Unmarshal(params, &p) == nil && p.FilePath != "" {
			tracker.Record(p.FilePath, "modified", toolName)
		}

	case "bash":
		var p struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(params, &p) == nil && p.Command != "" {
			// Best-effort: record bash commands that look like file modifications.
			// We don't parse exact file paths from bash — too unreliable.
			// Instead, just note the command was run.
			if looksLikeFileModification(p.Command) {
				tracker.Record("(bash) "+p.Command, "modified", toolName)
			}
		}
	}
}

// looksLikeFileModification returns true if a bash command likely modifies files.
func looksLikeFileModification(cmd string) bool {
	prefixes := []string{"rm ", "mv ", "cp ", "mkdir ", "touch ", "chmod ", "chown "}
	lower := strings.ToLower(strings.TrimSpace(cmd))
	for _, p := range prefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}
