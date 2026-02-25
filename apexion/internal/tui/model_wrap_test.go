package tui

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

func TestWrapByDisplayWidth_ASCII(t *testing.T) {
	lines := wrapByDisplayWidth("abcdefghijklmnopqrstuvwxyz", 10)
	if len(lines) < 3 {
		t.Fatalf("expected wrapped lines >= 3, got %d (%v)", len(lines), lines)
	}
	for _, ln := range lines {
		if runewidth.StringWidth(ln) > 10 {
			t.Fatalf("line width exceeds 10: %q", ln)
		}
	}
}

func TestWrapByDisplayWidth_CJK(t *testing.T) {
	lines := wrapByDisplayWidth("这是一个很长很长的中文输入内容用于测试自动换行", 12)
	if len(lines) < 2 {
		t.Fatalf("expected CJK text to wrap, got %v", lines)
	}
	for _, ln := range lines {
		if runewidth.StringWidth(ln) > 12 {
			t.Fatalf("line width exceeds 12: %q", ln)
		}
	}
}

func TestRenderWrappedInputPreview_EmptyForShortInput(t *testing.T) {
	got := renderWrappedInputPreview("short text", 40, 8)
	if got != "" {
		t.Fatalf("expected empty preview for short input, got %q", got)
	}
}

func TestRenderWrappedInputPreview_TruncatesHead(t *testing.T) {
	text := strings.Repeat("1234567890", 8) // wraps into many lines at width=10
	got := renderWrappedInputPreview(text, 10, 3)
	if got == "" {
		t.Fatal("expected non-empty preview")
	}
	if !strings.Contains(got, "… +") {
		t.Fatalf("expected truncation marker, got %q", got)
	}
}
