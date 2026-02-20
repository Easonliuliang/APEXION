package tui

import (
	"fmt"
	"strings"

	"github.com/aictl/aictl/internal/tools"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// ---------- messages sent from agent goroutine via program.Send() ----------

type readInputMsg struct{}

type inputResult struct {
	text string
	err  error
}

type userMsg struct{ text string }
type thinkingStartMsg struct{}
type textDeltaMsg struct{ delta string }
type textDoneMsg struct{ fullText string }
type toolStartMsg struct{ id, name, params string }
type toolDoneMsg struct {
	id, name, result string
	isErr            bool
}
type confirmMsg struct {
	name    string
	params  string
	level   tools.PermissionLevel
	replyCh chan bool
}
type systemMsg struct{ text string }
type errorMsg struct{ text string }
type tokensMsg struct{ n int }
type agentDoneMsg struct{ err error }

// ---------- spinner activity kinds ----------

type spinnerKind int

const (
	spinnerNone     spinnerKind = iota
	spinnerThinking             // LLM is thinking
	spinnerTool                 // tool is executing
)

// ---------- current tool call state ----------

type toolCallState struct {
	name   string
	params string
}

// ---------- styles ----------

var (
	statusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("235")).
			Foreground(lipgloss.Color("252")).
			Padding(0, 1)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true)

	systemStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Italic(true)

	userStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")).
			Bold(true)

	spinnerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")) // gray spinner

	// Tool call styles — minimalist gray left line
	toolBorderStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(lipgloss.Color("7")).
			PaddingLeft(1)

	toolNameStyle = lipgloss.NewStyle().
			Bold(true)

	toolParamStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")) // light gray

	toolSuccessStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("2")) // green

	toolErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")) // dark red

	// Confirm styles
	confirmBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder(), false, false, false, true).
				BorderForeground(lipgloss.Color("3")). // yellow left line
				PaddingLeft(1)

	confirmDangerBorderStyle = lipgloss.NewStyle().
					Border(lipgloss.NormalBorder(), false, false, false, true).
					BorderForeground(lipgloss.Color("9")). // red left line
					PaddingLeft(1)

	confirmHintStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("3")). // yellow
				Bold(true)

	confirmDangerHintStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("9")). // red
				Bold(true)
)

// ---------- Model ----------

const statusBarHeight = 1
const inputHeight = 1

// Model is the bubbletea model managing the full TUI state.
type Model struct {
	viewport  viewport.Model
	textinput textinput.Model
	spinner   spinner.Model
	width     int
	height    int

	content     strings.Builder // accumulated output
	streaming   bool            // LLM text deltas are arriving
	streamStart int             // byte offset in content where current stream began
	inputMode   bool            // text input is active (waiting for user)
	spinnerKind spinnerKind     // what the spinner is showing for

	currentTool *toolCallState // in-flight tool call (nil when idle)

	confirming   bool                 // waiting for confirmation
	confirmCh    chan bool             // send user's answer back to agent goroutine
	confirmLevel tools.PermissionLevel // permission level of current confirmation

	inputCh chan inputResult // send user input back to ReadInput()

	quitting bool

	// status bar
	tokens   int
	toolName string // current tool being executed (empty = none)

	program *tea.Program
}

