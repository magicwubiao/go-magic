package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/magicwubiao/go-magic/internal/agent"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/internal/session"
	"github.com/magicwubiao/go-magic/internal/skills"
	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/pkg/config"
)

// Color codes for terminal output
const (
	colorReset   = "\033[0m"
	colorRed     = "\033[31m"
	colorGreen   = "\033[32m"
	colorYellow  = "\033[33m"
	colorBlue    = "\033[34m"
	colorMagenta = "\033[35m"
	colorCyan    = "\033[36m"
	colorWhite   = "\033[37m"
	colorGray    = "\033[90m"
	colorBold    = "\033[1m"
)

// Platform-specific clear and cursor control
var (
	clearLine   = "\r\033[K"
	moveUp      = "\033[A"
	clearScreen = "\033[2J"
	hideCursor  = "\033[?25l"
	showCursor  = "\033[?25h"
)

func init() {
	if runtime.GOOS == "windows" {
		clearLine = "\r                    \r"
		moveUp = ""
		clearScreen = ""
		hideCursor = ""
		showCursor = ""
	}
}

// REPLState holds the state for the interactive REPL
type REPLState struct {
	// Session info
	sessionID  string
	sessionNum int
	modelName  string
	provider   string

	// Context tracking
	contextPct float64 // 0.0 - 1.0
	maxContext int
	curContext int

	// History
	historyLen int
	historyMax int

	// Token usage
	inputTokens  int
	outputTokens int

	// Settings
	streamingEnabled bool
	verboseTools     bool
}

// REPL is the main interactive REPL
type REPL struct {
	// Components
	agent    *agent.Agent
	registry *tool.Registry
	store    *session.Store
	cfg      *config.Config

	// State
	state REPLState

	// Input
	reader  *bufio.Reader
	history []string
	histIdx int

	// Control
	ctx     context.Context
	cancel  context.CancelFunc
	stopCh  chan struct{}
	mu      sync.RWMutex
	running bool
}

// NewREPL creates a new REPL instance
func NewREPL(cfg *config.Config, prov provider.Provider, registry *tool.Registry, store *session.Store) *REPL {
	ctx, cancel := context.WithCancel(context.Background())

	repl := &REPL{
		agent:    agent.NewAIAgent(prov, registry, getToolsSchema(registry), ""),
		registry: registry,
		store:    store,
		cfg:      cfg,
		reader:   bufio.NewReaderSize(os.Stdin, 4096),
		history:  []string{},
		histIdx:  -1,
		ctx:      ctx,
		cancel:   cancel,
		stopCh:   make(chan struct{}),
		running:  true,
	}

	repl.state.modelName = cfg.Model
	repl.state.provider = cfg.Provider
	repl.state.sessionID = uuid.New().String()
	repl.state.sessionNum = 1
	repl.state.streamingEnabled = true
	repl.state.verboseTools = true
	repl.state.maxContext = 200000 // 200K chars default

	repl.agent.SetSession(repl.state.sessionID)

	return repl
}

// Run starts the REPL
func (r *REPL) Run() {
	// Setup signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	// Hide cursor during REPL
	fmt.Print(hideCursor)
	defer fmt.Print(showCursor)

	// Load skills context
	if mgr, err := skills.NewManager(); err == nil {
		if skillsCtx := mgr.GetSkillsContext(); skillsCtx != "" {
			r.agent.AddSkillsContext(skillsCtx)
		}
	}

	// Restore previous session if exists
	if r.store != nil {
		if sess, err := r.store.GetLatestSession(r.ctx, "cli"); err == nil && sess != nil {
			r.agent.SetHistory(sess.Messages)
			r.state.sessionID = sess.ID
		}
	}

	// Print welcome
	r.printWelcome()

	// Main loop
	for r.running {
		select {
		case sig := <-sigCh:
			if sig == os.Interrupt {
				r.handleInterrupt()
			}
		default:
			input, err := r.readInput()
			if err != nil {
				if err.Error() == "EOF" || err.Error() == "exit" {
					r.doExit()
					break
				}
				fmt.Printf("%s%sError: %v%s\n", clearLine, colorRed, err, colorReset)
				continue
			}

			if input == "" {
				continue
			}

			// Add to history (skip if same as last)
			if len(r.history) == 0 || r.history[len(r.history)-1] != input {
				r.history = append(r.history, input)
			}
			r.histIdx = -1

			// Process input
			r.processInput(input)
		}
	}
}

