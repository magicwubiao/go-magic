package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"

	"github.com/magicwubiao/go-magic/internal/agent"
	"github.com/magicwubiao/go-magic/internal/kanban"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/internal/session"
	"github.com/magicwubiao/go-magic/internal/skills"
	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/utils"
)

// ---------------------------------------------------------------------------
// ChatMessage represents a single message displayed in the TUI
// ---------------------------------------------------------------------------

// ChatMessage represents a message in the chat
type ChatMessage struct {
	Role      string // "user", "assistant", "system", "tool", "error"
	Content   string
	Timestamp time.Time
	Streaming bool // true if currently being streamed
}

// ---------------------------------------------------------------------------
// Internal BubbleTea messages
// ---------------------------------------------------------------------------

type streamMsg struct {
	content string
	done    bool
}

type errMsg struct{ err error }

type responseMsg struct{ content string }

type statusMsg struct{ text string }

type newSessionMsg struct{}

type clearMsg struct{}

type appendUserMsg struct{ text string }

type appendAssistantMsg struct{ text string }

// ---------------------------------------------------------------------------
// Styles
// ---------------------------------------------------------------------------

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Background(lipgloss.Color("#1a1a2e")).
			Foreground(lipgloss.Color("#00d2ff")).
			Padding(0, 1)

	userStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4ade80")).
			Bold(true)

	assistantStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ffffff"))

	systemStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#facc15"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#f87171"))

	toolStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9ca3af"))

	statusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#1a1a2e")).
			Foreground(lipgloss.Color("#9ca3af")).
			Padding(0, 1)

	inputPromptStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#4ade80")).
				Bold(true)

	helpPanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#00d2ff")).
			Padding(1, 2)
)

// ---------------------------------------------------------------------------
// Markdown renderer (glamour)
// ---------------------------------------------------------------------------

var mdRenderer *glamour.TermRenderer

func init() {
	// Initial renderer with default width; will be re-created on resize
	initMarkdownRenderer(120)
}

func initMarkdownRenderer(width int) {
	if width < 40 {
		width = 40
	}
	if width > 200 {
		width = 200
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		mdRenderer = nil
	} else {
		mdRenderer = r
	}
}

func renderMarkdown(text string) string {
	if mdRenderer == nil {
		return text
	}
	out, err := mdRenderer.Render(text)
	if err != nil {
		return text
	}
	return strings.TrimSpace(out)
}

// ---------------------------------------------------------------------------
// TUIModel
// ---------------------------------------------------------------------------

