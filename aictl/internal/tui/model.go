package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/aictl/aictl/internal/tools"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
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
type toolTickMsg struct{}
type subAgentProgressMsg struct{ progress SubAgentProgress }
type questionMsg struct {
	question string
	options  []string
	replyCh  chan string // sends selected option text; closed on cancel
}

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

// TUIConfig carries version/provider info for the welcome page and status bar.
type TUIConfig struct {
	Version     string // e.g. "v0.1.0"
	Provider    string // e.g. "deepseek"
	Model       string // e.g. "deepseek-chat"
	SessionID   string // first 8 chars of session ID
	ShowWelcome bool   // false for run mode (non-interactive)
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

	// Status bar styles
	separatorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("238")) // dark gray line

	statusBarBgStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("235"))

	statusModelStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("235")).
				Foreground(lipgloss.Color("2")). // green model name
				Bold(true)

	// Welcome box styles
	welcomeBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("8")).
				Padding(0, 1)

	welcomeTitleStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("2")).
				Bold(true)

	welcomeLabelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("8"))

	welcomeValueStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("252"))

	welcomeHintStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("245"))

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
				Background(lipgloss.Color("237")).
				Foreground(lipgloss.Color("245")).
				Padding(0, 2).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("238"))

	confirmDangerHintStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("237")).
				Foreground(lipgloss.Color("203")). // subtle red text
				Padding(0, 2).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("238"))
)

// ---------- Model ----------

// Model is the bubbletea model managing the full TUI state.
//
// Rendering strategy (inline mode, no alt-screen):
//   - Completed content is pushed to terminal scrollback via tea.Println().
//   - View() only renders the "live" area: current streaming text, running
//     tool block, input line, and status bar.
//   - The terminal natively handles scrollback and mouse selection.
type Model struct {
	textinput    textinput.Model
	spinner      spinner.Model
	width        int
	height       int
	liveContent  strings.Builder // only the current streaming LLM text
	streaming    bool            // LLM text deltas are arriving
	inputMode    bool            // text input is active (waiting for user)
	spinnerKind spinnerKind      // what the spinner is showing for

	currentTool          *toolCallState // in-flight tool call (nil when idle)
	currentToolConfirmed bool           // true if tool was already shown in confirm block

	confirming   bool                  // waiting for confirmation
	confirmCh    chan bool             // send user's answer back to agent goroutine
	confirmLevel tools.PermissionLevel // permission level of current confirmation

	questioning   bool        // waiting for question answer
	questionCh    chan string  // send user's answer back
	questionOpts  []string    // available options
	questionSel   int         // currently highlighted option (0-based)

	inputCh chan inputResult // send user input back to ReadInput()

	noiseDropCount int // after terminal noise, drop next N single-char key events

	quitting bool

	// status bar
	tokens        int
	toolName      string    // current tool being executed (empty = none)
	toolStartTime time.Time // when the current tool started

	cancelToolFn func() bool // injected from TuiIO to cancel running tool
	cancelLoopFn func() bool // injected from TuiIO to cancel entire agent loop

	// sub-agent progress (shown when a "task" tool is running)
	subAgentTool  string // sub-agent's current tool name
	subAgentCount int    // sub-agent's total tool calls

	cfg     TUIConfig // version/provider info
	program *tea.Program

	mdRenderer      *glamour.TermRenderer // cached markdown renderer (avoids terminal queries)
	mdRendererWidth int                   // width the renderer was created for
}

// NewModel creates the initial bubbletea model.
func NewModel(inputCh chan inputResult, cfg TUIConfig) Model {
	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.CharLimit = 4096

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = spinnerStyle

	return Model{
		textinput: ti,
		spinner:   sp,
		inputCh:   inputCh,
		cfg:       cfg,
	}
}

func toolTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return toolTickMsg{} })
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{textinput.Blink, m.spinner.Tick}
	if m.cfg.ShowWelcome {
		cmds = append(cmds, tea.Println(renderWelcome(m.cfg)))
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.textinput.Width = m.width - 4 // account for prompt

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case tea.KeyMsg:
		// Filter out terminal escape sequences leaking as key events.
		s := msg.String()
		if isTerminalNoiseKey(s) {
			m.noiseDropCount = 4
			return m, nil
		}
		if s == "esc" && m.inputMode {
			m.noiseDropCount = 4
			return m, nil
		}
		if m.noiseDropCount > 0 && len(s) <= 2 {
			m.noiseDropCount--
			return m, nil
		}
		switch s {
		case "ctrl+c":
			if m.questioning && m.questionCh != nil {
				close(m.questionCh)
				m.questioning = false
				m.questionCh = nil
				cmds = append(cmds, tea.Println(systemStyle.Render("  [cancelled]")))
				return m, tea.Batch(cmds...)
			}
			if m.confirming && m.confirmCh != nil {
				m.confirmCh <- false
				m.confirming = false
				m.confirmCh = nil
				cmds = append(cmds, tea.Println(systemStyle.Render("  [cancelled]")))
				return m, tea.Batch(cmds...)
			}
			if m.inputMode {
				m.inputCh <- inputResult{err: fmt.Errorf("interrupted")}
				m.inputMode = false
				m.textinput.Blur()
			}
			m.quitting = true
			return m, tea.Quit
		case "enter":
			if m.questioning && m.questionCh != nil {
				if m.questionSel >= 0 && m.questionSel < len(m.questionOpts) {
					m.questionCh <- m.questionOpts[m.questionSel]
				}
				m.questioning = false
				m.questionCh = nil
				return m, nil
			}
			if m.confirming && m.confirmCh != nil {
				m.confirmCh <- true
				m.confirming = false
				m.confirmCh = nil
				m.currentToolConfirmed = true
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
		case "up":
			if m.questioning && m.questionSel > 0 {
				m.questionSel--
			}
			return m, nil
		case "down":
			if m.questioning && m.questionSel < len(m.questionOpts)-1 {
				m.questionSel++
			}
			return m, nil
		case "1", "2", "3", "4":
			if m.questioning && m.questionCh != nil {
				idx := int(s[0]-'0') - 1
				if idx >= 0 && idx < len(m.questionOpts) {
					m.questionCh <- m.questionOpts[idx]
					m.questioning = false
					m.questionCh = nil
				}
				return m, nil
			}
		case "esc":
			if m.questioning && m.questionCh != nil {
				close(m.questionCh)
				m.questioning = false
				m.questionCh = nil
				cmds = append(cmds, tea.Println(systemStyle.Render("  [cancelled]")))
				return m, tea.Batch(cmds...)
			}
			if m.confirming && m.confirmCh != nil {
				m.confirmCh <- false
				m.confirming = false
				m.confirmCh = nil
				cmds = append(cmds, tea.Println(toolErrorStyle.Render("  ✗ denied")))
				return m, tea.Batch(cmds...)
			}
			if m.toolName != "" && m.cancelToolFn != nil {
				m.cancelToolFn()
				return m, nil
			}
			if (m.spinnerKind == spinnerThinking || m.streaming) && m.cancelLoopFn != nil {
				m.cancelLoopFn()
				m.spinnerKind = spinnerNone
				m.streaming = false
				m.liveContent.Reset()
				return m, nil
			}
		}

		if m.inputMode && !m.confirming {
			if isControlKeyMsg(msg.String()) {
				return m, nil
			}
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
		cmds = append(cmds, tea.Println(userStyle.Render("You: "+msg.text)))

	case thinkingStartMsg:
		m.spinnerKind = spinnerThinking
		m.streaming = false

	case textDeltaMsg:
		if m.spinnerKind == spinnerThinking {
			m.spinnerKind = spinnerNone
		}
		m.streaming = true
		m.liveContent.WriteString(msg.delta)

	case textDoneMsg:
		m.spinnerKind = spinnerNone
		m.streaming = false
		rendered := m.renderMarkdown(msg.fullText)
		m.liveContent.Reset()
		cmds = append(cmds, tea.Println(rendered))

	case toolTickMsg:
		if m.toolName != "" {
			cmds = append(cmds, toolTickCmd())
		}
		return m, tea.Batch(cmds...)

	case toolStartMsg:
		m.toolName = msg.name
		m.toolStartTime = time.Now()
		m.spinnerKind = spinnerTool
		m.currentToolConfirmed = false
		m.subAgentTool = ""
		m.subAgentCount = 0
		m.currentTool = &toolCallState{
			name:   msg.name,
			params: formatToolParams(msg.params),
		}
		cmds = append(cmds, toolTickCmd())

	case toolDoneMsg:
		if m.currentTool != nil {
			var line string
			if m.currentToolConfirmed {
				if msg.isErr {
					line = toolErrorStyle.Render("  ✗ " + truncateStr(msg.result, 200))
				} else {
					summary := truncateStr(strings.ReplaceAll(msg.result, "\n", " "), 120)
					line = toolSuccessStyle.Render("  ✓ " + summary)
				}
			} else {
				line = m.renderToolDone(m.currentTool, msg.result, msg.isErr)
			}
			cmds = append(cmds, tea.Println(line))
		}
		m.toolName = ""
		m.toolStartTime = time.Time{}
		m.spinnerKind = spinnerNone
		m.currentTool = nil
		m.subAgentTool = ""
		m.subAgentCount = 0

	case subAgentProgressMsg:
		m.subAgentTool = msg.progress.ToolName
		m.subAgentCount = msg.progress.ToolCount

	case confirmMsg:
		m.confirming = true
		m.confirmCh = msg.replyCh
		m.confirmLevel = msg.level
		m.spinnerKind = spinnerNone
		cmds = append(cmds, tea.Println(m.renderConfirmBlock(msg.name, msg.params, msg.level)))

	case questionMsg:
		m.questioning = true
		m.questionCh = msg.replyCh
		m.questionOpts = msg.options
		m.questionSel = 0
		m.spinnerKind = spinnerNone
		cmds = append(cmds, tea.Println(m.renderQuestionBlock(msg.question, msg.options)))

	case systemMsg:
		cmds = append(cmds, tea.Println(systemStyle.Render(msg.text)))

	case errorMsg:
		cmds = append(cmds, tea.Println(errorStyle.Render("Error: "+msg.text)))

	case tokensMsg:
		m.tokens = msg.n

	case agentDoneMsg:
		m.quitting = true
		return m, tea.Quit
	}

	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}

	// === Live area (content that is currently changing) ===
	var live string
	switch m.spinnerKind {
	case spinnerThinking:
		live = m.spinner.View() + " Thinking..."
	case spinnerTool:
		if m.currentTool != nil {
			live = m.renderToolRunning(m.currentTool)
		}
	default:
		if m.streaming {
			live = m.liveContent.String()
		}
	}

	// === Input line ===
	var input string
	if m.questioning {
		sel := ""
		if m.questionSel >= 0 && m.questionSel < len(m.questionOpts) {
			sel = fmt.Sprintf(" [%d. %s]", m.questionSel+1, m.questionOpts[m.questionSel])
		}
		input = confirmHintStyle.Render("↑↓ select  1-4 pick  enter confirm  esc cancel" + sel)
	} else if m.confirming {
		if m.confirmLevel >= tools.PermissionDangerous {
			input = confirmDangerHintStyle.Render("⚠ dangerous  enter accept  esc deny")
		} else {
			input = confirmHintStyle.Render("enter accept  esc deny")
		}
	} else if m.inputMode {
		input = m.textinput.View()
	} else {
		input = systemStyle.Render("❯")
	}

	// === Status bar ===
	bar := m.renderStatusBar()

	// Combine: live content (if any) + input + status bar
	var parts []string
	if live != "" {
		parts = append(parts, live)
	}
	parts = append(parts, input, bar)
	return strings.Join(parts, "\n")
}