// readInput reads a line of input with prompt
func (r *REPL) readInput() (string, error) {
	prompt := r.makePrompt()
	fmt.Print(prompt)

	var lines []string
	for {
		line, err := r.reader.ReadString('\n')
		if err != nil {
			return "", err
		}

		line = strings.TrimRight(line, "\r\n")

		// Empty line ends multi-line input
		if line == "" && len(lines) > 0 {
			break
		}

		// Single line
		if len(lines) == 0 && !strings.HasSuffix(line, "\\") {
			return line, nil
		}

		// Continuation
		if strings.HasSuffix(line, "\\") {
			lines = append(lines, strings.TrimSuffix(line, "\\"))
		} else {
			lines = append(lines, line)
			break
		}
	}

	return strings.Join(lines, "\n"), nil
}

// makePrompt creates the prompt string
func (r *REPL) makePrompt() string {
	// Format: [provider:model | #session | context%]
	ctx := r.getContext()
	ctxPct := 0
	if r.state.maxContext > 0 {
		ctxPct = int(float64(ctx) / float64(r.state.maxContext) * 100)
	}
	if ctxPct > 100 {
		ctxPct = 100
	}

	return fmt.Sprintf("%s[%s%s:%s%s | %s#%d%s | %s%d%%%s] %s> %s",
		colorGray, colorReset,
		colorCyan, r.state.modelName, colorGray,
		colorReset, r.state.sessionNum, colorGray,
		colorReset, ctxPct, colorGray,
		colorReset, colorGreen)
}

// getContext calculates current context usage
func (r *REPL) getContext() int {
	// Simple estimate based on history length
	history := r.agent.GetHistory()
	total := 0
	for _, msg := range history {
		total += len(msg.Role) + len(msg.Content)
	}
	return total
}

// processInput processes user input
func (r *REPL) processInput(input string) {
	// Check for slash command
	if strings.HasPrefix(input, "/") {
		r.processCommand(input)
		return
	}

	// Run conversation
	r.runConversation(input)
}

// processCommand handles slash commands
func (r *REPL) processCommand(input string) {
	cmd, args := parseSlashCommand(input)

	switch cmd {
	case "help", "h", "?":
		r.cmdHelp()
	case "exit", "quit", "q":
		r.doExit()
	case "new", "reset":
		r.cmdNew()
	case "model":
		r.cmdModel(args)
	case "compress":
		r.cmdCompress()
	case "usage":
		r.cmdUsage()
	case "tools":
		r.cmdTools()
	case "skills":
		r.cmdSkills()
	case "undo":
		r.cmdUndo()
	case "retry":
		r.cmdRetry()
	case "stop":
		r.cmdStop()
	case "save":
		r.cmdSave(args)
	case "load":
		r.cmdLoad(args)
	case "stream":
		r.cmdStream()
	case "clear", "cls":
		r.cmdClear()
	case "history":
		r.cmdHistory()
	case "insights":
		r.cmdInsights(args)
	case "personality":
		r.cmdPersonality(args)
	case "export":
		r.cmdExport(args)
	case "export-md":
		r.cmdExportMD()
	default:
		fmt.Printf("%sUnknown command: /%s (type /help for commands)%s\n", colorRed, cmd, colorReset)
	}
}

// printWelcome prints welcome banner
func (r *REPL) printWelcome() {
	fmt.Println()
	fmt.Printf("%s╔══════════════════════════════════════════════════════╗%s\n", colorCyan, colorReset)
	fmt.Printf("%s║%s  %s⚡ magic Agent CLI v%s  %s                       %s║%s\n",
		colorCyan, colorReset, colorBold, "1.0", colorReset, colorCyan, colorReset)
	fmt.Printf("%s╠══════════════════════════════════════════════════════╣%s\n", colorCyan, colorReset)
	fmt.Printf("%s║%s  Provider: %-15s  Model: %-20s %s║%s\n",
		colorCyan, colorReset, r.state.provider, r.state.modelName, colorCyan, colorReset)
	fmt.Printf("%s║%s  Streaming: %-12s  Tools: %-20s %s║%s\n",
		colorCyan, colorReset, map[bool]string{true: "ON", false: "OFF"}[r.state.streamingEnabled],
		fmt.Sprintf("%d available", len(r.registry.List())), colorCyan, colorReset)
	fmt.Printf("%s╚══════════════════════════════════════════════════════╝%s\n", colorCyan, colorReset)
	fmt.Println()
	fmt.Printf("%sType %s/help%s for commands, %s/exit%s to quit%s\n\n",
		colorGray, colorYellow, colorGray, colorYellow, colorGray, colorReset)
}

