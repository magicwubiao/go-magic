package tui

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

// Styles defines lipgloss styles for the TUI
var (
	HeaderStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Bold(true)

	UserStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("86"))

	AssistantStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("75"))

	ErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	ToolStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("228")).
			Background(lipgloss.Color("235"))

	MetadataStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	BorderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("238"))

	PromptStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("86"))
)

// Message represents a chat message
type Message struct {
	Role      string    `json:"role"` // user, assistant, system, tool
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	Tool      string    `json:"tool,omitempty"`
	Streaming bool      `json:"-"`
}

// Session represents a chat session
type Session struct {
	ID         string
	Messages   []Message
	CreatedAt  time.Time
	Context    string
}

// Model represents the TUI state
type Model struct {
	session      *Session
	input        textarea.Model
	viewport     viewport.Model
	messages     []Message
	inputHistory []string
	historyPos   int
	commands     map[string]string
	streaming    bool
	mu           sync.RWMutex
	width        int
	height       int
	scrollPos    int
}

// NewModel creates a new TUI model
func NewModel(width, height int) *Model {
	input := textarea.New()
	input.SetHeight(3)
	input.SetWidth(width - 4)
	input.SetPlaceholder("Type a message... (Tab to autocomplete, ↑↓ for history)")
	input.Focus()

	viewport := viewport.New(width-4, height-10)
	viewport.SetContent("")

	return &Model{
		session: &Session{
			ID:        fmt.Sprintf("session_%d", time.Now().Unix()),
			CreatedAt: time.Now(),
			Messages:  make([]Message, 0),
		},
		input:        input,
		viewport:     viewport,
		messages:    make([]Message, 0),
		inputHistory: make([]string, 0),
		historyPos:   -1,
		commands:    newCommandRegistry(),
		width:       width,
		height:      height,
	}
}

// newCommandRegistry creates the slash command registry
func newCommandRegistry() map[string]string {
	return map[string]string{
		"/new":        "Start a new conversation",
		"/reset":      "Reset current conversation",
		"/model":      "Change the AI model (usage: /model provider:model)",
		"/personality": "Set agent personality",
		"/skills":     "List available skills",
		"/help":       "Show help",
		"/usage":      "Show token usage",
		"/compress":   "Compress context window",
		"/insights":   "Get usage insights",
		"/retry":      "Retry last response",
		"/undo":       "Undo last action",
		"/stop":       "Stop current operation",
		"/context":    "Manage context files",
		"/clear":      "Clear chat history",
		"/export":     "Export conversation",
		"/sessions":   "List sessions",
		"/status":     "Show system status",
		"/version":    "Show version",
		"/tools":      "List tools",
		"/prompt":     "Show/edit system prompt",
	}
}

// Resize handles window resize
func (m *Model) Resize(width, height int) {
	m.width = width
	m.height = height
	m.input.SetWidth(width - 4)
	m.viewport.Width = width - 4
	m.viewport.Height = height - 10
	m.updateViewport()
}

// updateViewport updates the viewport content
func (m *Model) updateViewport() {
	var buf bytes.Buffer

	// Header
	buf.WriteString(HeaderStyle.Render("═══ go-magic Chat ═══") + "\n\n")

	// Messages
	for _, msg := range m.messages {
		switch msg.Role {
		case "user":
			buf.WriteString(UserStyle.Render("You: ") + msg.Content + "\n\n")
		case "assistant":
			buf.WriteString(AssistantStyle.Render("Assistant: ") + msg.Content + "\n\n")
		case "tool":
			buf.WriteString(ToolStyle.Render(fmt.Sprintf("[%s] ", msg.Tool)) + msg.Content + "\n\n")
		case "system":
			buf.WriteString(MetadataStyle.Render(msg.Content) + "\n\n")
		}
	}

	// Scroll position indicator
	if len(m.messages) > 20 {
		buf.WriteString(MetadataStyle.Render(fmt.Sprintf("(%d/%d messages, scroll ↑↓)",
			m.scrollPos+20, len(m.messages))))
	}

	m.viewport.SetContent(buf.String())
}

// HandleInput processes keyboard input
func (m *Model) HandleInput(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch key {
	case "ctrl+c":
		return false
	case "ctrl+l":
		m.messages = nil
		m.updateViewport()
		return true
	case "tab":
		m.handleAutocomplete()
		return true
	case "up":
		m.historyBack()
		return true
	case "down":
		m.historyForward()
		return true
	case "enter":
		if m.input.Value() != "" {
			m.submitMessage()
		}
		return true
	}

	return true
}

// submitMessage submits the current input
func (m *Model) submitMessage() {
	content := strings.TrimSpace(m.input.Value())
	if content == "" {
		return
	}

	// Handle slash commands
	if strings.HasPrefix(content, "/") {
		m.handleSlashCommand(content)
		return
	}

	// Add to history
	m.inputHistory = append(m.inputHistory, content)
	m.historyPos = -1

	// Add user message
	m.messages = append(m.messages, Message{
		Role:      "user",
		Content:   content,
		Timestamp: time.Now(),
	})

	// Clear input
	m.input.Reset()

	// Scroll to bottom
	m.scrollPos = len(m.messages)
	m.updateViewport()

	// Trigger response (will be handled by caller)
}