// NewModel creates the initial bubbletea model.
func NewModel(inputCh chan inputResult) Model {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.CharLimit = 4096

	vp := viewport.New(80, 24)

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = spinnerStyle

	return Model{
		viewport:  vp,
		textinput: ti,
		spinner:   sp,
		inputCh:   inputCh,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.spinner.Tick)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		vpHeight := m.height - statusBarHeight - inputHeight
		if vpHeight < 1 {
			vpHeight = 1
		}
		m.viewport.Width = m.width
		m.viewport.Height = vpHeight
		m.textinput.Width = m.width - 4 // account for prompt
		m.viewport.SetContent(m.renderContent())
		m.viewport.GotoBottom()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.spinnerKind != spinnerNone {
			m.viewport.SetContent(m.renderContent())
			m.viewport.GotoBottom()
		}
		cmds = append(cmds, cmd)

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			if m.confirming && m.confirmCh != nil {
				m.confirmCh <- false
				m.confirming = false
				m.confirmCh = nil
				m.appendLine(systemStyle.Render("  [cancelled]"))
			} else if m.inputMode {
				m.inputCh <- inputResult{err: fmt.Errorf("interrupted")}
				m.inputMode = false
				m.textinput.Blur()
			}
			m.quitting = true
			return m, tea.Quit
		case "enter":
			if m.confirming && m.confirmCh != nil {
				// Enter = approve
				m.confirmCh <- true
				m.confirming = false
				m.confirmCh = nil
				m.appendLine(toolSuccessStyle.Render("  ✓ allowed"))
				return m, nil
			}
			if m.inputMode {
				text := strings.TrimSpace(m.textinput.Value())
				m.textinput.SetValue("")
				m.inputCh <- inputResult{text: text}
				m.inputMode = false
				m.textinput.Blur()
			}
			return m, nil
		case "esc":
			if m.confirming && m.confirmCh != nil {
				// Esc = deny
				m.confirmCh <- false
				m.confirming = false
				m.confirmCh = nil
				m.appendLine(toolErrorStyle.Render("  ✗ denied"))
				return m, nil
			}
		}

		if m.inputMode && !m.confirming {
			var cmd tea.Cmd
			m.textinput, cmd = m.textinput.Update(msg)
			cmds = append(cmds, cmd)
		}

	// ---------- custom messages from agent goroutine ----------

	case readInputMsg:
		m.inputMode = true
		m.textinput.Focus()
		cmds = append(cmds, textinput.Blink)

	case userMsg:
		m.appendLine(userStyle.Render("You: " + msg.text))

	case thinkingStartMsg:
		m.spinnerKind = spinnerThinking
		m.streaming = false

	case textDeltaMsg:
		if m.spinnerKind == spinnerThinking {
			m.spinnerKind = spinnerNone
		}
		if !m.streaming {
			// Record where this response starts so TextDone can replace it
			m.streamStart = m.content.Len()
			m.streaming = true
		}
		m.appendText(msg.delta)

	case textDoneMsg:
		m.spinnerKind = spinnerNone
		if m.streaming {
			m.replaceStreamWithMarkdown(msg.fullText)
		}
		m.streaming = false

	case toolStartMsg:
		m.toolName = msg.name
		m.spinnerKind = spinnerTool
		m.currentTool = &toolCallState{
			name:   msg.name,
			params: formatToolParams(msg.params),
		}

	case toolDoneMsg:
		// Render the completed tool block into content
		if m.currentTool != nil {
			m.appendLine(m.renderToolDone(m.currentTool, msg.result, msg.isErr))
		}
		m.toolName = ""
		m.spinnerKind = spinnerNone
		m.currentTool = nil

	case confirmMsg:
		m.confirming = true
		m.confirmCh = msg.replyCh
		m.confirmLevel = msg.level
		m.spinnerKind = spinnerNone
		m.appendLine(m.renderConfirmBlock(msg.name, msg.params, msg.level))

	case systemMsg:
		m.appendLine(systemStyle.Render(msg.text))

	case errorMsg:
		m.appendLine(errorStyle.Render("Error: " + msg.text))

	case tokensMsg:
		m.tokens = msg.n

	case agentDoneMsg:
		m.quitting = true
		return m, tea.Quit
	}

	// Update viewport
	m.viewport.SetContent(m.renderContent())
	m.viewport.GotoBottom()

	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	cmds = append(cmds, vpCmd)

	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}

	// Status bar
	status := fmt.Sprintf(" tokens: %d", m.tokens)
	if m.toolName != "" {
		status += fmt.Sprintf(" | tool: %s", m.toolName)
	}
	bar := statusBarStyle.Width(m.width).Render(status)

	// Input line
	var input string
	if m.confirming {
		if m.confirmLevel >= tools.PermissionDangerous {
			input = confirmDangerHintStyle.Render("  ⚠ Enter = allow • Esc = deny")
		} else {
			input = confirmHintStyle.Render("  Enter = allow • Esc = deny")
		}
	} else if m.inputMode {
		input = m.textinput.View()
	} else {
		input = ""
	}

	return m.viewport.View() + "\n" + bar + "\n" + input
}