// handleInterrupt handles Ctrl+C
func (r *REPL) handleInterrupt() {
	r.mu.Lock()
	if r.running {
		if r.cancel != nil {
			r.cancel()
			r.mu.Unlock()
			fmt.Printf("\n%s[Interrupted] Press Ctrl+C again to exit%s\n", colorYellow, colorReset)
			// Create new context for future use
			r.ctx, r.cancel = context.WithCancel(context.Background())
			return
		}
	}
	r.mu.Unlock()

	// Second interrupt - exit
	r.doExit()
}

// runConversation runs a conversation turn
func (r *REPL) runConversation(input string) {
	fmt.Println() // Move to new line

	// Save state for undo
	historyBeforeUndo := r.agent.GetHistory()

	// Print thinking indicator
	doneCh := make(chan struct{})
	go func() {
		spinner := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		i := 0
		for {
			select {
			case <-doneCh:
				return
			default:
				fmt.Printf("%s%sThinking %s%s\r", clearLine, colorGray, spinner[i%len(spinner)], colorReset)
				time.Sleep(80 * time.Millisecond)
				i++
			}
		}
	}()

	var response string
	var toolCallCount int
	var err error

	// Track if we're in streaming mode
	if r.state.streamingEnabled {
		var fullContent strings.Builder
		var tc []interface{}

		err = r.agent.RunConversationStream(r.ctx, input, func(content string, done bool) {
			if done {
				response = fullContent.String()
				return
			}
			fullContent.WriteString(content)
			// Streaming output with basic ANSI rendering
			r.renderStreaming(content)
		})

		if err == nil {
			response = fullContent.String()
		}
	} else {
		response, err = r.agent.RunConversation(r.ctx, input)
	}

	close(doneCh)

	// Clear thinking line
	fmt.Printf("%s        \r", clearLine)

	if err != nil {
		// Check if cancelled
		if r.ctx.Err() == context.Canceled {
			fmt.Printf("%s[Cancelled]%s\n", colorYellow, colorReset)
			return
		}
		fmt.Printf("\n%s✗ Error: %v%s\n\n", colorRed, err, colorReset)
		return
	}

	// Update context stats
	r.state.historyLen = len(r.agent.GetHistory())
	r.state.curContext = r.getContext()

	// Save session
	r.saveSession()

	fmt.Println() // Extra newline after response
}

// renderStreaming renders streaming content with basic formatting
func (r *REPL) renderStreaming(content string) {
	// Basic rendering - just print with some handling for code blocks
	fmt.Print(content)
}

// saveSession saves the current session
func (r *REPL) saveSession() {
	if r.store == nil {
		return
	}

	sess := &session.Session{
		ID:       r.state.sessionID,
		Profile:  r.cfg.Profile,
		Platform: "cli",
		Messages: r.agent.GetHistory(),
	}

	if err := r.store.SaveSession(r.ctx, sess); err != nil {
		// Silent fail for auto-save
	}
}

// doExit handles exit
func (r *REPL) doExit() {
	r.mu.Lock()
	r.running = false
	r.mu.Unlock()

	// Save session before exit
	r.saveSession()

	fmt.Printf("\n%sGoodbye! 👋%s\n", colorCyan, colorReset)
}

