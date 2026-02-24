package agent

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestNewEventLogger(t *testing.T) {
	sessionID := fmt.Sprintf("test-%d", time.Now().UnixNano())
	logger, err := NewEventLogger(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		logger.Close()
		os.Remove(logger.logPath)
	}()

	if logger.sessionID != sessionID {
		t.Fatalf("expected session ID %q, got %q", sessionID, logger.sessionID)
	}
	if logger.logPath == "" {
		t.Fatal("expected non-empty log path")
	}
	if logger.file == nil {
		t.Fatal("expected non-nil file handle")
	}
}

func TestLogAndReadRecent(t *testing.T) {
	sessionID := fmt.Sprintf("test-%d", time.Now().UnixNano())
	logger, err := NewEventLogger(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		logger.Close()
		os.Remove(logger.logPath)
	}()

	logger.Log(EventSessionStart, "session started")
	logger.Log(EventUserMessage, "hello")
	logger.Log(EventAssistantText, "hi there")
	logger.Log(EventToolCall, map[string]any{"tool_name": "read_file"})
	logger.Log(EventToolResult, "file contents")

	// Read all events
	all, err := logger.ReadRecent(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 {
		t.Fatalf("expected 5 events, got %d", len(all))
	}

	// Read last 3
	recent, err := logger.ReadRecent(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 3 {
		t.Fatalf("expected 3 events, got %d", len(recent))
	}
	if recent[0].Type != EventAssistantText {
		t.Fatalf("expected first of last 3 to be %s, got %s",
			EventAssistantText, recent[0].Type)
	}
	if recent[1].Type != EventToolCall {
		t.Fatalf("expected second of last 3 to be %s, got %s",
			EventToolCall, recent[1].Type)
	}
	if recent[2].Type != EventToolResult {
		t.Fatalf("expected third of last 3 to be %s, got %s",
			EventToolResult, recent[2].Type)
	}
}

func TestLogEventFields(t *testing.T) {
	sessionID := fmt.Sprintf("test-%d", time.Now().UnixNano())
	logger, err := NewEventLogger(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		logger.Close()
		os.Remove(logger.logPath)
	}()

	before := time.Now()
	logger.Log(EventUserMessage, "test data")
	after := time.Now()

	events, err := logger.ReadRecent(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	evt := events[0]
	if evt.Type != EventUserMessage {
		t.Fatalf("expected type %s, got %s", EventUserMessage, evt.Type)
	}
	if evt.SessionID != sessionID {
		t.Fatalf("expected session %q, got %q", sessionID, evt.SessionID)
	}
	if evt.Timestamp.Before(before) || evt.Timestamp.After(after) {
		t.Fatalf("timestamp %v not between %v and %v", evt.Timestamp, before, after)
	}
}

func TestFormatEventsEmpty(t *testing.T) {
	if s := FormatEvents(nil, "Test"); s != "No events recorded." {
		t.Fatalf("expected 'No events recorded.', got %q", s)
	}
	if s := FormatEvents([]Event{}, "Test"); s != "No events recorded." {
		t.Fatalf("expected 'No events recorded.' for empty slice, got %q", s)
	}
}

func TestFormatEvents(t *testing.T) {
	now := time.Now()
	events := []Event{
		{Type: EventUserMessage, Timestamp: now, SessionID: "s1", Data: "hello world"},
		{Type: EventToolCall, Timestamp: now, SessionID: "s1", Data: map[string]any{"tool_name": "read_file"}},
		{Type: EventSessionStart, Timestamp: now, SessionID: "s1"},
	}

	output := FormatEvents(events, "Recent Events")
	if !strings.Contains(output, "Recent Events") {
		t.Error("output should contain title")
	}
	if !strings.Contains(output, "3 events") {
		t.Error("output should contain event count")
	}
	if !strings.Contains(output, string(EventUserMessage)) {
		t.Error("output should contain user_message type")
	}
	if !strings.Contains(output, "hello world") {
		t.Error("output should contain string data")
	}
	if !strings.Contains(output, "read_file") {
		t.Error("output should extract tool_name from map data")
	}
	if !strings.Contains(output, string(EventSessionStart)) {
		t.Error("output should contain session_start type")
	}
}

func TestFormatEventsWithTextMap(t *testing.T) {
	events := []Event{
		{Type: EventAssistantText, Timestamp: time.Now(), SessionID: "s1",
			Data: map[string]any{"text": "This is a response from the assistant"}},
	}

	output := FormatEvents(events, "Test")
	if !strings.Contains(output, "This is a response") {
		t.Error("output should extract text from map data")
	}
}

func TestCloseIdempotent(t *testing.T) {
	sessionID := fmt.Sprintf("test-%d", time.Now().UnixNano())
	logger, err := NewEventLogger(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(logger.logPath)

	// Close twice should not panic
	logger.Close()
	logger.Close()
}
