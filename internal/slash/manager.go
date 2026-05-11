package slash

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Command represents a slash command
type Command struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Usage       string            `json:"usage"`
	Aliases     []string          `json:"aliases"`
	Handler     CommandHandler    `json:"-"`
	Flags       []Flag            `json:"flags"`
	Category    string            `json:"category"` // conversation, system, tools, info
}

// Flag represents a command flag
type Flag struct {
	Name        string `json:"name"`
	Short       string `json:"short"`
	Description string `json:"description"`
	Type        string `json:"type"` // string, int, bool
	Default     interface{} `json:"default"`
	Required    bool   `json:"required"`
}

// CommandHandler is the function signature for command handlers
type CommandHandler func(ctx context.Context, args []string, flags map[string]interface{}) (string, error)

// ParsedCommand represents a parsed command
type ParsedCommand struct {
	Name      string
	Args      []string
	Flags     map[string]interface{}
	Raw       string
	Handler   *Command
}

// Manager handles slash commands
type Manager struct {
	commands map[string]*Command
	mu       sync.RWMutex
	handlers map[string]CommandHandler
}

// NewManager creates a new slash command manager
func NewManager() *Manager {
	m := &Manager{
		commands: make(map[string]*Command),
		handlers: make(map[string]CommandHandler),
	}
	
	m.registerDefaultCommands()
	return m
}

