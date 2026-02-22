package tui

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/apexion-ai/apexion/internal/tools"
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
	replyCh  chan string
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
	Version     string
	Provider    string
	Model       string
	SessionID   string
	ShowWelcome bool
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

	// spinnerStyle: orange while active (matches ⏺ running color)
	spinnerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214"))

	// ── tool call: ⏺ dot ──────────────────────────────────────────────────
	// Orange while running, gray when done — same as Claude Code.
	dotRunningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Bold(true)

	dotDoneStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	// ── tool result: "  ⎿  " prefix ──────────────────────────────────────
	resultPrefixStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("245"))

	toolNameStyle = lipgloss.NewStyle().
			Bold(true)

	toolParamStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	toolOutputStyle = lipgloss.NewStyle() // raw output lines: default color

	toolErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	toolSuccessStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("2"))

	hintStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Italic(true)

	// Status bar
	separatorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("238"))

	statusBarBgStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("235"))

	statusModelStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("235")).
				Foreground(lipgloss.Color("2")).
				Bold(true)

	// Welcome box
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

	// Confirm / permission: rounded border, blue-purple (matches Claude Code)
	confirmBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("63")).
				Padding(0, 1)

	confirmDangerBorderStyle = lipgloss.NewStyle().
					Border(lipgloss.RoundedBorder()).
					BorderForeground(lipgloss.Color("196")).
					Padding(0, 1)

	confirmHintStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("237")).
				Foreground(lipgloss.Color("245")).
				Padding(0, 2).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("238"))

	confirmDangerHintStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("237")).
				Foreground(lipgloss.Color("203")).
				Padding(0, 2).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("238"))
)

// claudeSpinner matches Claude Code's spinner character set and speed.
var claudeSpinner = spinner.Spinner{
	Frames: []string{"·", "✢", "✳", "✶", "✻", "✽", "✻", "✶", "✳", "✢"},
	FPS:    120 * time.Millisecond,
}

// ---------- Model ----------

// Model is the bubbletea model managing the full TUI state.
type Model struct {
	textinput    textinput.Model
	spinner      spinner.Model
	width        int
	height       int
	liveContent  *strings.Builder
	streaming    bool
	inputMode    bool
	spinnerKind  spinnerKind

	currentTool          *toolCallState
	currentToolConfirmed bool

	confirming   bool
	confirmCh    chan bool
	confirmLevel tools.PermissionLevel

	questioning  bool
	questionCh   chan string
	questionOpts []string
	questionSel  int

	inputCh chan inputResult

	noiseDropCount int

	quitting bool

	tokens        int
	toolName      string
	toolStartTime time.Time

	cancelToolFn func() bool
	cancelLoopFn func() bool

	subAgentTool  string
	subAgentCount int

	cfg     TUIConfig
	program *tea.Program

	mdRenderer      *glamour.TermRenderer
	mdRendererWidth int
}

// NewModel creates the initial bubbletea model.
func NewModel(inputCh chan inputResult, cfg TUIConfig) Model {
	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.CharLimit = 4096

	sp := spinner.New()
	sp.Spinner = claudeSpinner
	sp.Style = spinnerStyle

	return Model{
		textinput:   ti,
		spinner:     sp,
		liveContent: &strings.Builder{},
		inputCh:     inputCh,
		cfg:         cfg,
	}
}

func toolTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return toolTickMsg{} })
}