// CmdHelp shows help
func (r *REPL) cmdHelp() {
	fmt.Println()
	fmt.Printf("%s╔══════════════════════════════════════════════════════════════╗%s\n", colorCyan, colorReset)
	fmt.Printf("%s║%s                    %s⚡ magic Agent Commands%s                     %s║%s\n",
		colorCyan, colorReset, colorBold, colorReset, colorCyan, colorReset)
	fmt.Printf("%s╠══════════════════════════════════════════════════════════════╣%s\n", colorCyan, colorReset)

	commands := [][]string{
		{"Navigation", ""},
		{"  /new, /reset", "Start a new conversation"},
		{"  /exit, /quit", "Exit the chat"},
		{""},
		{"Conversation", ""},
		{"  /undo", "Undo last assistant response"},
		{"  /retry", "Retry last user message"},
		{"  /stop", "Stop current generation"},
		{"  /clear", "Clear conversation history"},
		{"  /history", "Show conversation history"},
		{""},
		{"Model & Settings", ""},
		{"  /model [name]", "Show or set model"},
		{"  /stream", "Toggle streaming mode"},
		{"  /compress", "Manually compress context"},
		{""},
		{"Information", ""},
		{"  /usage", "Show token usage statistics"},
		{"  /tools", "List available tools"},
		{"  /skills", "List available skills"},
		{"  /insights [--days N]", "Show usage insights"},
		{""},
		{"Session", ""},
		{"  /save [name]", "Save current session"},
		{"  /load [name]", "Load a saved session"},
		{"  /export [format]", "Export conversation"},
	}

	for _, cmd := range commands {
		if len(cmd) == 1 {
			fmt.Printf("%s║%s%s%s\n", colorCyan, colorReset, cmd[0], colorReset)
		} else if cmd[0] != "" {
			fmt.Printf("%s║%s  %s%-20s %s%s%s\n",
				colorCyan, colorReset, colorYellow, cmd[0], colorGray, cmd[1], colorReset)
		} else {
			fmt.Printf("%s║%s                                                      %s║%s\n",
				colorCyan, colorReset, colorCyan, colorReset)
		}
	}

	fmt.Printf("%s╚══════════════════════════════════════════════════════════════╝%s\n", colorCyan, colorReset)
	fmt.Println()
	fmt.Printf("%sTips:%s\n", colorBold, colorReset)
	fmt.Printf("  %s•%s Multi-line input: end a line with %s\\%s and press Enter\n", colorGray, colorReset, colorYellow, colorReset)
	fmt.Printf("  %s•%s Commands auto-complete with %sTab%s\n", colorGray, colorReset, colorYellow, colorReset)
	fmt.Printf("  %s•%s Use %s↑↓%s to navigate command history\n", colorGray, colorReset, colorYellow, colorReset)
	fmt.Printf("  %s•%s Press %sCtrl+C%s to interrupt generation\n", colorGray, colorReset, colorYellow, colorReset)
	fmt.Println()
}

// cmdNew starts a new conversation
func (r *REPL) cmdNew() {
	r.saveSession()

	r.agent.Reset()
	r.state.sessionID = uuid.New().String()
	r.agent.SetSession(r.state.sessionID)
	r.state.sessionNum++

	// Reload skills
	if mgr, err := skills.NewManager(); err == nil {
		if skillsCtx := mgr.GetSkillsContext(); skillsCtx != "" {
			r.agent.AddSkillsContext(skillsCtx)
		}
	}

	fmt.Printf("%s✓ New conversation started (#%d)%s\n", colorGreen, r.state.sessionNum, colorReset)
}

// cmdModel handles model switching
func (r *REPL) cmdModel(args string) {
	if args == "" {
		fmt.Printf("Current model: %s%s%s\n", colorCyan, r.state.modelName, colorReset)
		return
	}

	fmt.Printf("%sModel change to '%s' requires restart%s\n", colorYellow, args, colorReset)
	fmt.Println("Restart magic to use a different model.")
}

// cmdCompress compresses context
func (r *REPL) cmdCompress() {
	r.agent.EnableCompression(true)
	r.agent.SetCompressionRatio(0.5)

	before := r.getContext()
	// Trigger compression by adding a marker that the next conversation will compress
	// Note: Actual compression happens automatically when context exceeds threshold
	after := r.getContext()

	fmt.Printf("%s✓ Compression enabled (ratio: 0.5). Context: %d chars%s\n",
		colorGreen, after, colorReset)
	fmt.Printf("%s  Compression will trigger automatically when context exceeds 50%% threshold%s\n",
		colorGray, colorReset)
}

