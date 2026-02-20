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

const statusBarHeight = 1
const inputHeight = 1

// Model is the bubbletea model managing the full TUI state.
type Model struct {
	viewport     viewport.Model
	textinput    textinput.Model
	spinner      spinner.Model
	width        int
	height       int
	content     *strings.Builder // pointer to avoid copy panic in bubbletea
	streaming   bool             // LLM text deltas are arriving
	streamStart int              // byte offset in content where current stream began
	inputMode   bool             // text input is active (waiting for user)
	spinnerKind spinnerKind      // what the spinner is showing for

	currentTool          *toolCallState // in-flight tool call (nil when idle)
	currentToolConfirmed bool           // true if tool was already shown in confirm block

	confirming   bool                  // waiting for confirmation
	confirmCh    chan bool             // send user's answer back to agent goroutine
	confirmLevel tools.PermissionLevel // permission level of current confirmation

	inputCh chan inputResult // send user input back to ReadInput()

	noiseDropCount int // after terminal noise, drop next N single-char key events

	quitting bool

	// status bar
	tokens   int
	toolName string // current tool being executed (empty = none)

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

	vp := viewport.New(80, 24)

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = spinnerStyle

	m := Model{
		viewport:     vp,
		textinput:    ti,
		spinner:      sp,
		content:      &strings.Builder{},
		inputCh:      inputCh,
		cfg:          cfg,
	}

	if cfg.ShowWelcome {
		m.content.WriteString(renderWelcome(cfg))
		m.content.WriteString("\n")
	}

	return m
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
		// Reserve 3 lines: 1 input + 1 separator + 1 status bar.
		vpHeight := m.height - 3
		if vpHeight < 1 {
			vpHeight = 1
		}
		m.viewport.Width = m.width
		m.viewport.Height = vpHeight
		m.textinput.Width = m.width - 4 // account for prompt
		m.viewport.SetContent(m.renderContent())
		m.viewport.GotoBottom()

	case spinner.TickMsg:
		wasAtBottom := m.viewport.AtBottom()
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.spinnerKind != spinnerNone {
			m.viewport.SetContent(m.renderContent())
			if wasAtBottom {
				m.viewport.GotoBottom()
			}
		}
		cmds = append(cmds, cmd)

	case tea.KeyMsg:
		// Filter out terminal escape sequences leaking as key events.
		// Terminal responses can be fragmented across multiple KeyMsgs;
		// after detecting noise, drop a few subsequent single-char events
		// that are likely trailing fragments (e.g. ESC is caught, but
		// the following '\', '[', '?' leak as separate keys).
		s := msg.String()
		if isTerminalNoiseKey(s) {
			m.noiseDropCount = 4
			return m, nil
		}
		if s == "esc" {
			// Bare ESC in input mode is likely the start of a split sequence.
			if m.inputMode {
				m.noiseDropCount = 4
				return m, nil
			}
		}
		if m.noiseDropCount > 0 && len(s) <= 2 {
			m.noiseDropCount--
			return m, nil
		}
		switch s {
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
				m.confirmCh <- true
				m.confirming = false
				m.confirmCh = nil
				m.currentToolConfirmed = true // tool block already shown, skip re-render
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
				m.confirmCh <- false
				m.confirming = false
				m.confirmCh = nil
				m.appendLine(toolErrorStyle.Render("  ✗ denied"))
				return m, nil
			}

		// --- Keyboard scrolling (no mouse capture, like opencode) ---
		case "pgup":
			m.viewport.ViewUp()
			return m, nil
		case "pgdown":
			m.viewport.ViewDown()
			return m, nil
		case "shift+up":
			m.viewport.LineUp(3)
			return m, nil
		case "shift+down":
			m.viewport.LineDown(3)
			return m, nil
		case "home":
			m.viewport.GotoTop()
			return m, nil
		case "end":
			m.viewport.GotoBottom()
			return m, nil
			}

		if m.inputMode && !m.confirming {
			// Block escape sequences and control chars from reaching textinput
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
		m.currentToolConfirmed = false
		m.currentTool = &toolCallState{
			name:   msg.name,
			params: formatToolParams(msg.params),
		}

	case toolDoneMsg:
		if m.currentTool != nil {
			if m.currentToolConfirmed {
				if msg.isErr {
					m.appendLine(toolErrorStyle.Render("  ✗ " + truncateStr(msg.result, 200)))
				} else {
					summary := truncateStr(strings.ReplaceAll(msg.result, "\n", " "), 120)
					m.appendLine(toolSuccessStyle.Render("  ✓ " + summary))
				}
			} else {
				m.appendLine(m.renderToolDone(m.currentTool, msg.result, msg.isErr))
			}
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

	// Update viewport content; only auto-scroll if user was already at bottom.
	wasAtBottom := m.viewport.AtBottom()
	m.viewport.SetContent(m.renderContent())
	switch msg.(type) {
	case tea.KeyMsg, tea.MouseMsg:
		// User interaction — don't force scroll to bottom.
	default:
		// Agent output — only follow stream if user hasn't scrolled up.
		if wasAtBottom {
			m.viewport.GotoBottom()
		}
	}

	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	cmds = append(cmds, vpCmd)

	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}

	// Status bar: model │ tokens: N │ tool: name
	modelName := m.cfg.Model
	if modelName == "" {
		modelName = "unknown"
	}
	status := statusModelStyle.Render(" "+modelName) + statusBarStyle.Render(fmt.Sprintf(" │ tokens: %d", m.tokens))
	if m.toolName != "" {
		status += statusBarStyle.Render(fmt.Sprintf(" │ tool: %s", m.toolName))
	}
	bar := separatorStyle.Width(m.width).Render(strings.Repeat("─", m.width)) + "\n" +
		statusBarBgStyle.Width(m.width).Render(status)

	// Input line
	var input string
	if m.confirming {
		if m.confirmLevel >= tools.PermissionDangerous {
			input = confirmDangerHintStyle.Render("⚠ dangerous  enter accept  esc deny")
		} else {
			input = confirmHintStyle.Render("enter accept  esc deny")
		}
	} else if m.inputMode {
		input = m.textinput.View()
	} else {
		// Agent is working — show dimmed prompt so screen doesn't look dead.
		input = systemStyle.Render("❯")
	}

	// Build layout: content + input tight, then gap, then status bar at bottom.
	// When content is short, input sits right below content (no gap in between).
	// When content overflows, viewport handles scrolling.
	rawContent := m.renderContent()
	contentHeight := lipgloss.Height(rawContent)
	// Fixed bottom block: 1 (input) + 2 (separator + statusbar) = 3 lines
	const bottomBlock = 3
	if contentHeight+bottomBlock <= m.height {
		// Content fits: content → input → bar packed at top, blank fills bottom.
		blank := strings.Repeat("\n", m.height-contentHeight-bottomBlock)
		return rawContent + "\n" + input + "\n" + bar + blank
	}

	// Content overflows: use viewport for scrolling.
	return m.viewport.View() + "\n" + input + "\n" + bar
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

// replaceStreamWithMarkdown replaces the raw streamed text (from streamStart
// to end of content) with glamour-rendered markdown.
func (m *Model) replaceStreamWithMarkdown(fullText string) {
	r := m.getMarkdownRenderer()
	if r == nil {
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