func (m Model) Init() tea.Cmd {
	var cmds []tea.Cmd
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
		m.textinput.Width = m.width - 4

	case spinner.TickMsg:
		if m.spinnerKind != spinnerNone {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}

	case tea.KeyMsg:
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

	case userMsg:
		cmds = append(cmds, tea.Println(userStyle.Render("You: "+msg.text)))

	case thinkingStartMsg:
		m.spinnerKind = spinnerThinking
		m.streaming = false
		cmds = append(cmds, m.spinner.Tick)

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
			params: msg.params,
		}
		cmds = append(cmds, toolTickCmd(), m.spinner.Tick)

	case toolDoneMsg:
		if m.currentTool != nil {
			elapsed := time.Since(m.toolStartTime)
			var line string
			if m.currentToolConfirmed {
				// Confirm block already printed the header — just show the result block.
				line = renderResultBlock(msg.name, msg.result, msg.isErr)
			} else {
				line = m.renderToolDone(m.currentTool, msg.result, msg.isErr, elapsed)
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

	var live string
	switch m.spinnerKind {
	case spinnerThinking:
		// ✶ Thinking… — matches Claude Code's thinking indicator
		live = dotRunningStyle.Render(m.spinner.View()) + hintStyle.Render(" Thinking…")
	case spinnerTool:
		if m.currentTool != nil {
			live = m.renderToolRunning(m.currentTool)
		}
	default:
		if m.streaming {
			live = m.liveContent.String()
		}
	}

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

	bar := m.renderStatusBar()

	var parts []string
	if live != "" {
		parts = append(parts, live)
	}
	parts = append(parts, input, bar)
	return strings.Join(parts, "\n")
}

// ---------- tool call rendering ----------

// renderToolRunning renders an in-flight tool call (shown in live View area).
//
//	⏺ Read(…/main.go)  3s · esc to cancel
//	  ⎿  Running…
func (m *Model) renderToolRunning(tc *toolCallState) string {
	header := toolHeader(tc.name, tc.params, true)
	elapsed := int(time.Since(m.toolStartTime).Seconds())
	hint := hintStyle.Render(fmt.Sprintf("  %ds · esc to cancel", elapsed))

	prefix := resultPrefixStyle.Render("  ⎿  ")
	runningLine := prefix + hintStyle.Render("Running…")

	// Sub-agent: show current sub-tool on a second ⎿ line
	if tc.name == "task" && m.subAgentTool != "" {
		subLine := prefix + toolParamStyle.Render(
			fmt.Sprintf("  └ %s (%d calls)", m.subAgentTool, m.subAgentCount))
		return header + hint + "\n" + runningLine + "\n" + subLine
	}

	return header + hint + "\n" + runningLine
}

// renderToolDone renders a completed tool call (printed to scrollback).
//
// Simple tools (read, list, glob, grep, write, edit) → 1 line:
//
//	⏺ Read(…/main.go)  Read 42 lines  0.3s
//
// Complex tools (bash, git, mcp…) / errors → 2 lines:
//
//	⏺ Bash(git status)  0.8s
//	  ⎿  output…
func (m *Model) renderToolDone(tc *toolCallState, result string, isErr bool, elapsed time.Duration) string {
	header := toolHeader(tc.name, tc.params, false)
	elapsedStr := hintStyle.Render("  " + formatElapsed(elapsed))

	// Simple tools: collapse to 1 line
	if !isErr {
		if summary := toolInlineSummary(tc.name, result); summary != "" {
			return header + "  " + summary + elapsedStr
		}
	}

	// Complex / error: 2-line format
	resultBlock := renderResultBlock(tc.name, result, isErr)
	return header + elapsedStr + "\n" + resultBlock
}

// toolInlineSummary returns a short inline summary for simple tools.
// Returns "" for complex tools that need a full result block.
func toolInlineSummary(name, result string) string {
	switch name {
	case "read_file":
		n := countNonEmptyContent(result)
		return toolParamStyle.Render(fmt.Sprintf("Read %d %s", n, pluralLine(n)))
	case "write_file":
		return toolSuccessStyle.Render(firstLine(result))
	case "edit_file":
		return toolSuccessStyle.Render(firstLine(result))
	case "glob":
		n := countNonEmptyLines(result)
		return toolParamStyle.Render(fmt.Sprintf("Found %d %s", n, pluralFile(n)))
	case "grep":
		n := countNonEmptyLines(result)
		return toolParamStyle.Render(fmt.Sprintf("Found %d %s", n, pluralMatch(n)))
	case "list_dir":
		n := countNonEmptyLines(result)
		return toolParamStyle.Render(fmt.Sprintf("Listed %d %s", n, pluralEntry(n)))
	case "todo_write", "todo_read":
		if result == "" {
			return hintStyle.Render("(empty)")
		}
		return toolParamStyle.Render(firstLine(result))
	}
	return ""
}

// renderConfirmBlock renders the permission dialog as a rounded-border box.
func (m *Model) renderConfirmBlock(name, params string, level tools.PermissionLevel) string {
	displayName := toolDisplayName(name)
	param := toolPrimaryParam(name, params)

	var header string
	if param != "" {
		header = toolNameStyle.Render(displayName) + toolParamStyle.Render("("+param+")")
	} else {
		header = toolNameStyle.Render(displayName)
	}

	border := confirmBorderStyle
	if level >= tools.PermissionDangerous {
		border = confirmDangerBorderStyle
	}

	var lines []string
	lines = append(lines, header)
	if param == "" && params != "" && params != "{}" {
		// Show raw params if we couldn't extract a primary param
		short := params
		if len(short) > 80 {
			short = short[:80] + "…"
		}
		lines = append(lines, toolParamStyle.Render(short))
	}
	if level >= tools.PermissionDangerous {
		lines = append(lines, "")
		lines = append(lines, confirmDangerHintStyle.Render("⚠  DANGEROUS OPERATION"))
	}

	return border.Render(strings.Join(lines, "\n"))
}

// renderQuestionBlock renders a question with numbered options.
func (m *Model) renderQuestionBlock(question string, options []string) string {
	var lines []string
	lines = append(lines, toolNameStyle.Render("? "+question))
	for i, opt := range options {
		lines = append(lines, toolParamStyle.Render(fmt.Sprintf("  %d. %s", i+1, opt)))
	}
	return confirmBorderStyle.Render(strings.Join(lines, "\n"))
}

// renderStatusBar renders the bottom separator + model/tokens/tool bar.
func (m *Model) renderStatusBar() string {
	modelName := m.cfg.Model
	if modelName == "" {
		modelName = "unknown"
	}
	status := statusModelStyle.Render(" "+modelName) +
		statusBarStyle.Render(fmt.Sprintf(" │ tokens: %d", m.tokens))
	if m.toolName != "" {
		elapsed := int(time.Since(m.toolStartTime).Seconds())
		status += statusBarStyle.Render(fmt.Sprintf(" │ %s (%ds)", toolDisplayName(m.toolName), elapsed))
	}
	return separatorStyle.Width(m.width).Render(strings.Repeat("─", m.width)) + "\n" +
		statusBarBgStyle.Width(m.width).Render(status)
}

// ---------- markdown rendering ----------

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

	info := []string{
		welcomeLabelStyle.Render("Provider: ") + welcomeValueStyle.Render(cfg.Provider),
		welcomeLabelStyle.Render("Model:    ") + welcomeValueStyle.Render(cfg.Model),
		welcomeLabelStyle.Render("Session:  ") + welcomeValueStyle.Render(cfg.SessionID),
		"",
		welcomeHintStyle.Render("/help 查看命令  /compact 压缩上下文  pgup/pgdn 滚动"),
	}

	var lines []string
	catWidth := 10
	for i := 0; i < len(cat) || i < len(info); i++ {
		left := ""
		if i < len(cat) {
			left = cat[i]
		}
		right := ""
		if i < len(info) {
			right = info[i]
		}
		visualWidth := lipgloss.Width(left)
		padding := catWidth - visualWidth
		if padding < 0 {
			padding = 0
		}
		lines = append(lines, left+strings.Repeat(" ", padding)+right)
	}
	lines = append(lines, strings.Repeat(" ", catWidth)+
		welcomeHintStyle.Render("/sessions 历史  /resume <id> 恢复"))

	inner := strings.Join(lines, "\n")
	title := welcomeTitleStyle.Render(fmt.Sprintf("apexion %s", version))
	box := welcomeBorderStyle.Render(inner)
	return title + "\n" + box
}