// cmdUsage shows usage statistics
func (r *REPL) cmdUsage() {
	history := r.agent.GetHistory()
	msgCount := len(history)

	// Count tokens estimate (rough: ~4 chars per token)
	inputTokens := 0
	for _, msg := range history {
		inputTokens += len(msg.Content) / 4
	}

	fmt.Println()
	fmt.Printf("%s╔═══════════════════════════════════════╗%s\n", colorCyan, colorReset)
	fmt.Printf("%s║%s           %s📊 Usage Statistics%s            %s║%s\n", colorCyan, colorReset, colorBold, colorReset, colorCyan, colorReset)
	fmt.Printf("%s╠═══════════════════════════════════════╣%s\n", colorCyan, colorReset)
	fmt.Printf("%s║%s  Messages:      %-20d %s║%s\n", colorCyan, colorReset, msgCount, colorCyan, colorReset)
	fmt.Printf("%s║%s  Est. Tokens:  %-20d %s║%s\n", colorCyan, colorReset, inputTokens, colorCyan, colorReset)
	fmt.Printf("%s║%s  Context Used: %-20d %s║%s\n", colorCyan, colorReset, r.getContext(), colorCyan, colorReset)
	fmt.Printf("%s║%s  Session:      %-20d %s║%s\n", colorCyan, colorReset, r.state.sessionNum, colorCyan, colorReset)
	fmt.Printf("%s╚═══════════════════════════════════════╝%s\n", colorCyan, colorReset)
	fmt.Println()
}

// cmdTools lists available tools
func (r *REPL) cmdTools() {
	tools := r.registry.List()

	fmt.Println()
	fmt.Printf("%s╔═══════════════════════════════════════════════════════════╗%s\n", colorCyan, colorReset)
	fmt.Printf("%s║%s                      %s🔧 Available Tools%s                      %s║%s\n",
		colorCyan, colorReset, colorBold, colorReset, colorCyan, colorReset)
	fmt.Printf("%s╠═══════════════════════════════════════════════════════════╣%s\n", colorCyan, colorReset)

	for i, tName := range tools {
		t, err := r.registry.Get(tName)
		if err != nil {
			continue
		}

		// Truncate description if too long
		desc := t.Description()
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}

		fmt.Printf("%s║%s  %s%-20s %s%-45s %s║%s\n",
			colorCyan, colorReset, colorYellow, t.Name(), colorGray, desc, colorCyan, colorReset)
	}

	fmt.Printf("%s║%s  %s(%d tools total)%s                                         %s║%s\n",
		colorCyan, colorReset, colorGray, len(tools), colorCyan, colorReset)
	fmt.Printf("%s╚═══════════════════════════════════════════════════════════╝%s\n", colorCyan, colorReset)
	fmt.Println()
}

// cmdSkills lists available skills
func (r *REPL) cmdSkills() {
	fmt.Println()
	fmt.Printf("%s╔═══════════════════════════════════════════════════════════╗%s\n", colorCyan, colorReset)
	fmt.Printf("%s║%s                      %s🎯 Available Skills%s                    %s║%s\n",
		colorCyan, colorReset, colorBold, colorReset, colorCyan, colorReset)
	fmt.Printf("%s╠═══════════════════════════════════════════════════════════╣%s\n", colorCyan, colorReset)

	if mgr, err := skills.NewManager(); err == nil {
		skillList := mgr.ListSkills()
		if len(skillList) == 0 {
			fmt.Printf("%s║%s  %sNo skills installed.%s                                         %s║%s\n",
				colorCyan, colorReset, colorGray, colorCyan, colorReset)
		} else {
			for _, skill := range skillList {
				desc := skill.Description
				if len(desc) > 45 {
					desc = desc[:42] + "..."
				}
				fmt.Printf("%s║%s  %s%-15s %s%-48s %s║%s\n",
					colorCyan, colorReset, colorYellow, skill.Name, colorGray, desc, colorCyan, colorReset)
			}
		}
	} else {
		fmt.Printf("%s║%s  %sSkills manager not available.%s                           %s║%s\n",
			colorCyan, colorReset, colorGray, colorCyan, colorReset)
	}

	fmt.Printf("%s╚═══════════════════════════════════════════════════════════╝%s\n", colorCyan, colorReset)
	fmt.Println()
}

// cmdUndo undoes last response
func (r *REPL) cmdUndo() {
	history := r.agent.GetHistory()
	if len(history) < 2 {
		fmt.Printf("%sNothing to undo.%s\n", colorYellow, colorReset)
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
		fmt.Printf("%sNothing to undo.%s\n", colorYellow, colorReset)
		return
	}

	r.agent.SetHistory(history[:userIdx])
	fmt.Printf("%s✓ Undo successful. Last response removed.%s\n", colorGreen, colorReset)
}