// TUIModel is the main BubbleTea model
type TUIModel struct {
	// Core dependencies (set before Run)
	agent       *agent.Agent
	registry    *tool.Registry
	store       *session.Store
	cfg         *config.Config
	goalManager *agent.GoalManager

	// UI components
	viewport viewport.Model
	input    textinput.Model
	spinner  spinner.Model

	// State
	messages    []ChatMessage
	sessionID   string
	sessionNum  int
	modelName   string
	streaming   bool
	streamBuf   *strings.Builder
	cancel      context.CancelFunc
	ctx         context.Context
	quitting    bool
	width       int
	height      int
	streamingOn bool

	// History for input
	inputHistory []string
	historyIdx   int

	// Status bar
	statusText string

	// Mode: "chat" (default) or "coding"
	codingMode bool

	// Undo support
	lastUserInput string

	// Channel for goroutine -> BubbleTea communication
	streamCh chan tea.Msg

	// Performance optimization: track if content needs re-render
	contentDirty bool
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

// NewTUIModel creates a new TUI model
func NewTUIModel(aiAgent *agent.Agent, registry *tool.Registry, store *session.Store, cfg *config.Config) TUIModel {
	// Spinner
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#00d2ff"))

	// Text input (single line)
	ti := textinput.New()
	ti.Placeholder = "Type a message... (Enter: send, /help for commands)"
	ti.Focus()
	ti.CharLimit = 0
	ti.Width = 80

	// Viewport
	vp := viewport.New(80, 20)
	vp.MouseWheelEnabled = true
	vp.MouseWheelDelta = 3

	sessionID := uuid.New().String()

	m := TUIModel{
		agent:       aiAgent,
		registry:    registry,
		store:       store,
		cfg:         cfg,
		modelName:   fmt.Sprintf("%s:%s", cfg.Provider, cfg.Model),
		sessionID:   sessionID,
		sessionNum:  1,
		spinner:     s,
		input:       ti,
		viewport:    vp,
		streamingOn: true,
		statusText:  "ready",
		streamCh:    make(chan tea.Msg, 100),
		streamBuf:   &strings.Builder{},
	}

	m.agent.SetSession(sessionID)

	return m
}

// ---------------------------------------------------------------------------
// Message management helpers
// ---------------------------------------------------------------------------

// refreshViewport updates the viewport content and scrolls to bottom
func (m *TUIModel) refreshViewport() {
	m.viewport.SetContent(m.renderMessages())
	m.viewport.GotoBottom()
	m.contentDirty = false
}

// addMessage appends a new message and marks content as dirty
func (m *TUIModel) addMessage(role, content string) {
	m.messages = append(m.messages, ChatMessage{
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
	})
	m.contentDirty = true
}

// addStreamingMessage appends a streaming message placeholder
func (m *TUIModel) addStreamingMessage(role, content string, streaming bool) {
	m.messages = append(m.messages, ChatMessage{
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
		Streaming: streaming,
	})
	m.contentDirty = true
}

// formatError converts an error to a user-friendly message
func formatError(err error) string {
	if err == nil {
		return ""
	}

	errStr := err.Error()

	// Common error patterns with user-friendly messages
	switch {
	case strings.Contains(errStr, "context canceled"):
		return "Request was cancelled"
	case strings.Contains(errStr, "deadline exceeded"):
		return "Request timed out. Please try again."
	case strings.Contains(errStr, "connection refused"):
		return "Cannot connect to server. Please check your network."
	case strings.Contains(errStr, "rate limit"):
		return "Rate limit exceeded. Please wait a moment and try again."
	case strings.Contains(errStr, "authentication") || strings.Contains(errStr, "unauthorized"):
		return "Authentication failed. Please check your API key."
	case strings.Contains(errStr, "quota"):
		return "API quota exceeded. Please check your usage limits."
	default:
		return fmt.Sprintf("Error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// BubbleTea Init
// ---------------------------------------------------------------------------

// Init initializes the BubbleTea program
func (m TUIModel) Init() tea.Cmd {
	// Add welcome message
	m.messages = append(m.messages, ChatMessage{
		Role: "system",
		Content: `Welcome to Magic Agent v0.3.1!

Quick start:
  Type a message to chat
  /help          Show commands
  /mode coding   Enable coding mode
  /tools         List tools

Tips: Shift+Enter = newline | Up/Down = history | Mouse wheel = scroll`,
		Timestamp: time.Now(),
	})
	m.contentDirty = true

	return tea.Batch(
		m.spinner.Tick,
		m.waitForActivity(),
	)
}

// waitForActivity returns a command that reads from the stream channel
func (m TUIModel) waitForActivity() tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-m.streamCh
		if !ok {
			return nil
		}
		return msg
	}
}

// ---------------------------------------------------------------------------
// BubbleTea Update
// ---------------------------------------------------------------------------

// Update handles all messages and key presses
func (m TUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		// Forward mouse events to viewport for scroll support
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = msg.Width
		m.input.Width = msg.Width - 4
		// viewport height = total - title(1) - input(1) - status(1)
		m.viewport.Height = msg.Height - 3
		m.viewport.SetContent(m.renderMessages())
		// Reinitialize markdown renderer with new terminal width
		initMarkdownRenderer(msg.Width - 4)
		return m, nil

	case spinner.TickMsg:
		if m.streaming {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)

	case streamMsg:
		return m.handleStreamMsg(msg)

	case errMsg:
		m.streaming = false
		m.streamBuf.Reset()
		m.addMessage("error", formatError(msg.err))
		m.statusText = "error"
		m.refreshViewport()
		m.input.Focus()
		return m, nil

	case responseMsg:
		m.streaming = false
		m.streamBuf.Reset()
		m.addMessage("assistant", msg.content)
		m.statusText = "ready"
		m.saveSession()
		m.refreshViewport()
		m.input.Focus()
		return m, nil

	case statusMsg:
		m.statusText = msg.text
		return m, nil

	case newSessionMsg:
		m.doNewSession()
		return m, nil

	case clearMsg:
		m.doClear()
		return m, nil

	case appendUserMsg:
		m.addMessage("user", msg.text)
		m.refreshViewport()
		return m, nil

	case appendAssistantMsg:
		m.addMessage("assistant", msg.text)
		m.refreshViewport()
		return m, nil
	}

	return m, tea.Batch(cmds...)
}

// ---------------------------------------------------------------------------
// Key handling
// ---------------------------------------------------------------------------

func (m TUIModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle Escape: if streaming, cancel generation
	if msg.Type == tea.KeyEscape {
		if m.streaming {
			if m.cancel != nil {
				m.cancel()
			}
			m.streaming = false
			m.streamBuf.Reset()
			for i := len(m.messages) - 1; i >= 0; i-- {
				if m.messages[i].Streaming {
					m.messages[i].Streaming = false
					break
				}
			}
			m.statusText = "cancelled"
			m.addMessage("system", "[Generation cancelled]")
			m.refreshViewport()
			m.input.Focus()
			return m, nil
		}
	}

	// Handle Ctrl+C: if streaming, cancel; otherwise quit
	if msg.Type == tea.KeyCtrlC {
		if m.streaming {
			if m.cancel != nil {
				m.cancel()
			}
			m.streaming = false
			m.streamBuf.Reset()
			// Mark the last streaming message as done
			for i := len(m.messages) - 1; i >= 0; i-- {
				if m.messages[i].Streaming {
					m.messages[i].Streaming = false
					break
				}
			}
			m.statusText = "cancelled"
			m.addMessage("system", "[Generation cancelled]")
			m.refreshViewport()
			m.input.Focus()
			return m, nil
		}
		m.quitting = true
		m.doExit()
		return m, tea.Quit
	}

	// Handle Ctrl+D: quit
	if msg.Type == tea.KeyCtrlD {
		m.quitting = true
		m.doExit()
		return m, tea.Quit
	}

	// While streaming, ignore most keys (except Ctrl+C/D)
	if m.streaming {
		return m, nil
	}

	// Handle Enter key: Enter sends, Shift+Enter inserts newline
	if msg.Type == tea.KeyEnter {
		// Shift+Enter inserts newline (detected via msg.String)
		if msg.String() == "shift+enter" {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}

		// Regular Enter sends message
		input := strings.TrimSpace(m.input.Value())
		if input == "" {
			return m, nil
		}

		// Add to input history
		if len(m.inputHistory) == 0 || m.inputHistory[len(m.inputHistory)-1] != input {
			m.inputHistory = append(m.inputHistory, input)
		}
		m.historyIdx = -1

		// Clear input
		m.input.SetValue("")

		// Process slash commands
		if strings.HasPrefix(input, "/") {
			return m.processCommand(input)
		}

		// Normal chat message
		return m.sendMessage(input)
	}

	switch msg.Type {
	case tea.KeyUp:
		// Navigate input history
		if len(m.inputHistory) > 0 {
			if m.historyIdx == -1 {
				m.historyIdx = len(m.inputHistory) - 1
			} else if m.historyIdx > 0 {
				m.historyIdx--
			}
			m.input.SetValue(m.inputHistory[m.historyIdx])
		}
		return m, nil

	case tea.KeyDown:
		if m.historyIdx >= 0 {
			m.historyIdx++
			if m.historyIdx >= len(m.inputHistory) {
				m.historyIdx = -1
				m.input.SetValue("")
			} else {
				m.input.SetValue(m.inputHistory[m.historyIdx])
			}
		}
		return m, nil

	case tea.KeyPgUp:
		m.viewport.LineUp(10)
		return m, nil

	case tea.KeyPgDown:
		m.viewport.LineDown(10)
		return m, nil

	case tea.KeyTab:
		// Tab completion for slash commands
		input := m.input.Value()
		if strings.HasPrefix(input, "/") {
			completions := []string{}
			commands := []string{
				"/help", "/new", "/exit", "/mode", "/model", "/stream", "/compress",
				"/tools", "/skills", "/usage", "/undo", "/retry", "/stop", "/clear",
				"/save", "/load", "/export", "/history", "/insights",
				"/req", "/reqs", "/req-done", "/req-del", "/req-priority", "/context",
				"/goal", "/kanban", "/kb", "/handoff", "/clarify", "/interrupt",
			}
			for _, cmd := range commands {
				if strings.HasPrefix(cmd, input) {
					completions = append(completions, cmd)
				}
			}
			if len(completions) == 1 {
				m.input.SetValue(completions[0] + " ")
				m.input.CursorEnd()
			} else if len(completions) > 1 {
				m.addMessage("system", "Did you mean: "+strings.Join(completions, "  "))
			}
		}
		return m, nil
	}

	// Delegate to textarea for typing
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// ---------------------------------------------------------------------------
// Slash command processing
// ---------------------------------------------------------------------------

func (m TUIModel) processCommand(input string) (tea.Model, tea.Cmd) {
	cmdName, cmdArgs := parseSlashCommand(input)

	switch cmdName {
	case "help", "h", "?":
		m.doHelp(cmdArgs)
		return m, nil

	case "exit", "quit", "q":
		m.quitting = true
		m.doExit()
		return m, tea.Quit

	case "new", "reset":
		return m, func() tea.Msg { return newSessionMsg{} }

	case "model":
		m.doModel(cmdArgs)
		return m, nil

	case "stream":
		m.streamingOn = !m.streamingOn
		status := "enabled"
		if !m.streamingOn {
			status = "disabled"
		}
		m.addMessage("system", fmt.Sprintf("Streaming %s.", status))
		m.refreshViewport()
		return m, nil

	case "compress":
		m.agent.EnableCompression(true)
		m.addMessage("system", "Compression enabled. Will trigger automatically when context exceeds threshold.")
		m.refreshViewport()
		return m, nil

	case "usage":
		m.doUsage()
		return m, nil

	case "tools":
		m.doTools()
		return m, nil

	case "skills":
		m.doSkills()
		return m, nil

	case "undo":
		m.doUndo()
		return m, nil

	case "retry":
		return m.doRetry()

	case "stop":
		if m.cancel != nil {
			m.cancel()
		}
		m.streaming = false
		m.addMessage("system", "Generation stopped.")
		m.refreshViewport()
		return m, nil

	case "save":
		m.doSave(cmdArgs)
		return m, nil

	case "load":
		m.doLoad(cmdArgs)
		return m, nil

	case "clear", "cls":
		return m, func() tea.Msg { return clearMsg{} }

	case "history":
		m.doHistory()
		return m, nil

	case "insights":
		m.doInsights(cmdArgs)
		return m, nil

	case "export":
		m.doExport(cmdArgs)
		return m, nil

	case "req":
		m.doAddRequirement(cmdArgs)
		return m, nil

	case "reqs", "requirements":
		m.doListRequirements()
		return m, nil

	case "req-done":
		m.doCompleteRequirement(cmdArgs)
		return m, nil

	case "req-del":
		m.doDeleteRequirement(cmdArgs)
		return m, nil

	case "req-priority":
		m.doSetRequirementPriority(cmdArgs)
		return m, nil

	case "context", "ctx":
		m.doShowContext()
		return m, nil

	case "goal":
		m.doGoal(cmdArgs)
		return m, nil

	case "kanban", "kb":
		m.doKanban(cmdArgs)
		return m, nil

	case "mode":
		m.doMode(cmdArgs)
		return m, nil

	default:
		m.addMessage("error", fmt.Sprintf("Unknown command: /%s (type /help for commands)", cmdName))
		m.refreshViewport()
		return m, nil
	}
}

// ---------------------------------------------------------------------------
// Send message (chat)
// ---------------------------------------------------------------------------

func (m TUIModel) sendMessage(input string) (tea.Model, tea.Cmd) {
	m.lastUserInput = input

	// Add user message to display
	m.addMessage("user", input)

	// Add a placeholder assistant message for streaming
	m.addStreamingMessage("assistant", "", true)

	m.refreshViewport()
	m.viewport.GotoBottom()

	// Create context
	ctx, cancel := context.WithCancel(context.Background())
	m.ctx = ctx
	m.cancel = cancel
	m.streaming = true
	m.streamBuf.Reset()
	m.statusText = "generating..."

	if m.streamingOn {
		return m, m.startStreaming(input)
	}
	return m, m.startNonStreaming(input)
}

// ---------------------------------------------------------------------------
// Streaming conversation
// ---------------------------------------------------------------------------

func (m TUIModel) startStreaming(input string) tea.Cmd {
	agentRef := m.agent
	ctx := m.ctx
	ch := m.streamCh

	return func() tea.Msg {
		err := agentRef.RunConversationStream(ctx, input, func(content string, done bool) {
			if done {
				ch <- streamMsg{content: "", done: true}
			} else {
				ch <- streamMsg{content: content, done: false}
			}
		})
		if err != nil {
			return errMsg{err: err}
		}
		return nil
	}
}

// toolResultRegex matches tool result markers
var toolResultStartRegex = regexp.MustCompile(`>>>TOOL_RESULT_START\|([^<]+)<<<`)
var toolResultEndRegex = regexp.MustCompile(`>>>TOOL_RESULT_END<<<`)

func (m TUIModel) handleStreamMsg(msg streamMsg) (tea.Model, tea.Cmd) {
	if msg.done {
		// Finalize the streaming message
		for i := len(m.messages) - 1; i >= 0; i-- {
			if m.messages[i].Streaming {
				m.messages[i].Content = m.streamBuf.String()
				m.messages[i].Streaming = false
				break
			}
		}
		m.streaming = false
		m.statusText = "ready"
		m.saveSession()
		m.input.Focus()
	} else {
		// Check if this is a turn start marker (for multi-turn conversations with tool calls)
		if strings.Contains(msg.content, ">>>TURN_START<<<") {
			// Finalize current streaming message if any
			for i := len(m.messages) - 1; i >= 0; i-- {
				if m.messages[i].Streaming {
					m.messages[i].Content = m.streamBuf.String()
					m.messages[i].Streaming = false
					break
				}
			}
			// Reset stream buffer and create new streaming message for the next turn
			m.streamBuf.Reset()
			m.messages = append(m.messages, ChatMessage{
				Role:      "assistant",
				Content:   "",
				Timestamp: time.Now(),
				Streaming: true,
			})
			// Remove the marker from content
			content := strings.ReplaceAll(msg.content, ">>>TURN_START<<<", "")
			if content != "" {
				m.streamBuf.WriteString(content)
			}
		} else if strings.Contains(msg.content, ">>>TOOL_RESULT_START|") {
			// Extract tool results and create separate tool messages
			content := msg.content
			for {
				startMatch := toolResultStartRegex.FindStringIndex(content)
				if startMatch == nil {
					break
				}
				endMatch := toolResultEndRegex.FindStringIndex(content)
				if endMatch == nil {
					break
				}

				// Extract tool name
				submatch := toolResultStartRegex.FindStringSubmatch(content[startMatch[0]:startMatch[1]])
				if len(submatch) < 2 {
					break
				}
				toolName := submatch[1]
				// Extract tool content
				toolContent := content[startMatch[1]:endMatch[0]]

				// Add tool message
				m.messages = append(m.messages, ChatMessage{
					Role:      "tool",
					Content:   fmt.Sprintf("[%s] %s", toolName, strings.TrimSpace(toolContent)),
					Timestamp: time.Now(),
					Streaming: false,
				})

				// Remove processed part from content
				content = content[:startMatch[0]] + content[endMatch[1]:]
			}

			// Append remaining content to stream buffer
			if content != "" {
				m.streamBuf.WriteString(content)
			}
		} else {
			m.streamBuf.WriteString(msg.content)
		}

		// Update the streaming message
		for i := len(m.messages) - 1; i >= 0; i-- {
			if m.messages[i].Streaming {
				m.messages[i].Content = m.streamBuf.String()
				break
			}
		}
	}
	m.contentDirty = true
	// Re-render content
	m.viewport.SetContent(m.renderMessages())
	// Only auto-scroll if user is near the bottom (within 5 lines)
	if m.viewport.AtBottom() || m.viewport.TotalLineCount()-m.viewport.YOffset <= 5 {
		m.viewport.GotoBottom()
	}
	return m, m.waitForActivity()
}

// ---------------------------------------------------------------------------
// Non-streaming conversation
// ---------------------------------------------------------------------------

func (m TUIModel) startNonStreaming(input string) tea.Cmd {
	agentRef := m.agent
	ctx := m.ctx

	return func() tea.Msg {
		response, err := agentRef.RunConversation(ctx, input)
		if err != nil {
			return errMsg{err: err}
		}
		return responseMsg{content: response}
	}
}

// ---------------------------------------------------------------------------
// View rendering
// ---------------------------------------------------------------------------

// View renders the entire TUI
func (m TUIModel) View() string {
	if m.quitting {
		return ""
	}

	// Title bar (1 line)
	title := m.renderTitle()

	// Messages (viewport) - only re-render if content changed
	if m.contentDirty {
		m.viewport.SetContent(m.renderMessages())
	}
	messagesView := m.viewport.View()

	// Force viewport to exactly its configured height to prevent overflow
	vpHeight := m.height - 3 // title(1) + input(1) + status(1)
	if vpHeight > 0 {
		messagesView = lipgloss.NewStyle().Height(vpHeight).MaxHeight(vpHeight).Render(messagesView)
	}

	// Input area (1 line)
	inputView := m.renderInput()

	// Status bar (1 line)
	statusView := m.renderStatus()

	// Combine
	return lipgloss.JoinVertical(lipgloss.Left,
		title,
		messagesView,
		inputView,
		statusView,
	)
}

func (m TUIModel) renderTitle() string {
	left := " Magic Agent "
	center := fmt.Sprintf(" %s ", m.modelName)
	right := fmt.Sprintf(" #%d ", m.sessionNum)

	modeLabel := "CHAT"
	modeColor := lipgloss.Color("#9ca3af")
	if m.codingMode {
		modeLabel = "CODING"
		modeColor = lipgloss.Color("#f97316")
	}
	modeStyled := lipgloss.NewStyle().Bold(true).Foreground(modeColor).Background(lipgloss.Color("#1a1a2e")).Render(fmt.Sprintf(" [%s] ", modeLabel))

	leftStyled := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00d2ff")).Background(lipgloss.Color("#1a1a2e")).Render(left)
	centerStyled := lipgloss.NewStyle().Foreground(lipgloss.Color("#9ca3af")).Background(lipgloss.Color("#1a1a2e")).Render(center)
	rightStyled := lipgloss.NewStyle().Foreground(lipgloss.Color("#facc15")).Background(lipgloss.Color("#1a1a2e")).Render(right)

	gap := m.width - lipgloss.Width(leftStyled) - lipgloss.Width(modeStyled) - lipgloss.Width(centerStyled) - lipgloss.Width(rightStyled)
	if gap < 0 {
		gap = 0
	}

	midBar := lipgloss.NewStyle().Background(lipgloss.Color("#1a1a2e")).Render(strings.Repeat(" ", gap))

	return lipgloss.NewStyle().Background(lipgloss.Color("#1a1a2e")).Render(
		leftStyled + midBar + modeStyled + midBar + centerStyled + midBar + rightStyled,
	)
}

func (m TUIModel) renderInput() string {
	return m.input.View()
}

func (m TUIModel) renderStatus() string {
	streamStatus := "off"
	if m.streamingOn {
		streamStatus = "on"
	}
	msgCount := 0
	for _, msg := range m.messages {
		if msg.Role == "user" || msg.Role == "assistant" {
			msgCount++
		}
	}

	modeTag := "[CHAT]"
	if m.codingMode {
		modeTag = "[CODING]"
	}

	parts := []string{
		modeTag,
		fmt.Sprintf("stream: %s", streamStatus),
		fmt.Sprintf("msgs: %d", msgCount),
		m.statusText,
	}

	if m.streaming {
		parts[3] = fmt.Sprintf("%s %s", m.spinner.View(), m.statusText)
	}

	content := strings.Join(parts, " | ")
	return statusBarStyle.Width(m.width).Render(content)
}

func (m TUIModel) renderMessages() string {
	var b strings.Builder

	// Limit total message count to prevent memory issues
	maxMessages := 100
	startIdx := 0
	if len(m.messages) > maxMessages {
		startIdx = len(m.messages) - maxMessages
	}

	for _, msg := range m.messages[startIdx:] {
		switch msg.Role {
		case "user":
			b.WriteString(userStyle.Render("> "))
			b.WriteString(utils.Truncate(msg.Content, 2000))
			b.WriteString("\n\n")

		case "assistant":
			if msg.Streaming && msg.Content == "" {
				// Show spinner while waiting for first content
				b.WriteString(m.spinner.View())
				b.WriteString(" Thinking...\n\n")
			} else if msg.Content != "" {
				// Truncate long content to prevent viewport overflow
				content := utils.Truncate(msg.Content, 50000)
				rendered := renderMarkdown(content)
				b.WriteString(rendered)
				b.WriteString("\n\n")
			}

		case "system":
			b.WriteString(systemStyle.Render("[System] "))
			b.WriteString(msg.Content)
			b.WriteString("\n\n")

		case "tool":
			b.WriteString(toolStyle.Render("[Tool] "))
			b.WriteString(utils.Truncate(msg.Content, 2000))
			b.WriteString("\n\n")

		case "error":
			b.WriteString(errorStyle.Render("[Error] "))
			b.WriteString(msg.Content)
			b.WriteString("\n\n")
		}
	}

	return b.String()
}

// ---------------------------------------------------------------------------
// Slash command implementations
// ---------------------------------------------------------------------------

func (m *TUIModel) doHelp(args string) {
	var helpText string

	switch strings.ToLower(strings.TrimSpace(args)) {
	case "req", "reqs", "requirement":
		helpText = `
 Requirements
   /req <text>              Add a new requirement
   /reqs                    List all pending requirements
   /req-done <id>           Mark requirement completed
   /req-del <id>            Delete a requirement
   /req-priority <id> <L>   Set priority (high/med/low)
   /context                 Show context + pending requirements`

	case "goal":
		helpText = `
 Goals
   /goal <text>    Set persistent goal (auto-continues)
   /goal status    Show current goal status
   /goal pause     Pause active goal
   /goal resume    Resume paused goal
   /goal clear     Clear current goal
   /subgoal <text> Add sub-goal to active goal`

	case "kanban":
		helpText = `
 Kanban
   /kanban                    Show kanban board
   /kanban create <title>     Create task
   /kanban list [search]      List tasks
   /kanban show <id>          Show task details
   /kanban start <id>         Start task
   /kanban complete <id>      Complete task
   /kanban delete <id>        Delete task`

	case "session":
		helpText = `
 Session
   /save [name]       Save current session
   /load [name]       Load a saved session
   /export [format]   Export conversation (text/json)`

	default:
		helpText = `
 Magic Agent Commands

 Navigation
   /new, /reset        New conversation
   /exit, /quit        Exit

 Conversation
   /undo               Undo last response
   /retry              Retry last message
   /stop               Stop generation
   /clear              Clear history
   /handoff [model]    Transfer session to another model

 Mode
   /mode [coding|chat] Switch mode

 Model & Info
   /model [name]       Show/set model
   /stream             Toggle streaming
   /tools              List tools
   /skills             List skills
   /usage              Token usage

 Tools
   /clarify [question] Ask user for clarification
   /interrupt [reason] Interrupt agent execution

 More: /help req  /help goal  /help kanban  /help session

Tips: Enter send | Shift+Enter newline | Up/Down history | PgUp/PgDn scroll`
	}

	m.addMessage("system", helpText)
	m.refreshViewport()
}

func (m *TUIModel) doNewSession() {
	m.saveSession()
	m.agent.Reset()
	m.sessionID = uuid.New().String()
	m.agent.SetSession(m.sessionID)
	m.sessionNum++

	// Reload skills
	if mgr, err := skills.NewManager(); err == nil {
		if skillsCtx := mgr.GetSkillsList(); skillsCtx != "" {
			m.agent.AddSkillsContext(skillsCtx)
		}
	}

	m.addMessage("system", fmt.Sprintf("New conversation started (#%d)", m.sessionNum))
	m.refreshViewport()
}

func (m *TUIModel) doModel(args string) {
	if args == "" {
		m.addMessage("system", fmt.Sprintf("Current model: %s", m.modelName))
	} else {
		m.addMessage("system", fmt.Sprintf("Model change to '%s' requires restart.", args))
	}
	m.refreshViewport()
}

func (m *TUIModel) doUsage() {
	history := m.agent.GetHistory()
	msgCount := len(history)

	inputTokens := 0
	for _, msg := range history {
		inputTokens += estimateTokens(msg.Content)
	}

	usageText := fmt.Sprintf(`
 Usage Statistics
   Messages:      %d
   Est. Tokens:  %d
   Session:      #%d
`, msgCount, inputTokens, m.sessionNum)

	m.addMessage("system", usageText)
	m.refreshViewport()
}

func (m *TUIModel) doTools() {
	tools := m.registry.List()
	var b strings.Builder
	b.WriteString(fmt.Sprintf("\n Available Tools (%d)\n\n", len(tools)))

	for _, tName := range tools {
		t, err := m.registry.Get(tName)
		if err != nil {
			continue
		}
		desc := t.Description()
		b.WriteString(fmt.Sprintf("  %-25s %s\n", tName, desc))
	}

	m.addMessage("system", b.String())
	m.refreshViewport()
}

func (m *TUIModel) doSkills() {
	var b strings.Builder
	b.WriteString("\n Available Skills\n\n")

	if mgr, err := skills.NewManager(); err == nil {
		skillList := mgr.ListSkills()
		if len(skillList) == 0 {
			b.WriteString("  No skills installed.\n")
		} else {
			for _, skill := range skillList {
				b.WriteString(fmt.Sprintf("  - %s\n", skill))
			}
		}
	} else {
		b.WriteString("  Skills manager not available.\n")
	}

	m.addMessage("system", b.String())
	m.refreshViewport()
}

func (m *TUIModel) doUndo() {
	history := m.agent.GetHistory()
	if len(history) < 2 {
		m.addMessage("system", "Nothing to undo.")
		m.refreshViewport()
		return
	}

	// Find last user message index
	userIdx := -1
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "user" {
			userIdx = i
			break
		}
	}

	if userIdx < 0 {
		m.addMessage("system", "Nothing to undo.")
	} else {
		m.agent.SetHistory(history[:userIdx])
		m.addMessage("system", "Undo successful. Last response removed.")
	}
	m.refreshViewport()
}

func (m *TUIModel) doRetry() (tea.Model, tea.Cmd) {
	history := m.agent.GetHistory()
	if len(history) < 2 {
		m.addMessage("system", "No message to retry.")
		m.refreshViewport()
		return m, nil
	}

	// Find last user message
	userIdx := -1
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "user" {
			userIdx = i
			break
		}
	}

	if userIdx < 0 {
		m.addMessage("system", "No message to retry.")
		m.refreshViewport()
		return m, nil
	}

	lastUserMsg := history[userIdx].Content

	// Remove assistant response
	m.agent.SetHistory(history[:userIdx+1])

	m.addMessage("system", fmt.Sprintf("Retrying: \"%s\"", utils.Truncate(lastUserMsg, 50)))
	m.refreshViewport()

	// Send the retry message
	return m.sendMessage(lastUserMsg)
}