// ---------- tool display helpers ----------

// toolHeader builds the "⏺ ToolName(param)" header line.
// running=true → orange dot; running=false → gray dot.
func toolHeader(name, rawParams string, running bool) string {
	dot := "⏺"
	var dotStr string
	if running {
		dotStr = dotRunningStyle.Render(dot)
	} else {
		dotStr = dotDoneStyle.Render(dot)
	}

	displayName := toolDisplayName(name)
	param := toolPrimaryParam(name, rawParams)

	var body string
	if param != "" {
		body = toolNameStyle.Render(displayName) + toolParamStyle.Render("("+param+")")
	} else {
		body = toolNameStyle.Render(displayName)
	}
	return dotStr + " " + body
}

// renderResultBlock renders the "  ⎿  ..." result lines.
// For tools with rich output (bash, git_*): head+tail truncation.
// For summary tools (read_file, list_dir, glob, grep): one-line semantic summary.
func renderResultBlock(name, result string, isErr bool) string {
	const resultPrefix = "  ⎿  "
	const contPrefix = "     "

	prefix := resultPrefixStyle.Render(resultPrefix)
	cont := contPrefix // continuation lines: plain indent

	if isErr {
		truncated := truncateResult(result, 10)
		return renderMultiLine(prefix, cont, truncated, toolErrorStyle)
	}

	switch name {
	case "read_file":
		n := countNonEmptyContent(result)
		return prefix + toolParamStyle.Render(fmt.Sprintf("Read %d %s", n, pluralLine(n)))

	case "write_file":
		// result is usually "File written successfully" or similar
		return prefix + toolSuccessStyle.Render(firstLine(result))

	case "edit_file":
		return prefix + toolSuccessStyle.Render(firstLine(result))

	case "glob":
		n := countNonEmptyLines(result)
		return prefix + toolParamStyle.Render(fmt.Sprintf("Found %d %s", n, pluralFile(n)))

	case "grep":
		n := countNonEmptyLines(result)
		return prefix + toolParamStyle.Render(fmt.Sprintf("Found %d %s", n, pluralMatch(n)))

	case "list_dir":
		n := countNonEmptyLines(result)
		return prefix + toolParamStyle.Render(fmt.Sprintf("Listed %d %s", n, pluralEntry(n)))

	case "todo_write", "todo_read":
		if result == "" {
			return prefix + hintStyle.Render("(empty)")
		}
		return prefix + toolParamStyle.Render(firstLine(result))

	default:
		// Multi-line output: bash, git_*, web_*, task, mcp__*, question
		if strings.TrimSpace(result) == "" {
			return prefix + hintStyle.Render("(no output)")
		}
		truncated := truncateResult(result, 13)
		return renderMultiLine(prefix, cont, truncated, toolOutputStyle)
	}
}