// ---------- tool call rendering ----------

// renderToolRunning renders an in-flight tool call block with spinner.
func (m *Model) renderToolRunning(tc *toolCallState) string {
	name := toolNameStyle.Render(tc.name)
	params := toolParamStyle.Render(tc.params)
	status := m.spinner.View() + " running..."
	inner := name + "\n" + params + "\n" + status
	return toolBorderStyle.Render(inner)
}

// renderToolDone renders a completed tool call block.
func (m *Model) renderToolDone(tc *toolCallState, result string, isErr bool) string {
	name := toolNameStyle.Render(tc.name)
	params := toolParamStyle.Render(tc.params)
	var status string
	if isErr {
		status = toolErrorStyle.Render("✗ " + truncateStr(result, 200))
	} else {
		summary := truncateStr(strings.ReplaceAll(result, "\n", " "), 120)
		status = toolSuccessStyle.Render("✓ " + summary)
	}
	inner := name + "\n" + params + "\n" + status
	return toolBorderStyle.Render(inner)
}

// renderConfirmBlock renders the permission confirmation as a styled inline block.
func (m *Model) renderConfirmBlock(name, params string, level tools.PermissionLevel) string {
	toolName := toolNameStyle.Render(name)
	display := formatToolParams(params)
	paramLine := toolParamStyle.Render(display)

	border := confirmBorderStyle
	if level >= tools.PermissionDangerous {
		border = confirmDangerBorderStyle
	}

	var lines []string
	lines = append(lines, toolName)
	lines = append(lines, paramLine)
	if level >= tools.PermissionDangerous {
		lines = append(lines, "")
		lines = append(lines, confirmDangerHintStyle.Render("⚠ DANGEROUS"))
	}

	return border.Render(strings.Join(lines, "\n"))
}

// renderContent returns the viewport content, appending dynamic elements
// (spinner, in-flight tool block) that are not persisted in the content builder.
func (m *Model) renderContent() string {
	base := m.content.String()
	switch m.spinnerKind {
	case spinnerThinking:
		return base + "\n" + m.spinner.View() + " Thinking..."
	case spinnerTool:
		if m.currentTool != nil {
			return base + "\n" + m.renderToolRunning(m.currentTool)
		}
		return base + "\n" + m.spinner.View() + " " + m.toolName + "..."
	default:
		return base
	}
}

// ---------- markdown rendering ----------

// replaceStreamWithMarkdown replaces the raw streamed text (from streamStart
// to end of content) with glamour-rendered markdown.
func (m *Model) replaceStreamWithMarkdown(fullText string) {
	width := m.width
	if width <= 0 {
		width = 80
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width-4),
	)
	if err != nil {
		// Fallback: keep raw text, just ensure trailing newline
		s := m.content.String()
		if len(s) > 0 && s[len(s)-1] != '\n' {
			m.content.WriteString("\n")
		}
		return
	}

	rendered, err := r.Render(fullText)
	if err != nil {
		s := m.content.String()
		if len(s) > 0 && s[len(s)-1] != '\n' {
			m.content.WriteString("\n")
		}
		return
	}

	// Replace: keep everything before streamStart, append rendered text
	before := m.content.String()[:m.streamStart]
	m.content.Reset()
	m.content.WriteString(before)
	m.content.WriteString(strings.TrimRight(rendered, "\n"))
	m.content.WriteString("\n")
}

// ---------- helpers ----------

func (m *Model) appendLine(text string) {
	m.content.WriteString(text)
	m.content.WriteString("\n")
}

func (m *Model) appendText(text string) {
	m.content.WriteString(text)
}

// formatToolParams extracts a compact display string from raw JSON params.
func formatToolParams(raw string) string {
	s := strings.TrimSpace(raw)
	if len(s) > 120 {
		s = s[:120] + "..."
	}
	return s
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