func (m *TUIModel) doSave(name string) {
	saveName := name
	if saveName == "" {
		saveName = fmt.Sprintf("session_%d", time.Now().Unix())
	}

	sess := &session.Session{
		ID:       m.sessionID,
		Profile:  m.cfg.Profile,
		Platform: "tui",
		Messages: m.agent.GetHistory(),
	}

	if err := m.store.SaveSession(context.Background(), sess); err != nil {
		m.addMessage("error", fmt.Sprintf("Failed to save session: %v", err))
	} else {
		shortID := m.sessionID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		m.addMessage("system", fmt.Sprintf("Session saved (%s) - %d messages", shortID, len(sess.Messages)))
	}
	m.refreshViewport()
}

func (m *TUIModel) doLoad(id string) {
	sessions, err := m.store.ListSessions(context.Background(), m.cfg.Profile)
	if err != nil {
		m.addMessage("error", fmt.Sprintf("Failed to load sessions: %v", err))
		m.refreshViewport()
		return
	}

	var sess *session.Session
	if id != "" {
		for _, s := range sessions {
			if s.ID == id {
				sess = s
				break
			}
		}
		if sess == nil {
			m.addMessage("error", fmt.Sprintf("Session '%s' not found", id))
			m.refreshViewport()
			return
		}
	} else {
		// Load most recent
		if len(sessions) > 0 {
			sess = sessions[0]
		}
	}

	if sess == nil {
		m.addMessage("system", "No sessions found")
		m.refreshViewport()
		return
	}

	m.agent.SetHistory(sess.Messages)
	m.sessionID = sess.ID
	m.sessionNum++

	shortID := sess.ID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	m.addMessage("system", fmt.Sprintf("Loaded session %s (%d messages)", shortID, len(sess.Messages)))
	m.refreshViewport()
}