// renderMultiLine renders a (possibly multi-line) string with the given
// prefix on the first line and contPrefix on subsequent lines.
// Lines matching "… +N lines" are rendered in hintStyle.
func renderMultiLine(prefix, contPrefix, text string, style lipgloss.Style) string {
	lines := strings.Split(text, "\n")
	// Trim trailing empty lines
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return prefix + hintStyle.Render("(no output)")
	}

	var out []string
	for i, line := range lines {
		p := contPrefix
		if i == 0 {
			p = prefix
		}
		if strings.HasPrefix(line, "… +") {
			out = append(out, p+hintStyle.Render(line))
		} else {
			out = append(out, p+style.Render(line))
		}
	}
	return strings.Join(out, "\n")
}

// truncateResult keeps up to maxLines of output using a head + hint + tail format.
//
//	line1
//	line2
//
//	… +47 lines
//
//	last1
//	last2
//	last3
func truncateResult(s string, maxLines int) string {
	lines := strings.Split(s, "\n")
	// Trim trailing empty lines
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) <= maxLines {
		return strings.Join(lines, "\n")
	}

	const tail = 3
	head := maxLines - tail - 3 // 3 = blank + hint + blank
	if head < 1 {
		head = 1
	}
	skipped := len(lines) - head - tail

	out := make([]string, 0, head+3+tail)
	out = append(out, lines[:head]...)
	out = append(out, "")
	out = append(out, fmt.Sprintf("… +%d lines", skipped))
	out = append(out, "")
	out = append(out, lines[len(lines)-tail:]...)
	return strings.Join(out, "\n")
}

// ---------- tool name / param helpers ----------