// ---------- tool call rendering ----------

// renderToolRunning renders an in-flight tool call block with spinner (2-3 lines).
func (m *Model) renderToolRunning(tc *toolCallState) string {
	name := toolNameStyle.Render(tc.name)
	params := toolParamStyle.Render(tc.params)
	elapsed := int(time.Since(m.toolStartTime).Seconds())
	status := fmt.Sprintf("%s running... (%ds)  esc to cancel", m.spinner.View(), elapsed)

	// Sub-agent progress: show current tool on an extra line
	if tc.name == "task" && m.subAgentTool != "" {
		subLine := toolParamStyle.Render(fmt.Sprintf("  └ %s  (%d tool calls)", m.subAgentTool, m.subAgentCount))
		inner := name + "  " + params + "\n" + subLine + "\n" + status
		return toolBorderStyle.Render(inner)
	}

	inner := name + "  " + params + "\n" + status
	return toolBorderStyle.Render(inner)
}

// renderToolDone renders a completed tool call block (single line).
func (m *Model) renderToolDone(tc *toolCallState, result string, isErr bool) string {
	name := toolNameStyle.Render(tc.name)
	var status string
	if isErr {
		status = toolErrorStyle.Render("✗ " + truncateStr(result, 200))
	} else {
		summary := truncateStr(strings.ReplaceAll(result, "\n", " "), 120)
		status = toolSuccessStyle.Render("✓ " + summary)
	}
	inner := name + "  " + status
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

// renderQuestionBlock renders a question with numbered options.
func (m *Model) renderQuestionBlock(question string, options []string) string {
	var lines []string
	lines = append(lines, toolNameStyle.Render("? "+question))
	for i, opt := range options {
		prefix := fmt.Sprintf("  %d. ", i+1)
		lines = append(lines, toolParamStyle.Render(prefix+opt))
	}
	return confirmBorderStyle.Render(strings.Join(lines, "\n"))
}

// renderStatusBar renders the bottom status bar (separator + model/tokens/tool info).
func (m *Model) renderStatusBar() string {
	modelName := m.cfg.Model
	if modelName == "" {
		modelName = "unknown"
	}
	status := statusModelStyle.Render(" "+modelName) + statusBarStyle.Render(fmt.Sprintf(" │ tokens: %d", m.tokens))
	if m.toolName != "" {
		elapsed := int(time.Since(m.toolStartTime).Seconds())
		status += statusBarStyle.Render(fmt.Sprintf(" │ tool: %s (%ds)", m.toolName, elapsed))
	}
	return separatorStyle.Width(m.width).Render(strings.Repeat("─", m.width)) + "\n" +
		statusBarBgStyle.Width(m.width).Render(status)
}

// ---------- markdown rendering ----------

// getMarkdownRenderer returns a cached glamour renderer, recreating it only
// when the terminal width changes. Uses DarkStyle to avoid terminal background
// color queries that produce escape sequence responses leaking into textinput.
func (m *Model) getMarkdownRenderer() *glamour.TermRenderer {
	width := m.width
	if width <= 0 {
		width = 80
	}
	wrapWidth := width - 4
	if m.mdRenderer != nil && m.mdRendererWidth == wrapWidth {
		return m.mdRenderer
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(wrapWidth),
	)
	if err != nil {
		return nil
	}
	m.mdRenderer = r
	m.mdRendererWidth = wrapWidth
	return r
}

// renderMarkdown renders text with glamour and trims trailing whitespace.
func (m *Model) renderMarkdown(text string) string {
	r := m.getMarkdownRenderer()
	if r == nil {
		return text
	}
	rendered, err := r.Render(text)
	if err != nil {
		return text
	}
	return strings.TrimRight(rendered, "\n")
}

// ---------- welcome page ----------

func renderWelcome(cfg TUIConfig) string {
	// Pixel cat logo
	cat := []string{
		"█▀▀▀▀▀█",
		"█ ●  ● █",
		"█  ▲  █",
		"█ ▄▄▄ █",
		" ▀▀▀▀▀ ",
	}

	version := cfg.Version
	if version == "" {
		version = "dev"
	}

	// Right-side info lines (aligned with cat lines)
	info := []string{
		welcomeLabelStyle.Render("Provider: ") + welcomeValueStyle.Render(cfg.Provider),
		welcomeLabelStyle.Render("Model:    ") + welcomeValueStyle.Render(cfg.Model),
		welcomeLabelStyle.Render("Session:  ") + welcomeValueStyle.Render(cfg.SessionID),
		"",
		welcomeHintStyle.Render("/help 查看命令  /compact 压缩上下文  pgup/pgdn 滚动"),
	}

	// Combine cat + info side by side
	var lines []string
	catWidth := 10 // visual width of cat lines + padding
	for i := 0; i < len(cat) || i < len(info); i++ {
		left := ""
		if i < len(cat) {
			left = cat[i]
		}
		right := ""
		if i < len(info) {
			right = info[i]
		}
		// Pad left to fixed width using runewidth for accurate visual width
		visualWidth := lipgloss.Width(left)
		padding := catWidth - visualWidth
		if padding < 0 {
			padding = 0
		}
		lines = append(lines, left+strings.Repeat(" ", padding)+right)
	}
	lines = append(lines, strings.Repeat(" ", catWidth)+welcomeHintStyle.Render("/sessions 历史  /resume <id> 恢复"))

	inner := strings.Join(lines, "\n")

	title := welcomeTitleStyle.Render(fmt.Sprintf("aictl %s", version))

	box := welcomeBorderStyle.Render(inner)

	return title + "\n" + box
}

// ---------- helpers ----------

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

func isTerminalNoiseKey(s string) bool {
	// OSC responses: ]11;rgb:... background color, etc.
	if strings.Contains(s, ";rgb:") || strings.HasPrefix(s, "]") || strings.HasPrefix(s, "alt+]") {
		return true
	}

	// SGR mouse reports: \;N;NM or \;N;Nm
	if (strings.HasSuffix(s, "M") || strings.HasSuffix(s, "m")) && strings.Contains(s, ";") {
		return true
	}

	// CSI mouse/SGR sequences: [<... or alt+[<...
	if strings.HasPrefix(s, "[<") || strings.HasPrefix(s, "alt+[<") {
		return true
	}

	// DA (Device Attributes) responses: [?...c
	if strings.HasPrefix(s, "[?") || strings.HasPrefix(s, "alt+[?") {
		return true
	}

	// CSI parameter sequences: [N... (numeric params)
	if len(s) > 1 && s[0] == '[' && len(s) > 1 && s[1] >= '0' && s[1] <= '9' {
		return true
	}

	return false
}

// isControlKeyMsg returns true if the key string contains non-printable
// control characters that should not be forwarded to the text input.
func isControlKeyMsg(s string) bool {
	for _, r := range s {
		if r == '\x1b' || (r < 0x20 && r != '\t' && r != '\n' && r != '\r') {
			return true
		}
	}
	return false
}
