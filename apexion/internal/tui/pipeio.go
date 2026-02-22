package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/apexion-ai/apexion/internal/tools"
)

// PipeIO implements IO for non-interactive pipe/CI mode.
// LLM text goes to stdout, diagnostics go to stderr.
// Confirm always returns true (auto-approve in pipe mode).
type PipeIO struct {
	format    string    // "text" or "jsonl"
	verbose   bool      // show tool calls on stderr
	printLast bool      // only output the final LLM text
	writer    io.Writer // stdout
	errW      io.Writer // stderr
	lastText  string    // last complete LLM text (for printLast mode)
}

// NewPipeIO creates a PipeIO instance.
func NewPipeIO(format string, verbose, printLast bool) *PipeIO {
	if format == "" {
		format = "text"
	}
	return &PipeIO{
		format:    format,
		verbose:   verbose,
		printLast: printLast,
		writer:    os.Stdout,
		errW:      os.Stderr,
	}
}

func (p *PipeIO) ReadInput() (string, error) { return "", io.EOF }
func (p *PipeIO) UserMessage(_ string)        {}
func (p *PipeIO) ThinkingStart()              {}

func (p *PipeIO) TextDelta(delta string) {
	if p.printLast {
		return // suppress streaming in printLast mode
	}
	if p.format == "jsonl" {
		return // jsonl emits full text on TextDone
	}
	fmt.Fprint(p.writer, delta)
}

func (p *PipeIO) TextDone(fullText string) {
	p.lastText = fullText
	if p.printLast {
		return // will be flushed at Flush()
	}
	if p.format == "jsonl" {
		p.emitJSONL("text", map[string]string{"content": fullText})
	} else {
		fmt.Fprintln(p.writer) // newline after streaming deltas
	}
}

func (p *PipeIO) ToolStart(id, name, params string) {
	if p.verbose {
		fmt.Fprintf(p.errW, "[tool] %s started\n", name)
	}
	if p.format == "jsonl" {
		p.emitJSONL("tool_start", map[string]string{"id": id, "name": name, "params": params})
	}
}

func (p *PipeIO) ToolDone(id, name, result string, isErr bool) {
	if p.verbose {
		status := "ok"
		if isErr {
			status = "error"
		}
		fmt.Fprintf(p.errW, "[tool] %s done (%s)\n", name, status)
	}
	if p.format == "jsonl" {
		p.emitJSONL("tool_done", map[string]any{
			"id": id, "name": name, "is_error": isErr,
			"result": truncatePipe(result, 4096),
		})
	}
}

func (p *PipeIO) Confirm(_ string, _ string, _ tools.PermissionLevel) bool {
	return true // auto-approve in pipe mode
}

func (p *PipeIO) SystemMessage(text string) {
	fmt.Fprintln(p.errW, text)
}

func (p *PipeIO) Error(msg string) {
	fmt.Fprintf(p.errW, "error: %s\n", msg)
}

func (p *PipeIO) SetTokens(_ int)         {}
func (p *PipeIO) SetContextInfo(_, _ int)  {}
func (p *PipeIO) SetPlanMode(_ bool)       {}
func (p *PipeIO) SetCost(_ float64)        {}

// Flush outputs the last LLM text when in printLast mode.
// Should be called after the agent finishes.
func (p *PipeIO) Flush() {
	if p.printLast && p.lastText != "" {
		fmt.Fprintln(p.writer, p.lastText)
	}
}

// emitJSONL writes a JSON line to stdout.
func (p *PipeIO) emitJSONL(eventType string, data any) {
	line, _ := json.Marshal(map[string]any{
		"type":      eventType,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data":      data,
	})
	fmt.Fprintln(p.writer, string(line))
}

// truncatePipe limits output size for JSONL events.
func truncatePipe(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...[truncated]"
}