var toolDisplayNames = map[string]string{
	"read_file":  "Read",
	"write_file": "Write",
	"edit_file":  "Edit",
	"bash":       "Bash",
	"glob":       "Glob",
	"grep":       "Search",
	"list_dir":   "List",
	"git_status": "GitStatus",
	"git_diff":   "GitDiff",
	"git_commit": "GitCommit",
	"git_push":   "GitPush",
	"web_fetch":  "WebFetch",
	"web_search": "WebSearch",
	"task":       "Task",
	"question":   "Question",
	"todo_write": "TodoWrite",
	"todo_read":  "TodoRead",
}

// toolDisplayName converts an internal tool name to a user-facing display name.
func toolDisplayName(name string) string {
	if d, ok := toolDisplayNames[name]; ok {
		return d
	}
	// MCP tools: mcp__server__tool → server:tool
	if strings.HasPrefix(name, "mcp__") {
		rest := name[5:]
		if i := strings.Index(rest, "__"); i >= 0 {
			return rest[:i] + ":" + rest[i+2:]
		}
		return rest
	}
	return name
}

// toolPrimaryParam extracts the most relevant single parameter from raw JSON params.
func toolPrimaryParam(name, rawParams string) string {
	if rawParams == "" || rawParams == "{}" {
		return ""
	}
	var params map[string]any
	if err := json.Unmarshal([]byte(rawParams), &params); err != nil {
		return ""
	}
	strVal := func(key string) string {
		if v, ok := params[key].(string); ok {
			return v
		}
		return ""
	}

	var val string
	switch name {
	case "read_file", "write_file", "edit_file":
		val = strVal("file_path")
	case "bash":
		val = strVal("command")
	case "glob":
		val = strVal("pattern")
	case "grep":
		val = strVal("pattern")
	case "list_dir":
		val = strVal("path")
	case "web_fetch":
		val = strVal("url")
	case "web_search":
		val = strVal("query")
	case "git_diff":
		val = strVal("target")
	case "git_commit":
		val = strVal("message")
	case "task":
		val = strVal("prompt")
	default:
		// MCP and unknown tools: try common param names
		for _, key := range []string{"path", "file_path", "command", "query", "name", "url"} {
			if v := strVal(key); v != "" {
				val = v
				break
			}
		}
	}

	if val == "" {
		return ""
	}

	// Shorten file paths: keep last 2 path components
	if strings.ContainsAny(val, "/\\") {
		parts := strings.Split(filepath.ToSlash(val), "/")
		if len(parts) > 2 {
			val = "…/" + strings.Join(parts[len(parts)-2:], "/")
		}
	}

	// Truncate long values
	if len(val) > 45 {
		val = val[:42] + "…"
	}
	return val
}

// ---------- time / count helpers ----------

func formatElapsed(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}

func countNonEmptyContent(s string) int {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return 0
	}
	return len(lines)
}

func countNonEmptyLines(s string) int {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

func firstLine(s string) string {
	if idx := strings.Index(s, "\n"); idx >= 0 {
		return s[:idx]
	}
	return s
}

func pluralLine(n int) string {
	if n == 1 {
		return "line"
	}
	return "lines"
}

func pluralFile(n int) string {
	if n == 1 {
		return "file"
	}
	return "files"
}

func pluralMatch(n int) string {
	if n == 1 {
		return "match"
	}
	return "matches"
}

func pluralEntry(n int) string {
	if n == 1 {
		return "entry"
	}
	return "entries"
}

// ---------- key event helpers ----------

func isTerminalNoiseKey(s string) bool {
	if strings.Contains(s, ";rgb:") || strings.HasPrefix(s, "]") || strings.HasPrefix(s, "alt+]") {
		return true
	}
	if (strings.HasSuffix(s, "M") || strings.HasSuffix(s, "m")) && strings.Contains(s, ";") {
		return true
	}
	if strings.HasPrefix(s, "[<") || strings.HasPrefix(s, "alt+[<") {
		return true
	}
	if strings.HasPrefix(s, "[?") || strings.HasPrefix(s, "alt+[?") {
		return true
	}
	if len(s) > 1 && s[0] == '[' && s[1] >= '0' && s[1] <= '9' {
		return true
	}
	return false
}

func isControlKeyMsg(s string) bool {
	for _, r := range s {
		if r == '\x1b' || (r < 0x20 && r != '\t' && r != '\n' && r != '\r') {
			return true
		}
	}
	return false
}