// cmdRetry retries last message
func (r *REPL) cmdRetry() {
	history := r.agent.GetHistory()
	if len(history) < 2 {
		fmt.Printf("%sNo message to retry.%s\n", colorYellow, colorReset)
		return
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
		fmt.Printf("%sNo message to retry.%s\n", colorYellow, colorReset)
		return
	}

	lastUserMsg := history[userIdx].Content

	// Remove assistant response
	r.agent.SetHistory(history[:userIdx+1])

	fmt.Printf("%sRetrying: \"%s\"%s\n", colorYellow, truncateStr(lastUserMsg, 50), colorReset)
	r.runConversation(lastUserMsg)
}

// cmdStop stops current generation
func (r *REPL) cmdStop() {
	r.mu.Lock()
	if r.cancel != nil {
		r.cancel()
		// Create new context
		r.ctx, r.cancel = context.WithCancel(context.Background())
	}
	r.mu.Unlock()
	fmt.Printf("%s✓ Generation stopped%s\n", colorGreen, colorReset)
}

// cmdSave saves session
func (r *REPL) cmdSave(name string) {
	saveName := name
	if saveName == "" {
		saveName = fmt.Sprintf("session_%d", time.Now().Unix())
	}

	sess := &session.Session{
		ID:       r.state.sessionID,
		Profile:  r.cfg.Profile,
		Platform: "cli",
		Messages: r.agent.GetHistory(),
	}

	if err := r.store.SaveSession(r.ctx, sess); err != nil {
		fmt.Printf("%s✗ Failed to save session: %v%s\n", colorRed, err, colorReset)
		return
	}

	fmt.Printf("%s✓ Session saved (%s) - %d messages%s\n", colorGreen, r.state.sessionID[:8], len(sess.Messages), colorReset)
}

// cmdLoad loads session
func (r *REPL) cmdLoad(id string) {
	sessions, err := r.store.ListSessions(r.ctx, r.cfg.Profile)
	if err != nil {
		fmt.Printf("%s✗ Failed to load sessions: %v%s\n", colorRed, err, colorReset)
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
			fmt.Printf("%sSession '%s' not found%s\n", colorRed, id, colorReset)
			return
		}
	} else {
		// Load most recent
		for _, s := range sessions {
			sess = s
			break
		}
	}

	if sess == nil {
		fmt.Printf("%sNo sessions found%s\n", colorYellow, colorReset)
		return
	}

	r.agent.SetHistory(sess.Messages)
	r.state.sessionID = sess.ID
	r.state.sessionNum++

	fmt.Printf("%s✓ Loaded session %s (%d messages)%s\n", colorGreen, sess.ID[:8], len(sess.Messages), colorReset)
}

// cmdStream toggles streaming
func (r *REPL) cmdStream() {
	r.state.streamingEnabled = !r.state.streamingEnabled
	status := map[bool]string{true: "enabled", false: "disabled"}
	fmt.Printf("%s✓ Streaming %s%s\n", colorGreen, status[r.state.streamingEnabled], colorReset)
}

// cmdClear clears conversation
func (r *REPL) cmdClear() {
	r.agent.Reset()

	// Reload skills
	if mgr, err := skills.NewManager(); err == nil {
		if skillsCtx := mgr.GetSkillsContext(); skillsCtx != "" {
			r.agent.AddSkillsContext(skillsCtx)
		}
	}

	fmt.Printf("%s✓ Conversation cleared%s\n", colorGreen, colorReset)
}

// cmdHistory shows history
func (r *REPL) cmdHistory() {
	history := r.agent.GetHistory()

	fmt.Println()
	fmt.Printf("%s╔═══════════════════════════════════════════════════════════╗%s\n", colorCyan, colorReset)
	fmt.Printf("%s║%s                     %s📜 History (%d messages)%s                     %s║%s\n",
		colorCyan, colorReset, colorBold, len(history), colorReset, colorCyan, colorReset)
	fmt.Printf("%s╠═══════════════════════════════════════════════════════════╣%s\n", colorCyan, colorReset)

	for i, msg := range history {
		role := msg.Role
		var roleColor string
		switch role {
		case "user":
			roleColor = colorGreen
		case "assistant":
			roleColor = colorCyan
		case "system":
			roleColor = colorYellow
		case "tool":
			roleColor = colorMagenta
		default:
			roleColor = colorGray
		}

		content := msg.Content
		if len(content) > 60 {
			content = content[:57] + "..."
		}
		content = strings.ReplaceAll(content, "\n", " ")

		fmt.Printf("%s║%s  [%2d] %s%-10s%s: %-50s %s║%s\n",
			colorCyan, colorReset, i, roleColor, role, colorReset, content, colorCyan, colorReset)
	}

	fmt.Printf("%s╚═══════════════════════════════════════════════════════════╝%s\n", colorCyan, colorReset)
	fmt.Println()
}

