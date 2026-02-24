package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// EventType classifies an event in the event stream.
type EventType string

const (
	EventUserMessage   EventType = "user_message"
	EventAssistantText EventType = "assistant_text"
	EventToolCall      EventType = "tool_call"
	EventToolResult    EventType = "tool_result"
	EventCompaction    EventType = "compaction"
	EventToolRoute     EventType = "tool_route"
	EventToolRepair    EventType = "tool_repair"
	EventError         EventType = "error"
	EventSessionStart  EventType = "session_start"
	EventSessionEnd    EventType = "session_end"
)

// Event is a single structured event in the event stream.
type Event struct {
	Type      EventType `json:"type"`
	Timestamp time.Time `json:"ts"`
	SessionID string    `json:"session_id"`
	Data      any       `json:"data,omitempty"`
}

// EventLogger writes structured JSONL events to a file.
type EventLogger struct {
	mu        sync.Mutex
	file      *os.File
	enc       *json.Encoder
	sessionID string
	logPath   string
}

// NewEventLogger creates a new event logger for the given session.
// Events are written to ~/.local/share/apexion/events/{session_id}.jsonl.
func NewEventLogger(sessionID string) (*EventLogger, error) {
	var lastErr error
	for _, dir := range eventLogDirs() {
		if err := os.MkdirAll(dir, 0755); err != nil {
			lastErr = fmt.Errorf("create events directory %s: %w", dir, err)
			continue
		}

		logPath := filepath.Join(dir, sessionID+".jsonl")
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			lastErr = fmt.Errorf("open event log %s: %w", logPath, err)
			continue
		}

		return &EventLogger{
			file:      f,
			enc:       json.NewEncoder(f),
			sessionID: sessionID,
			logPath:   logPath,
		}, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no writable events directory found")
	}
	return nil, lastErr
}

// eventLogDirs returns candidate directories in priority order.
// 1) APEXION_EVENTS_DIR (explicit override)
// 2) ~/.local/share/apexion/events (default)
// 3) $TMPDIR/apexion/events (fallback for restricted environments)
func eventLogDirs() []string {
	seen := make(map[string]bool)
	var dirs []string

	add := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir == "" || seen[dir] {
			return
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}

	add(os.Getenv("APEXION_EVENTS_DIR"))

	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, ".local", "share", "apexion", "events"))
	}

	add(filepath.Join(os.TempDir(), "apexion", "events"))
	return dirs
}

// Log writes an event to the JSONL file.
func (el *EventLogger) Log(evtType EventType, data any) {
	el.mu.Lock()
	defer el.mu.Unlock()

	evt := Event{
		Type:      evtType,
		Timestamp: time.Now(),
		SessionID: el.sessionID,
		Data:      data,
	}
	_ = el.enc.Encode(evt)
}

// Close flushes and closes the event log file.
func (el *EventLogger) Close() {
	el.mu.Lock()
	defer el.mu.Unlock()
	if el.file != nil {
		_ = el.file.Close()
		el.file = nil
	}
}

// ReadRecent reads the last n events from the log file.
func (el *EventLogger) ReadRecent(n int) ([]Event, error) {
	el.mu.Lock()
	path := el.logPath
	el.mu.Unlock()

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open event log: %w", err)
	}
	defer f.Close()

	var events []Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)
	for scanner.Scan() {
		var evt Event
		if json.Unmarshal(scanner.Bytes(), &evt) == nil {
			events = append(events, evt)
		}
	}

	if n > 0 && len(events) > n {
		events = events[len(events)-n:]
	}
	return events, nil
}

// FormatEvents formats events for display.
func FormatEvents(events []Event, title string) string {
	if len(events) == 0 {
		return "No events recorded."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s (%d events):\n", title, len(events)))
	for _, evt := range events {
		ts := evt.Timestamp.Format("15:04:05")
		dataStr := ""
		if evt.Data != nil {
			switch d := evt.Data.(type) {
			case string:
				dataStr = truncate(d, 80)
			case map[string]any:
				if name, ok := d["tool_name"].(string); ok {
					dataStr = name
				} else if intent, ok := d["intent"].(string); ok {
					dataStr = "intent=" + intent
				} else if text, ok := d["text"].(string); ok {
					dataStr = truncate(text, 80)
				}
			default:
				raw, _ := json.Marshal(d)
				dataStr = truncate(string(raw), 80)
			}
		}
		if dataStr != "" {
			sb.WriteString(fmt.Sprintf("  %s  %-16s  %s\n", ts, evt.Type, dataStr))
		} else {
			sb.WriteString(fmt.Sprintf("  %s  %s\n", ts, evt.Type))
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}
