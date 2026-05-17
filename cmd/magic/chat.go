package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/internal/session"
	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/pkg/config"
)

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Start interactive chat with Magic Agent",
	Long:  "Start an interactive TUI chat session with Magic Agent.\nFeatures: streaming output, slash commands, skills loading, session persistence.\nType /help in the chat to see available commands.",
	RunE:  runChat,
}

func init() {
	rootCmd.AddCommand(chatCmd)
	chatCmd.Flags().BoolP("stream", "s", true, "Enable streaming output")
	chatCmd.Flags().BoolP("no-stream", "n", false, "Disable streaming output")
}

func runChat(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err == config.ErrNoConfig {
		fmt.Println("Welcome to magic! It looks like this is your first run.")
		fmt.Println("Let's set things up...")
		runSetup(nil, nil)
		cfg, err = config.Load()
		if err != nil {
			return fmt.Errorf("failed to load config after setup: %v", err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}

	ctx := context.Background()

	// Use unified provider creation
	prov, err := config.CreateProvider(cfg)
	if err != nil {
		return fmt.Errorf("failed to create provider: %v", err)
	}

	// Initialize tool registry with auto-registration
	registry := tool.NewRegistry()
	registry.RegisterAll(cfg.WorkingDir)

	// Initialize session store
	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, ".magic", "sessions.db")
	os.MkdirAll(filepath.Dir(dbPath), 0755)

	store, err := session.NewStore(dbPath)
	if err != nil {
		fmt.Printf("Warning: Failed to open session store: %v\n", err)
	}

	if store != nil {
		defer store.Close()
	}

	// Pass streaming flags to TUI
	noStream, _ := cmd.Flags().GetBool("no-stream")
	enableStream, _ := cmd.Flags().GetBool("stream")

	return RunTUIWithOptions(ctx, cfg, prov, registry, store, enableStream && !noStream)
}

// parseSlashCommand parses a slash command into name and arguments
func parseSlashCommand(input string) (string, string) {
	input = strings.TrimSpace(input)
	if len(input) > 0 && input[0] == '/' {
		input = input[1:]
	} else {
		return "", ""
	}

	parts := strings.SplitN(input, " ", 2)

	cmdName := strings.ToLower(parts[0])
	var cmdArgs string
	if len(parts) > 1 {
		cmdArgs = strings.TrimSpace(parts[1])
	}

	return cmdName, cmdArgs
}

// getToolsSchema generates a tools schema from the registry
func getToolsSchema(registry *tool.Registry) []map[string]interface{} {
	tools := []map[string]interface{}{}
	for _, tName := range registry.List() {
		t, err := registry.Get(tName)
		if err != nil {
			continue
		}
		name := t.Name()
		if name == "" {
			fmt.Fprintf(os.Stderr, "[WARN] Tool with empty name found, skipping\n")
			continue
		}
		desc := t.Description()
		if desc == "" {
			desc = name + " tool"
		}
		tools = append(tools, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        name,
				"description": desc,
				"parameters":  t.Schema(),
			},
		})
	}
	return tools
}

// buildSystemPrompt builds the system prompt for the agent
func buildSystemPrompt(cfg *config.Config, codingMode bool) string {
	if codingMode {
		return buildCodingSystemPrompt(cfg)
	}

	prompt := `You are Magic, a powerful AI coding assistant.

CORE PRINCIPLES:
- You are an expert programmer with deep knowledge of all programming languages, frameworks, and tools
- Always respond in the user's language
- Be concise and actionable - provide working code, not just explanations
- When writing code, follow best practices and include error handling

TOOL USAGE RULES:
- Small talk/greetings (hello/hi) -> Respond directly, no tool calls
- Knowledge Q&A -> Respond directly
- List/view/read files -> Call list_files or read_file
- Create/write/edit files -> Call write_file or edit_file
- Web search -> Call web_search
- Execute commands/code -> Call execute_command
- Do NOT call time, system, math, memory_recall, todo, session_search unless explicitly requested

CODING WORKFLOW:
1. Understand the requirements before writing code
2. Plan the approach for complex tasks
3. Write clean, well-documented code
4. Verify the solution works
5. Explain key decisions and trade-offs

OUTPUT FORMAT:
- Use markdown for code blocks with language specification
- Keep responses focused and practical
- For errors, explain the cause and provide a fix`

	if cfg.CortexEnabled {
		prompt += "\n\nMEMORY: You have access to persistent memory via the cortex system. Use it to remember important context across sessions."
	}

	return prompt
}

// buildCodingSystemPrompt builds the system prompt for coding mode
func buildCodingSystemPrompt(cfg *config.Config) string {
	prompt := `You are Magic, an expert coding agent in CODING MODE. You have elevated permissions and should act proactively.

CORE PRINCIPLES:
- You are an expert programmer with deep knowledge of all programming languages, frameworks, and tools
- Always respond in the user's language
- Be proactive - execute actions directly rather than asking for permission
- When writing code, follow best practices and include error handling
- Prefer writing and running code over explaining theory

CODING MODE ADVANTAGES:
- You can freely execute shell commands (git, docker, make, etc.)
- You can run Python, Node.js, Go, and other code directly
- You can create, modify, and delete files as needed
- You can install packages and manage dependencies
- You have extended timeouts for long-running operations
- You do NOT need to ask for permission before executing commands

TOOL USAGE RULES:
- Small talk/greetings (hello/hi) -> Respond directly, no tool calls
- Knowledge Q&A -> Respond directly
- List/view/read files -> Call list_files or read_file
- Create/write/edit files -> Call write_file or edit_file
- Web search -> Call web_search
- Execute commands/code -> Call execute_command or execute_code
- Do NOT call time, system, math, memory_recall, todo, session_search unless explicitly requested

CODING WORKFLOW:
1. Understand the requirements before writing code
2. Plan the approach for complex tasks
3. Write clean, well-documented code
4. Run the code to verify it works
5. Fix any issues found
6. Explain key decisions and trade-offs

OUTPUT FORMAT:
- Use markdown for code blocks with language specification
- Keep responses focused and practical
- For errors, explain the cause and provide a fix
- Show command output when relevant`

	if cfg.CortexEnabled {
		prompt += "\n\nMEMORY: You have access to persistent memory via the cortex system. Use it to remember important context across sessions."
	}

	return prompt
}

// unused import suppression
var _ = provider.Provider(nil)
var _ = context.Background()