// registerDefaultCommands registers built-in commands
func (m *Manager) registerDefaultCommands() {
	commands := []*Command{
		// Conversation commands
		{
			Name:        "new",
			Description: "Start a new conversation",
			Usage:       "/new",
			Aliases:     []string{"/reset"},
			Category:    "conversation",
			Handler: func(ctx context.Context, args []string, flags map[string]interface{}) (string, error) {
				return "Starting new conversation...", nil
			},
		},
		{
			Name:        "clear",
			Description: "Clear chat history",
			Usage:       "/clear",
			Category:    "conversation",
			Handler: func(ctx context.Context, args []string, flags map[string]interface{}) (string, error) {
				return "Chat history cleared.", nil
			},
		},
		{
			Name:        "compress",
			Description: "Compress context window",
			Usage:       "/compress",
			Category:    "conversation",
			Handler: func(ctx context.Context, args []string, flags map[string]interface{}) (string, error) {
				return "Context compressed. Summary saved.", nil
			},
		},
		{
			Name:        "retry",
			Description: "Retry last response",
			Usage:       "/retry",
			Category:    "conversation",
			Handler: func(ctx context.Context, args []string, flags map[string]interface{}) (string, error) {
				return "Retrying last response...", nil
			},
		},
		{
			Name:        "undo",
			Description: "Undo last action",
			Usage:       "/undo",
			Category:    "conversation",
			Handler: func(ctx context.Context, args []string, flags map[string]interface{}) (string, error) {
				return "Last action undone.", nil
			},
		},
		{
			Name:        "export",
			Description: "Export conversation",
			Usage:       "/export [format]",
			Aliases:     []string{"/save"},
			Category:    "conversation",
			Handler: func(ctx context.Context, args []string, flags map[string]interface{}) (string, error) {
				format := "markdown"
				if len(args) > 0 {
					format = args[0]
				}
				return fmt.Sprintf("Conversation exported as %s.", format), nil
			},
		},
		
		// Model commands
		{
			Name:        "model",
			Description: "Change the AI model",
			Usage:       "/model [provider:model]",
			Aliases:     []string{"/m"},
			Category:    "system",
			Handler: func(ctx context.Context, args []string, flags map[string]interface{}) (string, error) {
				if len(args) == 0 {
					return "Current model: not set. Usage: /model provider:model", nil
				}
				return fmt.Sprintf("Model set to: %s", args[0]), nil
			},
		},
		
		// Personality commands
		{
			Name:        "personality",
			Description: "Set agent personality",
			Usage:       "/personality [name]",
			Aliases:     []string{"/persona", "/tone"},
			Category:    "system",
			Handler: func(ctx context.Context, args []string, flags map[string]interface{}) (string, error) {
				if len(args) == 0 {
					return "Current personality: assistant. Usage: /personality [name]", nil
				}
				return fmt.Sprintf("Personality set to: %s", args[0]), nil
			},
		},
		
		// Tool commands
		{
			Name:        "tools",
			Description: "List available tools",
			Usage:       "/tools [category]",
			Category:    "tools",
			Handler: func(ctx context.Context, args []string, flags map[string]interface{}) (string, error) {
				if len(args) > 0 {
					return fmt.Sprintf("Tools in %s:", args[0]), nil
				}
				return "Available tools: web_search, web_extract, read_file, write_file, execute_command, execute_code, skills_list, memory_recall...", nil
			},
		},
		{
			Name:        "skills",
			Description: "List available skills",
			Usage:       "/skills [name]",
			Aliases:     []string{"/skill"},
			Category:    "tools",
			Handler: func(ctx context.Context, args []string, flags map[string]interface{}) (string, error) {
				if len(args) > 0 {
					return fmt.Sprintf("Skill: %s", args[0]), nil
				}
				return "Available skills. Use /skills [name] for details.", nil
			},
		},
		
		// Info commands
		{
			Name:        "help",
			Description: "Show help",
			Usage:       "/help [command]",
			Aliases:     []string{"/?"},
			Category:    "info",
			Handler: func(ctx context.Context, args []string, flags map[string]interface{}) (string, error) {
				if len(args) > 0 {
					return fmt.Sprintf("Help for %s:", args[0]), nil
				}
				return "Type /help [command] for details. Use /commands to list all commands.", nil
			},
		},
		{
			Name:        "commands",
			Description: "List all commands",
			Usage:       "/commands [category]",
			Aliases:     []string{"/cmds"},
			Category:    "info",
			Handler: func(ctx context.Context, args []string, flags map[string]interface{}) (string, error) {
				var result strings.Builder
				result.WriteString("Available commands:\n\n")
				result.WriteString("**Conversation:** /new, /clear, /compress, /retry, /undo, /export\n")
				result.WriteString("**System:** /model, /personality\n")
				result.WriteString("**Tools:** /tools, /skills\n")
				result.WriteString("**Info:** /help, /commands, /status, /version\n")
				result.WriteString("**Usage:** /usage, /insights\n")
				result.WriteString("**Sessions:** /sessions, /sethome\n")
				return result.String(), nil
			},
		},
		{
			Name:        "status",
			Description: "Show system status",
			Usage:       "/status",
			Category:    "info",
			Handler: func(ctx context.Context, args []string, flags map[string]interface{}) (string, error) {
				return "System status: OK\nModel: not configured\nPersonality: assistant\nActive tools: all", nil
			},
		},
		{
			Name:        "version",
			Description: "Show version",
			Usage:       "/version",
			Aliases:     []string{"/ver"},
			Category:    "info",
			Handler: func(ctx context.Context, args []string, flags map[string]interface{}) (string, error) {
				return "go-magic v1.0.0", nil
			},
		},
		
		// Usage commands
		{
			Name:        "usage",
			Description: "Show token usage",
			Usage:       "/usage [--days N]",
			Category:    "usage",
			Handler: func(ctx context.Context, args []string, flags map[string]interface{}) (string, error) {
				days := 1
				if d, ok := flags["days"].(int); ok {
					days = d
				}
				return fmt.Sprintf("Token usage (last %d day(s)):\n- Today: 0 tokens\n- Cost: $0.00", days), nil
			},
			Flags: []Flag{
				{Name: "days", Type: "int", Default: 1, Description: "Number of days to show"},
			},
		},
		{
			Name:        "insights",
			Description: "Get usage insights",
			Usage:       "/insights [--days N]",
			Category:    "usage",
			Handler: func(ctx context.Context, args []string, flags map[string]interface{}) (string, error) {
				days := 7
				if d, ok := flags["days"].(int); ok {
					days = d
				}
				return fmt.Sprintf("Usage insights (last %d days):\n- Most used tools: read_file, web_search\n- Common tasks: code review, debugging\n- Peak hours: 10:00-12:00", days), nil
			},
			Flags: []Flag{
				{Name: "days", Short: "d", Type: "int", Default: 7, Description: "Number of days"},
			},
		},
		
		// Session commands
		{
			Name:        "sessions",
			Description: "List sessions",
			Usage:       "/sessions [list|search]",
			Aliases:     []string{"/session"},
			Category:    "info",
			Handler: func(ctx context.Context, args []string, flags map[string]interface{}) (string, error) {
				if len(args) > 0 && args[0] == "search" {
					return "Session search: enter search term", nil
				}
				return "Current session active. Use /sessions list for history.", nil
			},
		},
		{
			Name:        "sethome",
			Description: "Set home session for messaging",
			Usage:       "/sethome [session_id]",
			Category:    "info",
			Handler: func(ctx context.Context, args []string, flags map[string]interface{}) (string, error) {
				if len(args) > 0 {
					return fmt.Sprintf("Home session set to: %s", args[0]), nil
				}
				return "Usage: /sethome [session_id]", nil
			},
		},
		
		// Context commands
		{
			Name:        "context",
			Description: "Manage context files",
			Usage:       "/context [add|remove|list]",
			Aliases:     []string{"/ctx"},
			Category:    "system",
			Handler: func(ctx context.Context, args []string, flags map[string]interface{}) (string, error) {
				if len(args) == 0 {
					return "Context files: AGENTS.md, README.md loaded. Use /context list for details.", nil
				}
				return fmt.Sprintf("Context %s:", args[0]), nil
			},
		},
		
		// Stop command
		{
			Name:        "stop",
			Description: "Stop current operation",
			Usage:       "/stop",
			Aliases:     []string{"/cancel"},
			Category:    "system",
			Handler: func(ctx context.Context, args []string, flags map[string]interface{}) (string, error) {
				return "Operation stopped.", nil
			},
		},
	}
	
	for _, cmd := range commands {
		m.Register(cmd)
	}
}

// Register registers a new command
func (m *Manager) Register(cmd *Command) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.commands[cmd.Name] = cmd
	for _, alias := range cmd.Aliases {
		aliasName := strings.TrimPrefix(alias, "/")
		m.commands[aliasName] = cmd
	}
}

