package tui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/apexion-ai/apexion/internal/tools"
)

// PlainIO implements IO using plain terminal output (fmt.Print / bufio.Scanner).
// It preserves the exact behaviour of the original agent loop and is used
// when TUI mode is disabled or the terminal does not support raw mode.
type PlainIO struct {
	scanner *bufio.Scanner
	tokens  int
	mu      sync.Mutex // protects concurrent output during parallel tool execution
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
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Printf("\n%s\n  Executing %s...\n", strings.Repeat("-", 30), name)
}

func (p *PlainIO) ToolDone(_, _, result string, isErr bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
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

func (p *PlainIO) SetContextInfo(_, _ int) {}
func (p *PlainIO) SetPlanMode(_ bool)      {}
func (p *PlainIO) SetCost(_ float64)       {}

func (p *PlainIO) AskQuestion(question string, options []string) (string, error) {
	fmt.Printf("\n? %s\n", question)
	for i, opt := range options {
		fmt.Printf("  %d. %s\n", i+1, opt)
	}
	fmt.Print("Enter number (or type custom answer): ")
	var answer string
	fmt.Scanln(&answer)
	answer = strings.TrimSpace(answer)
	// If user typed a number, map to option.
	if len(answer) == 1 && answer[0] >= '1' && answer[0] <= '9' {
		idx := int(answer[0]-'0') - 1
		if idx >= 0 && idx < len(options) {
			return options[idx], nil
		}
	}
	if answer == "" {
		return "", fmt.Errorf("cancelled")
	}
	return answer, nil
}

// truncate shortens s to maxLen characters, appending "..." if cut.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