func (m *TUIModel) doClear() {
	m.agent.Reset()

	// Reload skills
	if mgr, err := skills.NewManager(); err == nil {
		if skillsCtx := mgr.GetSkillsList(); skillsCtx != "" {
			m.agent.AddSkillsContext(skillsCtx)
		}
	}

	m.addMessage("system", "Conversation cleared.")
	m.refreshViewport()
}

func (m *TUIModel) doHistory() {
	history := m.agent.GetHistory()
	var b strings.Builder
	b.WriteString(fmt.Sprintf("\n History (%d messages)\n\n", len(history)))

	for i, msg := range history {
		role := msg.Role
		content := msg.Content
		if len(content) > 60 {
			content = content[:57] + "..."
		}
		content = strings.ReplaceAll(content, "\n", " ")
		if content == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("  [%2d] %-10s: %s\n", i, role, content))
	}

	m.addMessage("system", b.String())
	m.refreshViewport()
}

func (m *TUIModel) doInsights(args string) {
	days := 7
	if strings.Contains(args, "--days") {
		re := regexp.MustCompile(`--days\s+(\d+)`)
		matches := re.FindStringSubmatch(args)
		if len(matches) > 1 {
			if d, err := strconv.Atoi(matches[1]); err == nil {
				days = d
			}
		}
	}

	history := m.agent.GetHistory()
	msgCount := len(history)

	sessionCount := 1
	sessions, _ := m.store.ListSessions(context.Background(), m.cfg.Profile)
	if sessions != nil {
		sessionCount = len(sessions) + 1
	}

	avgMessages := msgCount
	if sessionCount > 0 {
		avgMessages = msgCount / sessionCount
	}

	insightsText := fmt.Sprintf(`
 Insights (last %d days)
   Total Sessions:      %d
   Current Session Msgs: %d
   Avg Messages/Session: %d
`, days, sessionCount, msgCount, avgMessages)

	m.addMessage("system", insightsText)
	m.refreshViewport()
}

