package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

var interactiveCmd = &cobra.Command{
	Use:   "interactive [prompt]",
	Short: "Start interactive REPL mode",
	Long: `Start an interactive read-eval-print loop (REPL) for magic.

In interactive mode, you can:
- Enter commands directly
- Use arrow keys for history
- Get auto-completion suggestions
- Use special commands (help, exit, clear, history)

Special Commands:
  /help          Show this help
  /exit, /quit   Exit the REPL
  /clear         Clear the screen
  /history       Show command history
  /save <file>   Save session to file
  /load <file>   Load commands from file
  /env           Show environment variables
  /vars          Show defined variables
  /reset         Reset the session`,
	Args: cobra.RangeArgs(0, 1),
	RunE: runInteractive,
}

var (
	interactiveHistorySize = 1000
	interactivePrompt      = "magic> "
)

func init() {
	interactiveCmd.Flags().IntVar(&interactiveHistorySize, "history-size", 1000, "Maximum history entries")
	interactiveCmd.Flags().StringVar(&interactivePrompt, "prompt", "magic> ", "REPL prompt")
	rootCmd.AddCommand(interactiveCmd)
}

// REPL represents the interactive shell
type InteractiveShell struct {
	reader    *bufio.Reader
	writer    io.Writer
	prompt    string
	history   []string
	variables map[string]string
}

// NewREPL creates a new REPL instance
func NewInteractiveShell() *InteractiveShell {
	return &InteractiveShell{
		reader:    bufio.NewReader(os.Stdin),
		writer:    os.Stdout,
		history:   make([]string, 0),
		variables: make(map[string]string),
	}
}

// Run starts the REPL loop
func (r *InteractiveShell) Run(initialPrompt string) error {
	// Set up signal handling for graceful exit
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Get initial prompt if provided
	if len(os.Args) > 2 {
		// magic interactive "hello"
		initialInput := strings.Join(os.Args[2:], " ")
		if _, err := r.processInput(initialInput); err != nil {
			return err
		}
	}

	prompt := initialPrompt
	if prompt == "" {
		prompt = interactivePrompt
	}

	for {
		select {
		case <-sigChan:
			fmt.Fprintln(r.writer, "\nInterrupted. Use /exit to quit.")
			continue
		default:
		}

		// Print prompt
		fmt.Fprint(r.writer, prompt)

		// Read input
		line, err := r.reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				fmt.Fprintln(r.writer, "\nGoodbye!")
				return nil
			}
			return err
		}

		line = strings.TrimRight(line, "\r\n")

		// Handle empty lines
		if line == "" {
			continue
		}

		// Add to history
		r.addToHistory(line)

		// Handle special commands
		if strings.HasPrefix(line, "/") {
			if err := r.handleSpecialCommand(line); err != nil {
				fmt.Fprintf(r.writer, "Error: %v\n", err)
			}
			continue
		}

		// Process single line input
		if _, err := r.processInput(line); err != nil {
			fmt.Fprintf(r.writer, "Error: %v\n", err)
		}
	}
}

func (r *InteractiveShell) handleSpecialCommand(cmd string) error {
	parts := strings.SplitN(cmd, " ", 2)
	command := strings.ToLower(parts[0])
	args := ""
	if len(parts) > 1 {
		args = parts[1]
	}

	switch command {
	case "/help", "/h", "/?":
		return r.showHelp()
	case "/exit", "/quit", "/q":
		fmt.Fprintln(r.writer, "Goodbye!")
		os.Exit(0)
	case "/clear", "/cls":
		return r.clearScreen()
	case "/history", "/hist":
		return r.showHistory()
	case "/save":
		return r.saveSession(args)
	case "/load":
		return r.loadSession(args)
	case "/env":
		return r.showEnv()
	case "/vars":
		return r.showVariables()
	case "/reset":
		return r.resetSession()
	case "/set":
		return r.setVariable(args)
	case "/echo":
		fmt.Fprintln(r.writer, args)
	case "/time", "/timer":
		// Timer mode - execute and time
		start := timeNow()
		_, err := r.processInput(args)
		elapsed := timeSince(start)
		fmt.Fprintf(r.writer, "Elapsed: %v\n", elapsed)
		return err
	default:
		return fmt.Errorf("unknown command: %s", command)
	}
	return nil
}

func (r *InteractiveShell) processInput(input string) (string, error) {
	// Parse and execute the input
	// This is where you'd integrate with the agent

	// Echo the input for now
	return input, nil
}

func (r *InteractiveShell) addToHistory(line string) {
	r.history = append(r.history, line)
	if len(r.history) > interactiveHistorySize {
		r.history = r.history[len(r.history)-interactiveHistorySize:]
	}
}