// cmdInsights shows insights
func (r *REPL) cmdInsights(args string) {
	// Parse --days argument
	days := 7
	if strings.Contains(args, "--days") {
		re := regexp.MustCompile(`--days\s+(\d+)`)
		matches := re.FindStringSubmatch(args)
		if len(matches) > 1 {
			fmt.Sscanf(matches[1], "%d", &days)
		}
	}

	// Get stats from store - calculate from current session
	history := r.agent.GetHistory()
	msgCount := len(history)

	// Get total sessions count
	sessionCount := 1 // Current session
	sessions, _ := r.store.ListSessions(r.ctx, r.cfg.Profile)
	if sessions != nil {
		sessionCount = len(sessions) + 1
	}

	avgMessages := msgCount
	if sessionCount > 0 {
		avgMessages = msgCount / sessionCount
	}

	fmt.Println()
	fmt.Printf("%s╔═══════════════════════════════════════════════════════════╗%s\n", colorCyan, colorReset)
	fmt.Printf("%s║%s                   %s📈 Insights (last %d days)%s                    %s║%s\n",
		colorCyan, colorReset, colorBold, days, colorReset, colorCyan, colorReset)
	fmt.Printf("%s╠═══════════════════════════════════════════════════════════╣%s\n", colorCyan, colorReset)
	fmt.Printf("%s║%s  Total Sessions:  %-20d %s║%s\n", colorCyan, colorReset, sessionCount, colorCyan, colorReset)
	fmt.Printf("%s║%s  Current Session Msgs: %-15d %s║%s\n", colorCyan, colorReset, msgCount, colorCyan, colorReset)
	fmt.Printf("%s║%s  Avg Messages/Session: %-15d %s║%s\n", colorCyan, colorReset, avgMessages, colorCyan, colorReset)
	fmt.Printf("%s╚═══════════════════════════════════════════════════════════╝%s\n", colorCyan, colorReset)
	fmt.Println()
}

// cmdPersonality shows/switches personality
func (r *REPL) cmdPersonality(args string) {
	if args == "" {
		fmt.Printf("Available personalities: default, creative, technical, concise\n")
		return
	}
	fmt.Printf("%sPersonality switching requires restart%s\n", colorYellow, colorReset)
}

// cmdExport exports conversation
func (r *REPL) cmdExport(args string) {
	format := strings.TrimSpace(args)
	if format == "" {
		format = "text"
	}

	history := r.agent.GetHistory()
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
			output.WriteString(fmt.Sprintf(`{"role":"%s","content":"%s"}`, msg.Role, escapeJSON(msg.Content)))
		}
		output.WriteString("]")
	default:
		fmt.Printf("%sUnknown export format: %s%s\n", colorRed, format, colorReset)
		return
	}

	// Write to file
	filename := fmt.Sprintf("export_%s_%d.%s", format, time.Now().Unix(), format)
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".magic", "exports", filename)

	os.MkdirAll(filepath.Dir(path), 0755)
	if err := os.WriteFile(path, []byte(output.String()), 0644); err != nil {
		fmt.Printf("%s✗ Failed to export: %v%s\n", colorRed, err, colorReset)
		return
	}

	fmt.Printf("%s✓ Exported to %s%s\n", colorGreen, path, colorReset)
}

// cmdExportMD exports as markdown
func (r *REPL) cmdExportMD() {
	history := r.agent.GetHistory()
	var output strings.Builder

	output.WriteString("# Conversation Export\n\n")
	output.WriteString(fmt.Sprintf("**Session:** %s  \n", r.state.sessionID))
	output.WriteString(fmt.Sprintf("**Date:** %s  \n\n", time.Now().Format(time.RFC822)))

	for _, msg := range history {
		role := msg.Role
		if role == "system" {
			role = "System"
		} else if role == "assistant" {
			role = "Assistant"
		} else if role == "tool" {
			role = "Tool"
		}

		output.WriteString(fmt.Sprintf("## %s\n\n", role))
		output.WriteString(msg.Content)
		output.WriteString("\n\n---\n\n")
	}

	filename := fmt.Sprintf("conversation_%d.md", time.Now().Unix())
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".magic", "exports", filename)

	os.MkdirAll(filepath.Dir(path), 0755)
	if err := os.WriteFile(path, []byte(output.String()), 0644); err != nil {
		fmt.Printf("%s✗ Failed to export: %v%s\n", colorRed, err, colorReset)
		return
	}

	fmt.Printf("%s✓ Exported to %s%s\n", colorGreen, path, colorReset)
}