// handleSlashCommand processes slash commands
func (m *Model) handleSlashCommand(cmd string) {
	parts := strings.SplitN(cmd, " ", 2)
	command := parts[0]
	args := ""
	if len(parts) > 1 {
		args = parts[1]
	}

	switch command {
	case "/new":
		m.messages = nil
		m.session = &Session{
			ID:        fmt.Sprintf("session_%d", time.Now().Unix()),
			CreatedAt: time.Now(),
		}
		m.addSystemMessage("New conversation started")

	case "/reset":
		m.messages = nil
		m.addSystemMessage("Conversation reset")

	case "/help":
		m.showHelp()

	case "/clear":
		m.messages = nil
		m.updateViewport()

	case "/version":
		m.addSystemMessage("go-magic v1.0.0")

	case "/usage":
		m.addSystemMessage("Token usage: 0 tokens today (run 'magic usage' for details)")

	default:
		if desc, ok := m.commands[command]; ok {
			m.addSystemMessage(fmt.Sprintf("Command: %s\nUsage: %s", command, desc))
		} else {
			m.addSystemMessage(fmt.Sprintf("Unknown command: %s", command))
		}
	}

	// Clear input
	m.input.Reset()
}

// handleAutocomplete handles tab completion
func (m *Model) handleAutocomplete() {
	value := m.input.Value()
	
	// If typing a command, show completions
	if strings.HasPrefix(value, "/") {
		var matches []string
		for cmd := range m.commands {
			if strings.HasPrefix(cmd, value) {
				matches = append(matches, cmd)
			}
		}
		
		if len(matches) == 1 {
			m.input.SetValue(matches[0] + " ")
		} else if len(matches) > 1 {
			m.addSystemMessage("Commands: " + strings.Join(matches, ", "))
		}
	}
}

// historyBack moves back in input history
func (m *Model) historyBack() {
	if len(m.inputHistory) == 0 {
		return
	}

	if m.historyPos == -1 {
		m.historyPos = len(m.inputHistory) - 1
	} else if m.historyPos > 0 {
		m.historyPos--
	}

	m.input.SetValue(m.inputHistory[m.historyPos])
}

// historyForward moves forward in input history
func (m *Model) historyForward() {
	if m.historyPos == -1 {
		return
	}

	if m.historyPos < len(m.inputHistory)-1 {
		m.historyPos++
		m.input.SetValue(m.inputHistory[m.historyPos])
	} else {
		m.historyPos = -1
		m.input.Reset()
	}
}

// showHelp displays help information
func (m *Model) showHelp() {
	var buf bytes.Buffer
	buf.WriteString("Available Commands:\n\n")
	
	for cmd, desc := range m.commands {
		buf.WriteString(fmt.Sprintf("  %-15s %s\n", cmd, desc))
	}
	
	buf.WriteString("\nKeybindings:\n")
	buf.WriteString("  Tab        - Autocomplete command\n")
	buf.WriteString("  ↑↓         - Navigate history\n")
	buf.WriteString("  Ctrl+L     - Clear screen\n")
	buf.WriteString("  Ctrl+C     - Exit\n")
	
	m.addSystemMessage(buf.String())
}

// AddMessage adds a message to the session
func (m *Model) AddMessage(role, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.messages = append(m.messages, Message{
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
	})

	m.scrollPos = len(m.messages)
	m.updateViewport()
}

// AddStreamingMessage adds a streaming message
func (m *Model) AddStreamingMessage(role, content, tool string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.messages = append(m.messages, Message{
		Role:      role,
		Content:   content,
		Tool:      tool,
		Timestamp: time.Now(),
		Streaming: true,
	})

	m.updateViewport()
}

// UpdateStreamingMessage updates the last streaming message
func (m *Model) UpdateStreamingMessage(content string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.messages) > 0 && m.messages[len(m.messages)-1].Streaming {
		m.messages[len(m.messages)-1].Content = content
		m.updateViewport()
	}
}

// FinalizeStreamingMessage marks streaming as complete
func (m *Model) FinalizeStreamingMessage() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Streaming {
			m.messages[i].Streaming = false
			break
		}
	}

	m.updateViewport()
}

// addSystemMessage adds a system message
func (m *Model) addSystemMessage(content string) {
	m.messages = append(m.messages, Message{
		Role:      "system",
		Content:   content,
		Timestamp: time.Now(),
	})
	m.updateViewport()
}

// GetInput returns the current input
func (m *Model) GetInput() string {
	return m.input.Value()
}

// GetSession returns the current session
func (m *Model) GetSession() *Session {
	return m.session
}

// GetMessages returns all messages
func (m *Model) GetMessages() []Message {
	return m.messages
}