func (m *TUIModel) doExport(args string) {
	format := strings.TrimSpace(args)
	if format == "" {
		format = "text"
	}

	history := m.agent.GetHistory()
	var output strings.Builder

	switch format {
	case "text":
		for _, msg := range history {
			output.WriteString(fmt.Sprintf("[%s]\n%s\n\n", msg.Role, msg.Content))
		}
	case "json":
		output.WriteString("[")
		for i, msg := range history {
			if i > 0 {
				output.WriteString(",")
			}
			output.WriteString(fmt.Sprintf(`{"role":"%s","content":"%s"}`, msg.Role, escapeJSONTUI(msg.Content)))
		}
		output.WriteString("]")
	default:
		m.addMessage("error", fmt.Sprintf("Unknown export format: %s (use: text, json)", format))
		m.refreshViewport()
		return
	}

	// Write to file
	filename := fmt.Sprintf("export_%s_%d.%s", format, time.Now().Unix(), format)
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".magic", "exports", filename)

	os.MkdirAll(filepath.Dir(path), 0755)
	if err := os.WriteFile(path, []byte(output.String()), 0644); err != nil {
		m.addMessage("error", fmt.Sprintf("Failed to export: %v", err))
	} else {
		m.addMessage("system", fmt.Sprintf("Exported to %s", path))
	}
	m.refreshViewport()
}

// ---------------------------------------------------------------------------
// Requirement management commands
// ---------------------------------------------------------------------------

func (m *TUIModel) doAddRequirement(args string) {
	if args == "" {
		m.addMessage("system", "Usage: /req <requirement description>\n  /req --priority high Fix login bug")
		m.refreshViewport()
		return
	}

	// Stop current generation if running
	if m.cancel != nil {
		m.cancel()
		ctx, cancel := context.WithCancel(context.Background())
		m.ctx = ctx
		m.cancel = cancel
	}

	text, priority := parseReqArgsTUI(args)
	if text == "" {
		m.addMessage("error", "Requirement text cannot be empty")
		m.refreshViewport()
		return
	}

	todoTool := tool.GetTodoTool()
	res, err := todoTool.Execute(m.ctx, map[string]interface{}{
		"action":      "create",
		"title":       text,
		"priority":    priority,
		"description": fmt.Sprintf("Added via TUI (session: %s)", m.sessionID[:8]),
	})
	if err != nil {
		m.addMessage("error", fmt.Sprintf("Failed to add requirement: %v", err))
		m.refreshViewport()
		return
	}

	todoID := ""
	if resMap, ok := res.(map[string]interface{}); ok {
		if id, exists := resMap["id"]; exists {
			todoID = fmt.Sprintf("%v", id)
		}
	}

	resultText := fmt.Sprintf("New Requirement Added [%s] %s", strings.ToUpper(priority), text)
	if todoID != "" {
		short := todoID
		if len(short) > 20 {
			short = short[:20]
		}
		resultText += fmt.Sprintf("\n  ID: %s", short)
	}

	m.addMessage("system", resultText)
	m.saveSession()
	m.refreshViewport()
}