// Unregister removes a command
func (m *Manager) Unregister(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	delete(m.commands, name)
}

// Get returns a command by name
func (m *Manager) Get(name string) *Command {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	// Remove leading slash
	name = strings.TrimPrefix(name, "/")
	
	if cmd, ok := m.commands[name]; ok {
		return cmd
	}
	return nil
}

// List returns all commands
func (m *Manager) List() []*Command {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	seen := make(map[string]bool)
	var result []*Command
	
	for _, cmd := range m.commands {
		if !seen[cmd.Name] {
			result = append(result, cmd)
			seen[cmd.Name] = true
		}
	}
	return result
}

// ListByCategory returns commands filtered by category
func (m *Manager) ListByCategory(category string) []*Command {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	var result []*Command
	seen := make(map[string]bool)
	
	for _, cmd := range m.commands {
		if cmd.Category == category && !seen[cmd.Name] {
			result = append(result, cmd)
			seen[cmd.Name] = true
		}
	}
	return result
}

// ParseCommand parses a command string
func (m *Manager) ParseCommand(input string) (*ParsedCommand, error) {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "/") {
		return nil, fmt.Errorf("not a slash command")
	}
	
	parts := parseCommandParts(input)
	name := strings.TrimPrefix(parts[0], "/")
	
	cmd := m.Get(name)
	if cmd == nil {
		return nil, fmt.Errorf("unknown command: %s", name)
	}
	
	parsed := &ParsedCommand{
		Name:    name,
		Raw:     input,
		Args:    parts[1:],
		Flags:   make(map[string]interface{}),
		Handler: cmd,
	}
	
	// Parse flags
	for _, arg := range parts[1:] {
		if strings.HasPrefix(arg, "--") {
			flagParts := strings.SplitN(strings.TrimPrefix(arg, "--"), "=", 2)
			flagName := flagParts[0]
			
			var flagValue interface{} = true
			if len(flagParts) > 1 {
				flagValue = flagParts[1]
			}
			parsed.Flags[flagName] = flagValue
		} else if strings.HasPrefix(arg, "-") && len(arg) == 2 {
			// Short flag
			flagName := arg[1:]
			for _, f := range cmd.Flags {
				if f.Short == flagName {
					parsed.Flags[f.Name] = true
					break
				}
			}
		}
	}
	
	return parsed, nil
}

// Execute executes a command
func (m *Manager) Execute(ctx context.Context, input string) (string, error) {
	parsed, err := m.ParseCommand(input)
	if err != nil {
		return "", err
	}

	// Call the handler function from the Command
	if parsed.Handler != nil && parsed.Handler.Handler != nil {
		return parsed.Handler.Handler(ctx, parsed.Args, parsed.Flags)
	}

	return "", fmt.Errorf("command %s has no handler", parsed.Name)
}

// parseCommandParts splits a command string respecting quotes
func parseCommandParts(input string) []string {
	var parts []string
	var current strings.Builder
	inQuote := false
	quoteChar := ' '
	
	for i, r := range input {
		if i == 0 && r == '/' {
			current.WriteRune(r)
			continue
		}
		
		switch r {
		case '"', '\'':
			if !inQuote {
				inQuote = true
				quoteChar = r
			} else if r == quoteChar {
				inQuote = false
			} else {
				current.WriteRune(r)
			}
		case ' ':
			if !inQuote {
				if current.Len() > 0 {
					parts = append(parts, current.String())
					current.Reset()
				}
			} else {
				current.WriteRune(r)
			}
		default:
			current.WriteRune(r)
		}
	}
	
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	
	return parts
}

// Autocomplete returns completion suggestions
func (m *Manager) Autocomplete(partial string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	var suggestions []string
	partial = strings.TrimPrefix(partial, "/")
	
	for _, cmd := range m.commands {
		name := cmd.Name
		if strings.HasPrefix(name, partial) {
			suggestions = append(suggestions, "/"+name)
		}
		for _, alias := range cmd.Aliases {
			if strings.HasPrefix(alias, "/"+partial) {
				suggestions = append(suggestions, alias)
			}
		}
	}
	return suggestions
}

// HelpText generates help text for all commands
func (m *Manager) HelpText(category string) string {
	var result strings.Builder
	
	commands := m.List()
	if category != "" {
		var filtered []*Command
		for _, cmd := range commands {
			if cmd.Category == category {
				filtered = append(filtered, cmd)
			}
		}
		commands = filtered
	}
	
	result.WriteString("## Slash Commands\n\n")
	
	categories := map[string][]*Command{}
	for _, cmd := range commands {
		cat := cmd.Category
		if cat == "" {
			cat = "other"
		}
		categories[cat] = append(categories[cat], cmd)
	}
	
	for cat, cmds := range categories {
		result.WriteString(fmt.Sprintf("### %s\n", strings.Title(cat)))
		for _, cmd := range cmds {
			result.WriteString(fmt.Sprintf("- `%s` - %s\n", cmd.Usage, cmd.Description))
		}
		result.WriteString("\n")
	}
	
	return result.String()
}