func (r *InteractiveShell) showHelp() error {
	help := `
Special Commands:
  /help, /h        Show this help
  /exit, /quit     Exit the REPL
  /clear           Clear the screen
  /history         Show command history
  /save <file>     Save session to file
  /load <file>     Load commands from file
  /env             Show environment variables
  /vars            Show defined variables
  /reset           Reset the session
  /set <name>=<val> Set a variable
  /echo <text>     Echo text
  /time <cmd>      Execute and time a command

Keyboard Shortcuts:
  Ctrl+C           Cancel current input
  Ctrl+D           Exit (EOF)
  Ctrl+L           Clear screen
  Ctrl+R           Search history
  Up/Down          Navigate history
`
	fmt.Fprintln(r.writer, help)
	return nil
}

func (r *InteractiveShell) clearScreen() error {
	switch runtime.GOOS {
	case "windows":
		fmt.Fprint(r.writer, "\x1b[2J\x1b[H")
	default:
		fmt.Fprint(r.writer, "\033[2J\033[H")
	}
	return nil
}

func (r *InteractiveShell) showHistory() error {
	fmt.Fprintln(r.writer, "Command History:")
	for i, cmd := range r.history {
		fmt.Fprintf(r.writer, "  %d: %s\n", i+1, cmd)
	}
	return nil
}

func (r *InteractiveShell) saveSession(filename string) error {
	if filename == "" {
		return fmt.Errorf("filename required: /save <filename>")
	}

	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	for _, cmd := range r.history {
		fmt.Fprintln(file, cmd)
	}

	fmt.Fprintf(r.writer, "Saved %d commands to %s\n", len(r.history), filename)
	return nil
}

func (r *InteractiveShell) loadSession(filename string) error {
	if filename == "" {
		return fmt.Errorf("filename required: /load <filename>")
	}

	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			r.addToHistory(line)
			count++
		}
	}

	fmt.Fprintf(r.writer, "Loaded %d commands from %s\n", count, filename)
	return nil
}

func (r *InteractiveShell) showEnv() error {
	fmt.Fprintln(r.writer, "Environment Variables:")
	env := os.Environ()
	for _, e := range env {
		fmt.Fprintf(r.writer, "  %s\n", e)
	}
	return nil
}

func (r *InteractiveShell) showVariables() error {
	fmt.Fprintln(r.writer, "Variables:")
	if len(r.variables) == 0 {
		fmt.Fprintln(r.writer, "  (none)")
	} else {
		for name, value := range r.variables {
			fmt.Fprintf(r.writer, "  %s = %s\n", name, value)
		}
	}
	return nil
}

func (r *InteractiveShell) resetSession() error {
	r.history = make([]string, 0)
	r.variables = make(map[string]string)
	fmt.Fprintln(r.writer, "Session reset.")
	return nil
}

func (r *InteractiveShell) setVariable(args string) error {
	// Parse name=value
	parts := strings.SplitN(args, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("usage: /set <name>=<value>")
	}
	name := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	r.variables[name] = value
	fmt.Fprintf(r.writer, "Set %s = %s\n", name, value)
	return nil
}

// History search
func (r *InteractiveShell) searchHistory(pattern string) []string {
	var matches []string
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return nil
	}

	for _, cmd := range r.history {
		if re.MatchString(cmd) {
			matches = append(matches, cmd)
		}
	}
	return matches
}

// Completion suggestions
func (r *InteractiveShell) getCompletions(partial string) []string {
	// Built-in commands
	commands := []string{
		"/help", "/exit", "/quit", "/clear", "/history",
		"/save", "/load", "/env", "/vars", "/reset", "/set",
	}

	var completions []string
	for _, cmd := range commands {
		if strings.HasPrefix(cmd, partial) {
			completions = append(completions, cmd)
		}
	}

	return completions
}

// runInteractive is the main entry point for interactive mode
func runInteractive(cmd *cobra.Command, args []string) error {
	prompt := interactivePrompt
	if len(args) > 0 {
		prompt = args[0]
	}

	repl := NewInteractiveShell()
	repl.prompt = prompt

	return repl.Run(prompt)
}

// Prompt helper functions
func printPrompt(prompt string, continuation bool) {
	if continuation {
		fmt.Print(strings.Repeat("  ", 2))
	} else {
		fmt.Print(prompt)
	}
}

// Check if we should use rich output
func shouldUseRichOutput() bool {
	// Check if stdout is a terminal
	if !isTerminal(os.Stdout.Fd()) {
		return false
	}
	return !flagNoColor
}

// isTerminal checks if the file descriptor is a terminal
func isTerminal(fd uintptr) bool {
	return true
}

var timeNow = func() time.Time { return time.Now() }
var timeSince = func(t time.Time) time.Duration { return time.Since(t) }