func (m *TUIModel) doListRequirements() {
	todoTool := tool.GetTodoTool()
	raw, err := todoTool.Execute(context.Background(), map[string]interface{}{
		"action": "list",
	})
	if err != nil {
		m.addMessage("error", fmt.Sprintf("Failed to list requirements: %v", err))
		m.refreshViewport()
		return
	}

	resMap, ok := raw.(map[string]interface{})
	if !ok {
		m.addMessage("system", "No requirements found.")
		m.refreshViewport()
		return
	}

	total, _ := resMap["total"].(float64)
	todos, _ := resMap["todos"].([]interface{})

	var b strings.Builder
	b.WriteString(fmt.Sprintf("\n Pending Requirements (%d)\n\n", int(total)))

	if int(total) == 0 {
		b.WriteString("  No pending requirements.\n")
	} else {
		for _, item := range todos {
			todo, _ := item.(map[string]interface{})
			id, _ := todo["id"].(string)
			title, _ := todo["title"].(string)
			status, _ := todo["status"].(string)
			priority, _ := todo["priority"].(string)

			if status != "pending" && status != "in_progress" {
				continue
			}

			priorityIcon := map[string]string{"high": "[H]", "medium": "[M]", "low": "[L]"}
			icon := priorityIcon[priority]
			if icon == "" {
				icon = "[M]"
			}

			displayTitle := title
			if len(displayTitle) > 50 {
				displayTitle = displayTitle[:47] + "..."
			}

			shortID := id
			if len(shortID) > 12 {
				shortID = shortID[:12]
			}

			b.WriteString(fmt.Sprintf("  %s %-50s (%s)\n", icon, displayTitle, shortID))
		}
	}

	m.addMessage("system", b.String())
	m.refreshViewport()
}

func (m *TUIModel) doCompleteRequirement(id string) {
	if id == "" {
		m.addMessage("system", "Usage: /req-done <requirement-id>")
		m.refreshViewport()
		return
	}

	todoTool := tool.GetTodoTool()
	raw, err := todoTool.Execute(context.Background(), map[string]interface{}{
		"action": "complete",
		"id":     id,
	})
	if err != nil {
		m.addMessage("error", fmt.Sprintf("Failed to complete requirement: %v", err))
	} else {
		title := ""
		if resMap, ok := raw.(map[string]interface{}); ok {
			if t, exists := resMap["title"]; exists {
				title = fmt.Sprintf("%v", t)
			}
		}
		m.addMessage("system", fmt.Sprintf("Requirement completed: %s", title))
	}
	m.refreshViewport()
}

func (m *TUIModel) doDeleteRequirement(id string) {
	if id == "" {
		m.addMessage("system", "Usage: /req-del <requirement-id>")
		m.refreshViewport()
		return
	}

	todoTool := tool.GetTodoTool()
	_, err := todoTool.Execute(context.Background(), map[string]interface{}{
		"action": "delete",
		"id":     id,
	})
	if err != nil {
		m.addMessage("error", fmt.Sprintf("Failed to delete requirement: %v", err))
	} else {
		m.addMessage("system", fmt.Sprintf("Requirement deleted: %s", id))
	}
	m.refreshViewport()
}

func (m *TUIModel) doSetRequirementPriority(args string) {
	parts := strings.Fields(args)
	if len(parts) < 2 {
		m.addMessage("system", "Usage: /req-priority <id> <high|medium|low>")
		m.refreshViewport()
		return
	}

	id := parts[0]
	priority := strings.ToLower(parts[1])

	validPriorities := map[string]bool{"high": true, "medium": true, "low": true}
	if !validPriorities[priority] {
		m.addMessage("error", fmt.Sprintf("Invalid priority: %s (use: high, medium, low)", priority))
		m.refreshViewport()
		return
	}

	todoTool := tool.GetTodoTool()
	raw, err := todoTool.Execute(context.Background(), map[string]interface{}{
		"action":   "update",
		"id":       id,
		"priority": priority,
	})
	if err != nil {
		m.addMessage("error", fmt.Sprintf("Failed to update priority: %v", err))
	} else {
		title := ""
		if resMap, ok := raw.(map[string]interface{}); ok {
			if t, exists := resMap["title"]; exists {
				title = fmt.Sprintf("%v", t)
			}
		}
		m.addMessage("system", fmt.Sprintf("Priority set: %s -> %s", title, strings.ToUpper(priority)))
	}
	m.refreshViewport()
}

func (m *TUIModel) doShowContext() {
	history := m.agent.GetHistory()
	msgCount := len(history)
	ctxSize := 0
	for _, msg := range history {
		ctxSize += len(msg.Role) + len(msg.Content)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("\n Conversation Context\n"))
	b.WriteString(fmt.Sprintf("   Session:     %s...\n", m.sessionID[:8]))
	b.WriteString(fmt.Sprintf("   Messages:    %d\n", msgCount))
	b.WriteString(fmt.Sprintf("   Context:     %d chars\n", ctxSize))
	b.WriteString(fmt.Sprintf("   Model:       %s\n", m.modelName))

	// Show pending requirements
	b.WriteString("\n   Pending Requirements:\n")
	todoTool := tool.GetTodoTool()
	raw, err := todoTool.Execute(context.Background(), map[string]interface{}{
		"action": "list",
	})
	pendingCount := 0
	if err == nil {
		if resMap, ok := raw.(map[string]interface{}); ok {
			todos, _ := resMap["todos"].([]interface{})
			for _, item := range todos {
				todo, _ := item.(map[string]interface{})
				status, _ := todo["status"].(string)
				if status == "pending" || status == "in_progress" {
					pendingCount++
					title, _ := todo["title"].(string)
					shortTitle := title
					if len(shortTitle) > 45 {
						shortTitle = shortTitle[:42] + "..."
					}
					b.WriteString(fmt.Sprintf("     - %s\n", shortTitle))
				}
			}
		}
	}
	if pendingCount == 0 {
		b.WriteString("     (no pending requirements)\n")
	}

	m.addMessage("system", b.String())
	m.refreshViewport()
}

// ---------------------------------------------------------------------------
// Goal management commands
// ---------------------------------------------------------------------------