// InputView returns the input view
func (m *Model) InputView() string {
	return fmt.Sprintf("%s\n%s", PromptStyle.Render(">"), m.input.View())
}

// MainView returns the main view
func (m *Model) MainView() string {
	return m.viewport.View()
}

// FullView returns the complete view
func (m *Model) FullView() string {
	return fmt.Sprintf("%s\n%s", m.MainView(), m.InputView())
}

// RenderMarkdown renders markdown to terminal-friendly format
func RenderMarkdown(text string) string {
	// Headers
	text = regexp.MustCompile(`^### (.+)$`, 0).ReplaceAllString(text, "\033[1;34m$1\033[0m\n")
	text = regexp.MustCompile(`^## (.+)$`, 0).ReplaceAllString(text, "\033[1;36m$1\033[0m\n")
	text = regexp.MustCompile(`^# (.+)$`, 0).ReplaceAllString(text, "\033[1;33m$1\033[0m\n")

	// Bold
	text = regexp.MustCompile(`\*\*(.+?)\*\*`).ReplaceAllString(text, "\033[1m$1\033[0m")
	text = regexp.MustCompile(`__(.+?)__`).ReplaceAllString(text, "\033[1m$1\033[0m")

	// Italic
	text = regexp.MustCompile(`\*(.+?)\*`).ReplaceAllString(text, "\033[3m$1\033[0m")
	text = regexp.MustCompile(`_(.+?)_`).ReplaceAllString(text, "\033[3m$1\033[0m")

	// Code blocks
	text = regexp.MustCompile("```(\\w+)?\\n([\\s\\S]*?)```").ReplaceAllString(text, "\033[90m$2\033[0m")
	text = regexp.MustCompile("`(.+?)`").ReplaceAllString(text, "\033[92m$1\033[0m")

	// Links
	text = regexp.MustCompile(`\[(.+?)\]\((.+?)\)`).ReplaceAllString(text, "\033[94m$1\033[0m ($2)")

	// Lists
	text = regexp.MustCompile(`^- (.+)$`, 0).ReplaceAllString(text, "  • $1")
	text = regexp.MustCompile(`^\d+\. (.+)$`, 0).ReplaceAllString(text, "  $1")

	return text
}

// Colors returns ANSI color codes
var Colors = map[string]string{
	"black":   "\033[30m",
	"red":     "\033[31m",
	"green":   "\033[32m",
	"yellow":  "\033[33m",
	"blue":    "\033[34m",
	"magenta": "\033[35m",
	"cyan":    "\033[36m",
	"white":   "\033[37m",
	"reset":   "\033[0m",
}

// Colorize applies color to text
func Colorize(text, color string) string {
	if c, ok := Colors[color]; ok {
		return c + text + Colors["reset"]
	}
	return text
}

// ClearScreen clears the terminal screen
func ClearScreen() {
	if runtime.GOOS == "windows" {
		fmt.Print("\033[2J")
	} else {
		fmt.Print("\033[2J\033[H")
	}
}

// HideCursor hides the terminal cursor
func HideCursor() {
	fmt.Print("\033[?25l")
}

// ShowCursor shows the terminal cursor
func ShowCursor() {
	fmt.Print("\033[?25h")
}

// SaveCursorPosition saves cursor position
func SaveCursorPosition() {
	fmt.Print("\033[s")
}

// RestoreCursorPosition restores cursor position
func RestoreCursorPosition() {
	fmt.Print("\033[u")
}

// EnableRawMode enables raw terminal mode
func EnableRawMode() (restore func()) {
	if runtime.GOOS != "windows" {
		// Already handled by bubbletea
	}
	return func() {}
}

// HandleSignals sets up signal handling
func HandleSignals(callback func()) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		callback()
	}()
}

// InteractiveInput reads input with autocomplete support
func InteractiveInput(prompt string, completions []string) string {
	reader := bufio.NewReader(os.Stdin)
	
	fmt.Print(Colorize(prompt, "cyan"))
	input, _ := reader.ReadString('\n')
	
	return strings.TrimSpace(input)
}

// ProgressBar renders a progress bar
func ProgressBar(current, total int, width int) string {
	filled := int(float64(current) / float64(total) * float64(width))
	empty := width - filled
	
	bar := Colorize(strings.Repeat("█", filled), "green")
	bar += Colorize(strings.Repeat("░", empty), "white")
	
	return fmt.Sprintf("[%s] %d%%", bar, current*100/total)
}

// Spinner represents an animated spinner
type Spinner struct {
	frames []string
	pos    int
}

// NewSpinner creates a new spinner
func NewSpinner() *Spinner {
	return &Spinner{
		frames: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
	}
}

// Next returns the next frame
func (s *Spinner) Next() string {
	s.pos = (s.pos + 1) % len(s.frames)
	return s.frames[s.pos]
}

// Render renders the spinner with text
func (s *Spinner) Render(text string) string {
	return fmt.Sprintf("%s %s", Colorize(s.Next(), "cyan"), text)
}