// parseSlashCommand parses a slash command
func parseSlashCommand(input string) (string, string) {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "/") {
		return "", ""
	}

	input = input[1:] // Remove leading /
	parts := strings.SplitN(input, " ", 2)

	cmdName := strings.ToLower(parts[0])
	var cmdArgs string
	if len(parts) > 1 {
		cmdArgs = strings.TrimSpace(parts[1])
	}

	return cmdName, cmdArgs
}

// truncateStr truncates a string
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// escapeJSON escapes a string for JSON
func escapeJSON(s string) string {
	var result strings.Builder
	for _, r := range s {
		switch r {
		case '"':
			result.WriteString(`\"`)
		case '\\':
			result.WriteString(`\\`)
		case '\n':
			result.WriteString(`\n`)
		case '\r':
			result.WriteString(`\r`)
		case '\t':
			result.WriteString(`\t`)
		default:
			if r < ' ' || r > 127 {
				result.WriteString(fmt.Sprintf(`\u%04x`, r))
			} else {
				result.WriteRune(r)
			}
		}
	}
	return result.String()
}

// completeCommand provides tab completion for commands
func completeCommand(partial string) string {
	commands := []string{
		"help", "exit", "quit", "new", "reset", "model", "compress",
		"usage", "tools", "skills", "undo", "retry", "stop", "save",
		"load", "stream", "clear", "history", "insights", "personality",
		"export", "export-md",
	}

	partial = strings.TrimPrefix(partial, "/")
	var matches []string
	for _, cmd := range commands {
		if strings.HasPrefix(cmd, partial) {
			matches = append(matches, cmd)
		}
	}

	if len(matches) == 1 {
		return "/" + matches[0]
	}
	if len(matches) > 1 {
		// Return longest common prefix
		prefix := matches[0]
		for _, m := range matches[1:] {
			for !strings.HasPrefix(m, prefix) {
				prefix = prefix[:len(prefix)-1]
			}
		}
		return "/" + prefix
	}

	return ""
}

// isValidCommand checks if input is a valid slash command
func isValidCommand(input string) bool {
	if !strings.HasPrefix(input, "/") {
		return false
	}

	cmd, _ := parseSlashCommand(input)
	validCmds := map[string]bool{
		"help": true, "exit": true, "quit": true, "q": true,
		"new": true, "reset": true, "model": true, "compress": true,
		"usage": true, "tools": true, "skills": true, "undo": true,
		"retry": true, "stop": true, "save": true, "load": true,
		"stream": true, "clear": true, "cls": true, "history": true,
		"insights": true, "personality": true, "export": true,
		"export-md": true, "h": true, "?": true,
	}

	return validCmds[cmd]
}

// Tab completion helper
type Completer struct {
	commands []string
	history  []string
}

func NewCompleter() *Completer {
	return &Completer{
		commands: []string{
			"help", "exit", "quit", "new", "reset", "model", "compress",
			"usage", "tools", "skills", "undo", "retry", "stop", "save",
			"load", "stream", "clear", "history", "insights", "personality",
			"export",
		},
	}
}

// Complete returns completions for the given input
func (c *Completer) Complete(input string) []string {
	var results []string

	// Command completion
	if strings.HasPrefix(input, "/") {
		partial := strings.TrimPrefix(input, "/")
		for _, cmd := range c.commands {
			if strings.HasPrefix(cmd, partial) {
				results = append(results, "/"+cmd)
			}
		}
	}

	// History completion (for non-slash commands)
	if !strings.HasPrefix(input, "/") && len(input) > 0 {
		for _, h := range c.history {
			if strings.HasPrefix(h, input) {
				results = append(results, h)
			}
		}
	}

	return results
}

// sanitizeInput removes non-printable characters
func sanitizeInput(input string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) || r == '\n' || r == '\t' {
			return r
		}
		return -1
	}, input)
}