func (m *TUIModel) doGoal(args string) {
	home, _ := os.UserHomeDir()
	goalsDir := filepath.Join(home, ".magic", "goals")

	// Initialize goal manager if needed
	if m.goalManager == nil {
		prov := m.agent.GetProvider()
		if prov == nil {
			m.addMessage("error", "Goal manager requires AI provider (not available)")
			m.refreshViewport()
			return
		}
		m.goalManager = agent.NewGoalManager(prov, goalsDir)
		maxTurns := m.cfg.Agent.GoalMaxTurns
		if maxTurns <= 0 {
			maxTurns = 20
		}
		m.goalManager.SetMaxTurns(maxTurns)

		// Try to load saved goal
		_ = m.goalManager.Load(m.sessionID)
	}

	parts := strings.SplitN(strings.TrimSpace(args), " ", 2)
	subcmd := parts[0]

	switch subcmd {
	case "", "status":
		goal := m.goalManager.GetStatus()
		if goal == nil {
			m.addMessage("system", "No active goal")
		} else {
			m.addMessage("system", fmt.Sprintf("Goal: %s\nState: %s | Turns: %d/%d", goal.Text, goal.State, goal.TurnCount, goal.MaxTurns))
		}
	case "pause":
		goal := m.goalManager.Pause()
		if goal != nil {
			m.addMessage("system", fmt.Sprintf("Goal paused: %s", goal.Text))
			m.goalManager.SaveWithSessionID(m.sessionID)
		} else {
			m.addMessage("system", "No active goal to pause")
		}
	case "resume":
		goal := m.goalManager.Resume()
		if goal != nil {
			m.addMessage("system", fmt.Sprintf("Goal resumed: %s (turn counter reset)", goal.Text))
			m.goalManager.SaveWithSessionID(m.sessionID)
		} else {
			m.addMessage("system", "No paused goal to resume")
		}
	case "clear":
		m.goalManager.Clear()
		m.addMessage("system", "Goal cleared")
	default:
		goalText := strings.TrimSpace(args)
		if goalText == "" {
			m.addMessage("system", "Usage: /goal <text> | /goal status | /goal pause | /goal resume | /goal clear")
		} else {
			goal := m.goalManager.SetGoal(goalText)
			maxTurns := m.cfg.Agent.GoalMaxTurns
			if maxTurns <= 0 {
				maxTurns = 20
			}
			goal.MaxTurns = maxTurns
			m.goalManager.SetMaxTurns(maxTurns)
			m.addMessage("system", fmt.Sprintf("Goal set: %s (max %d turns)", goal.Text, goal.MaxTurns))
			m.goalManager.SaveWithSessionID(m.sessionID)
		}
	}
	m.refreshViewport()
}

// ---------------------------------------------------------------------------
// Kanban commands
// ---------------------------------------------------------------------------

func (m *TUIModel) doKanban(args string) {
	home, _ := os.UserHomeDir()
	mgr, err := kanban.NewManager(filepath.Join(home, ".magic"))
	if err != nil {
		m.addMessage("error", fmt.Sprintf("Failed to initialize kanban: %v", err))
		m.refreshViewport()
		return
	}
	if err := mgr.Init(); err != nil {
		m.addMessage("error", fmt.Sprintf("Failed to init kanban: %v", err))
		m.refreshViewport()
		return
	}
	defer mgr.Close()

	parts := strings.SplitN(strings.TrimSpace(args), " ", 2)
	subcmd := parts[0]
	subargs := ""
	if len(parts) > 1 {
		subargs = parts[1]
	}

	switch subcmd {
	case "", "board":
		m.doKanbanBoard(mgr)
	case "create":
		m.doKanbanCreate(mgr, subargs)
	case "list", "ls":
		m.doKanbanList(mgr, subargs)
	case "show":
		m.doKanbanShow(mgr, subargs)
	case "start":
		m.doKanbanStart(mgr, subargs)
	case "complete", "done":
		m.doKanbanComplete(mgr, subargs)
	case "delete", "del":
		m.doKanbanDelete(mgr, subargs)
	case "stats":
		m.doKanbanStats(mgr)
	default:
		m.addMessage("system", `Kanban commands:
  /kanban              - Show board
  /kanban create <t>   - Create task
  /kanban list [q]     - List tasks
  /kanban show <id>    - Show task
  /kanban start <id>   - Start task
  /kanban complete <id> - Complete task
  /kanban delete <id>  - Delete task
  /kanban stats        - Show statistics`)
	}
	m.refreshViewport()
}

func (m *TUIModel) doKanbanBoard(mgr *kanban.Manager) {
	board, err := mgr.GetBoard("")
	if err != nil {
		m.addMessage("error", fmt.Sprintf("Failed to get board: %v", err))
		return
	}

	statuses := []kanban.TaskStatus{
		kanban.StatusTriage, kanban.StatusTodo, kanban.StatusReady,
		kanban.StatusRunning, kanban.StatusBlocked, kanban.StatusDone,
	}
	statusLabels := map[kanban.TaskStatus]string{
		kanban.StatusTriage:  "Triage",
		kanban.StatusTodo:    "Todo",
		kanban.StatusReady:   "Ready",
		kanban.StatusRunning: "Running",
		kanban.StatusBlocked: "Blocked",
		kanban.StatusDone:    "Done",
	}

	var b strings.Builder
	b.WriteString("\n Kanban Board\n\n")

	for _, status := range statuses {
		tasks := board[status]
		label := statusLabels[status]
		b.WriteString(fmt.Sprintf(" %s (%d tasks)\n", label, len(tasks)))

		if len(tasks) == 0 {
			b.WriteString("   (empty)\n")
		} else {
			for _, task := range tasks {
				title := task.Title
				if len(title) > 55 {
					title = title[:52] + "..."
				}
				b.WriteString(fmt.Sprintf("   - [%s] %s\n", task.ID, title))
			}
		}
		b.WriteString("\n")
	}

	m.messages = append(m.messages, ChatMessage{
		Role:      "system",
		Content:   b.String(),
		Timestamp: time.Now(),
	})
}

func (m *TUIModel) doKanbanCreate(mgr *kanban.Manager, args string) {
	if args == "" {
		m.addMessage("system", "Usage: /kanban create <title>")
		return
	}

	assignee := os.Getenv("USER")
	if assignee == "" {
		assignee = "agent"
	}

	task, err := mgr.CreateTask(args, "", assignee)
	if err != nil {
		m.addMessage("error", fmt.Sprintf("Failed to create task: %v", err))
	} else {
		m.addMessage("system", fmt.Sprintf("Task created: %s\n  Title: %s\n  Status: %s", task.ID, task.Title, task.Status))
	}
}

func (m *TUIModel) doKanbanList(mgr *kanban.Manager, args string) {
	filter := kanban.TaskFilter{}
	if args != "" {
		filter.Search = args
	}

	tasks, err := mgr.ListTasks(filter)
	if err != nil {
		m.addMessage("error", fmt.Sprintf("Failed to list tasks: %v", err))
		return
	}

	if len(tasks) == 0 {
		m.addMessage("system", "No tasks found")
		return
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("\n Tasks (%d)\n\n", len(tasks)))
	for _, task := range tasks {
		title := task.Title
		if len(title) > 50 {
			title = title[:47] + "..."
		}
		assignee := ""
		if task.Assignee != "" {
			assignee = fmt.Sprintf(" @%s", task.Assignee)
		}
		b.WriteString(fmt.Sprintf("  [%s] %s - %s%s\n", task.Status, task.ID, title, assignee))
	}

	m.addMessage("system", b.String())
}

func (m *TUIModel) doKanbanShow(mgr *kanban.Manager, args string) {
	if args == "" {
		m.addMessage("system", "Usage: /kanban show <task-id>")
		return
	}

	task, err := mgr.GetTask(args)
	if err != nil {
		m.addMessage("error", fmt.Sprintf("Failed to get task: %v", err))
		return
	}

	body := task.Body
	if body == "" {
		body = "(no description)"
	}

	m.addMessage("system", fmt.Sprintf("Task: %s\nTitle: %s\nStatus: %s\nPriority: %d\nAssignee: %s\n\n%s",
		task.ID, task.Title, task.Status, task.Priority, task.Assignee, body))
}

func (m *TUIModel) doKanbanStart(mgr *kanban.Manager, args string) {
	if args == "" {
		m.addMessage("system", "Usage: /kanban start <task-id>")
		return
	}
	if _, err := mgr.StartTask(args); err != nil {
		m.addMessage("error", fmt.Sprintf("Failed to start task: %v", err))
	} else {
		m.addMessage("system", fmt.Sprintf("Task %s started", args))
	}
}

func (m *TUIModel) doKanbanComplete(mgr *kanban.Manager, args string) {
	if args == "" {
		m.addMessage("system", "Usage: /kanban complete <task-id>")
		return
	}
	if _, err := mgr.CompleteTask(args, ""); err != nil {
		m.addMessage("error", fmt.Sprintf("Failed to complete task: %v", err))
	} else {
		m.addMessage("system", fmt.Sprintf("Task %s completed", args))
	}
}

