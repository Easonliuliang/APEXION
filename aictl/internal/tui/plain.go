package tui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/aictl/aictl/internal/tools"
)

// PlainIO implements IO using plain terminal output (fmt.Print / bufio.Scanner).
// It preserves the exact behaviour of the original agent loop and is used
// when TUI mode is disabled or the terminal does not support raw mode.
type PlainIO struct {
	scanner *bufio.Scanner
	tokens  int
}

// NewPlainIO creates a PlainIO that reads from stdin.
func NewPlainIO() *PlainIO {
	s := bufio.NewScanner(os.Stdin)
	s.Buffer(make([]byte, 1024*1024), 1024*1024)
	return &PlainIO{scanner: s}
}

func (p *PlainIO) ReadInput() (string, error) {
	fmt.Print("\n> ")
	if !p.scanner.Scan() {
		if err := p.scanner.Err(); err != nil {
			return "", err
		}
		return "", io.EOF
	}
	return strings.TrimSpace(p.scanner.Text()), nil
}

func (p *PlainIO) UserMessage(_ string) {
	// Plain terminal: the user already sees what they typed.
}

func (p *PlainIO) ThinkingStart() {
	fmt.Println() // blank line before AI output begins
}

func (p *PlainIO) TextDelta(delta string) {
	fmt.Print(delta)
}

func (p *PlainIO) TextDone(_ string) {
	// Plain terminal: text is already rendered incrementally.
}

func (p *PlainIO) ToolStart(_, name, _ string) {
	fmt.Printf("\n%s\n  Executing %s...\n", strings.Repeat("-", 30), name)
}

func (p *PlainIO) ToolDone(_, _, result string, isErr bool) {
	if isErr {
		fmt.Printf("    Error: %s\n", truncate(result, 80))
	} else {
		preview := truncate(strings.ReplaceAll(result, "\n", " "), 60)
		fmt.Printf("    Result: %s\n", preview)
	}
}

func (p *PlainIO) Confirm(name, params string, level tools.PermissionLevel) bool {
	display := params
	if len(display) > 200 {
		display = display[:200] + "..."
	}
	if level >= tools.PermissionDangerous {
		fmt.Printf("\n⚠  WARNING — DANGEROUS OPERATION\n")
	}
	fmt.Printf("\n--- Tool: %s ---\n%s\n[y/N] ", name, display)
	var answer string
	fmt.Scanln(&answer)
	return strings.ToLower(strings.TrimSpace(answer)) == "y"
}

func (p *PlainIO) SystemMessage(text string) {
	fmt.Println(text)
}

func (p *PlainIO) Error(msg string) {
	fmt.Fprintf(os.Stderr, "error: %s\n", msg)
}

func (p *PlainIO) SetTokens(n int) {
	p.tokens = n
}

// truncate shortens s to maxLen characters, appending "..." if cut.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