func (m *TUIModel) doKanbanDelete(mgr *kanban.Manager, args string) {
	if args == "" {
		m.addMessage("system", "Usage: /kanban delete <task-id>")
		return
	}
	if err := mgr.DeleteTask(args); err != nil {
		m.addMessage("error", fmt.Sprintf("Failed to delete task: %v", err))
	} else {
		m.addMessage("system", fmt.Sprintf("Task %s deleted", args))
	}
}

func (m *TUIModel) doKanbanStats(mgr *kanban.Manager) {
	board, err := mgr.GetBoard("")
	if err != nil {
		m.addMessage("error", fmt.Sprintf("Failed to get board: %v", err))
		return
	}

	total := 0
	var b strings.Builder
	b.WriteString("\n Kanban Statistics\n\n")

	statuses := []kanban.TaskStatus{
		kanban.StatusTriage, kanban.StatusTodo, kanban.StatusReady,
		kanban.StatusRunning, kanban.StatusBlocked, kanban.StatusDone,
	}
	statusLabels := map[kanban.TaskStatus]string{
		kanban.StatusTriage:  "Triage",
		kanban.StatusTodo:    "Todo",
		kanban.StatusReady:   "Ready",
		kanban.StatusRunning: "Running",
		kanban.StatusBlocked: "Blocked",
		kanban.StatusDone:    "Done",
	}

	for _, status := range statuses {
		count := len(board[status])
		total += count
		b.WriteString(fmt.Sprintf("  %-12s %d\n", statusLabels[status], count))
	}
	b.WriteString(fmt.Sprintf("  %-12s %d\n", "Total", total))

	m.addMessage("system", b.String())
}

// ---------------------------------------------------------------------------
// Mode management
// ---------------------------------------------------------------------------

func (m *TUIModel) doMode(args string) {
	args = strings.TrimSpace(strings.ToLower(args))

	switch args {
	case "coding":
		if m.codingMode {
			m.addMessage("system", "Already in CODING mode.")
		} else {
			m.codingMode = true
			m.applyCodingMode()
			m.addMessage("system", "Switched to CODING mode.\n- Elevated permissions: arbitrary commands, pipes, chains, command substitution\n- Extended timeouts: up to 10 min (commands), 10 min (code execution)\n- Memory limit: 4GB for code execution\n- New tools available: batch_file_ops, project_analyze, diff_patch\n- Dangerous commands (rm -rf /, shutdown, etc.) are still blocked")
		}
	case "chat":
		if !m.codingMode {
			m.addMessage("system", "Already in CHAT mode.")
		} else {
			m.codingMode = false
			m.applyCodingMode()
			m.addMessage("system", "Switched to CHAT mode. Standard safety restrictions applied.")
		}
	default:
		currentMode := "chat"
		if m.codingMode {
			currentMode = "coding"
		}
		m.addMessage("system", fmt.Sprintf("Current mode: %s\nUsage: /mode coding | /mode chat", currentMode))
	}
	m.refreshViewport()
}

// applyCodingMode updates the agent system prompt and tool restrictions based on current mode
func (m *TUIModel) applyCodingMode() {
	// Rebuild system prompt
	newPrompt := buildSystemPrompt(m.cfg, m.codingMode)

	// Update agent system prompt by replacing the first message in history
	history := m.agent.GetHistory()
	if len(history) > 0 && history[0].Role == "system" {
		history[0].Content = newPrompt
		m.agent.SetHistory(history)
	}

	// Update tool coding mode
	if m.registry != nil {
		if execTool, err := m.registry.Get("execute_command"); err == nil {
			if ect, ok := execTool.(*tool.ExecuteCommandTool); ok {
				ect.SetCodingMode(m.codingMode)
			}
		}
		if codeTool, err := m.registry.Get("execute_code"); err == nil {
			if ect, ok := codeTool.(*tool.ExecuteCodeTool); ok {
				ect.SetCodingMode(m.codingMode)
			}
		}
	}
}

func (m *TUIModel) saveSession() {
	if m.store == nil {
		return
	}

	// Get token stats from agent
	inputTokens, outputTokens, cacheReadTokens := m.agent.GetTokenStats()

	sess := &session.Session{
		ID:              m.sessionID,
		Profile:         m.cfg.Profile,
		Platform:        "tui",
		Messages:        m.agent.GetHistory(),
		InputTokens:     inputTokens,
		OutputTokens:    outputTokens,
		CacheReadTokens: cacheReadTokens,
	}

	// Use background context to avoid blocking
	_ = m.store.SaveSession(context.Background(), sess)
}

func (m *TUIModel) doExit() {
	m.saveSession()
}

// ---------------------------------------------------------------------------
// RunTUI - entry point
// ---------------------------------------------------------------------------

// RunTUI starts the TUI chat interface
func RunTUI(ctx context.Context, cfg *config.Config, prov provider.Provider, registry *tool.Registry, store *session.Store, opts ...func(*TUIModel)) error {
	// Generate tools schema for provider
	toolsSchema := getToolsSchema(registry)

	// Determine initial coding mode from config (chat_mode: "chat" or "coding")
	codingMode := cfg.ChatMode == "coding"

	// Build system prompt
	systemPrompt := buildSystemPrompt(cfg, codingMode)

	// Initialize agent
	aiAgent := agent.NewEnhancedAgent(prov, registry, toolsSchema, systemPrompt)

	// Generate session ID
	sessionID := uuid.New().String()
	aiAgent.SetSession(sessionID)

	// Load skills context (compact list only)
	if mgr, err := skills.NewManager(); err == nil {
		if skillsList := mgr.GetSkillsList(); skillsList != "" {
			aiAgent.AddSkillsContext(skillsList)
		}
	}

	// Create TUI model
	m := NewTUIModel(aiAgent, registry, store, cfg)

	// Apply initial coding mode if configured
	if codingMode {
		m.codingMode = true
		m.applyCodingMode()
	}

	// Apply options
	for _, opt := range opts {
		opt(&m)
	}

	// Run BubbleTea program
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

// WithStreaming enables streaming mode for RunTUI
func WithStreaming(enabled bool) func(*TUIModel) {
	return func(m *TUIModel) {
		m.streamingOn = enabled
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func escapeJSONTUI(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return s
	}
	return string(b)
}

func parseReqArgsTUI(args string) (string, string) {
	priority := "medium"
	text := args
	parts := strings.Fields(args)
	for i, p := range parts {
		if p == "--priority" || p == "-p" {
			if i+1 < len(parts) {
				pval := strings.ToLower(parts[i+1])
				switch pval {
				case "high", "h":
					priority = "high"
				case "low", "l":
					priority = "low"
				default:
					priority = "medium"
				}
				cleaned := make([]string, 0, len(parts)-2)
				for j, pp := range parts {
					if j == i || j == i+1 {
						continue
					}
					cleaned = append(cleaned, pp)
				}
				text = strings.Join(cleaned, " ")
			}
			break
		}
	}
	return strings.TrimSpace(text), priority
}

// estimateTokens provides a rough token count estimate.
// English: ~4 chars/token, CJK: ~2 chars/token, mixed: weighted average.
func estimateTokens(text string) int {
	if len(text) == 0 {
		return 0
	}
	cjk := 0
	other := 0
	for _, r := range text {
		if r >= 0x4E00 && r <= 0x9FFF || r >= 0x3400 && r <= 0x4DBF || r >= 0xF900 && r <= 0xFAFF {
			cjk++
		} else {
			other++
		}
	}
	// CJK: ~1.5 tokens per char, English: ~4 chars per token
	return cjk + other/4
}
