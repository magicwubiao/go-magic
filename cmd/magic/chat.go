package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/google/uuid"
	"github.com/magicwubiao/go-magic/internal/agent"
	"github.com/magicwubiao/go-magic/internal/cortex"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/internal/session"
	"github.com/magicwubiao/go-magic/internal/skills"
	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/pkg/config"
)

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Start interactive chat with Magic Agent",
	Long:  "Start an interactive chat session with Magic Agent.\nFeatures: streaming output, slash commands, skills loading, session persistence.\nType /help to see available commands.",
	RunE:  runChat,
}

func init() {
	rootCmd.AddCommand(chatCmd)
	chatCmd.Flags().BoolP("stream", "s", true, "Enable streaming output")
	chatCmd.Flags().BoolP("no-stream", "n", false, "Disable streaming output")
	chatCmd.Flags().BoolP("legacy", "l", false, "Use legacy REPL mode")
	chatCmd.Flags().BoolP("tui", "t", true, "Use TUI (terminal UI) mode")
}

func runChat(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err == config.ErrNoConfig {
		fmt.Println("Welcome to magic! It looks like this is your first run.")
		fmt.Println("Let's set things up...")
		runSetup(nil, nil)
		// Reload config after setup
		cfg, err = config.Load()
		if err != nil {
			return fmt.Errorf("failed to load config after setup: %v", err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}

	ctx := context.Background()

	provCfg, ok := cfg.Providers[cfg.Provider]
	if !ok {
		return fmt.Errorf("provider %s not configured", cfg.Provider)
	}

	var prov provider.Provider
	switch cfg.Provider {
	case "openai":
		prov = provider.NewOpenAIProvider(provCfg.APIKey, provCfg.BaseURL, provCfg.Model)
	case "anthropic":
		prov = provider.NewAnthropicProvider(provCfg.APIKey, provCfg.Model)
	case "deepseek":
		prov = provider.NewDeepSeekProvider(provCfg.APIKey, provCfg.Model)
	case "huoshan":
		prov = provider.NewHuoshanProvider(provCfg.APIKey, provCfg.BaseURL, provCfg.Model)
	case "dashscope":
		prov = provider.NewDashScopeProvider(provCfg.APIKey, provCfg.BaseURL, provCfg.Model)
	case "kimi":
		prov = provider.NewKimiProvider(provCfg.APIKey, provCfg.BaseURL, provCfg.Model)
	case "minimax":
		prov = provider.NewMiniMaxProvider(provCfg.APIKey, provCfg.BaseURL, provCfg.Model)
	case "ollama":
		prov = provider.NewOllamaProvider(provCfg.APIKey, provCfg.Model)
	case "openrouter":
		prov = provider.NewOpenRouterProvider(provCfg.APIKey, provCfg.BaseURL, provCfg.Model)
	case "vllm":
		prov = provider.NewVLLMProvider(provCfg.APIKey, provCfg.Model)
	case "zhipu":
		prov = provider.NewZhipuProvider(provCfg.APIKey, provCfg.BaseURL, provCfg.Model)
	case "gemini":
		prov = provider.NewGeminiProvider(provCfg.APIKey, provCfg.BaseURL, provCfg.Model)
	case "groq":
		prov = provider.NewGroqProvider(provCfg.APIKey, provCfg.BaseURL, provCfg.Model)
	case "together":
		prov = provider.NewTogetherProvider(provCfg.APIKey, provCfg.BaseURL, provCfg.Model)
	case "mistral":
		prov = provider.NewMistralProvider(provCfg.APIKey, provCfg.BaseURL, provCfg.Model)
	case "cohere":
		prov = provider.NewCohereProvider(provCfg.APIKey, provCfg.BaseURL, provCfg.Model)
	case "perplexity":
		prov = provider.NewPerplexityProvider(provCfg.APIKey, provCfg.BaseURL, provCfg.Model)
	case "doubao":
		prov = provider.NewDoubaoProvider(provCfg.APIKey, provCfg.BaseURL, provCfg.Model)
	case "wenxin":
		prov = provider.NewWenxinProvider(provCfg.APIKey, provCfg.APIKey, provCfg.Model)
	case "moonshot":
		prov = provider.NewMoonshotProvider(provCfg.APIKey, provCfg.Model)
	case "mimo":
		prov = provider.NewMiMoProvider(provCfg.APIKey, provCfg.BaseURL, provCfg.Model)
	case "hunyuan":
		prov = provider.NewHunyuanProvider(provCfg.APIKey, provCfg.BaseURL, provCfg.Model)
	case "custom":
		prov = provider.NewOpenAICompatibleProvider("custom", provCfg.APIKey, provCfg.BaseURL, provCfg.Model)
	default:
		return fmt.Errorf("unknown provider: %s", cfg.Provider)
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

	// Check for legacy mode
	useLegacy, _ := cmd.Flags().GetBool("legacy")
	if useLegacy {
		return runLegacyChat(cmd, ctx, cfg, prov, registry, store)
	}

	// Run new REPL
	runREPLChat(ctx, cfg, prov, registry, store)
	return nil
}

// runREPLChat runs the enhanced REPL
func runREPLChat(ctx context.Context, cfg *config.Config, prov provider.Provider, registry *tool.Registry, store *session.Store) {
	repl := NewREPL(cfg, prov, registry, store)
	repl.Run()
}

// runLegacyChat runs the original legacy chat mode
func runLegacyChat(cmd *cobra.Command, ctx context.Context, cfg *config.Config, prov provider.Provider, registry *tool.Registry, store *session.Store) error {
	// Generate tools schema for provider
	toolsSchema := getToolsSchema(registry)

	// Initialize agent with optional cortex
	home, _ := os.UserHomeDir()
	var cortexMgr *cortex.Manager
	var agentOpts []agent.AgentOption
	if cfg.CortexEnabled {
		cortexMgr = cortex.NewManager(filepath.Join(home, ".magic", "cortex"))
		cortexMgr.Start()
		agentOpts = append(agentOpts, agent.WithCortex(cortexMgr))
	}
	aiAgent := agent.NewEnhancedAgent(prov, registry, toolsSchema, `You are Magic, a helpful AI assistant.

RULES:
- Small talk/greetings (hello/hi) → Respond directly, do not call tools
- Knowledge Q&A → Respond directly
- List/view/read files → Call list_files or read_file
- Create/write files → Call write_file
- Web search → Call web_search
- Execute command/code → Call execute_command
- Do not call time, system, math, memory_recall, todo, session_search unless explicitly requested
- Respond in the user's language
- Summarize file lists concisely, do not output raw JSON`, agentOpts...)

	// Note: cortex memory is NOT injected into system prompt here.
	// Memory injection is controlled by memoryEnabled flag and handled
	// inside RunWithCortex when appropriate. Injecting raw memory
	// context pollutes the prompt and confuses the model.

	// Load skills if available (compact list only, not full content)
	if mgr, err := skills.NewManager(); err == nil {
		if skillsList := mgr.GetSkillsList(); skillsList != "" {
			aiAgent.AddSkillsContext(skillsList)
			fmt.Println("Skills loaded.")
		}
	}

	// Generate session ID
	sessionID := uuid.New().String()
	aiAgent.SetSession(sessionID)

	// Check streaming flag
	noStream, _ := cmd.Flags().GetBool("no-stream")
	enableStream, _ := cmd.Flags().GetBool("stream")
	streamingEnabled := enableStream && !noStream

	// State for undo/retry
	var historyBeforeUndo []provider.Message
	var lastUserInput string

	fmt.Printf("Magic Agent v%s\n", Version)
	logInfo("Magic Agent v%s started | Provider: %s | Model: %s | Tools: %d", Version, cfg.Provider, cfg.Model, len(toolsSchema))
	fmt.Printf("Provider: %s | Model: %s\n", cfg.Provider, cfg.Model)
	fmt.Printf("Streaming: %s | Commands: /help\n\n", map[bool]string{true: "ON", false: "OFF"}[streamingEnabled])

	for {
		fmt.Print("> ")
		var input string

		// Read input
		line, err := readLineMultiLine()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			fmt.Printf("Error reading input: %v\n", err)
			continue
		}
		input = line

		// Process slash commands
		if strings.HasPrefix(input, "/") {
			cmdName, cmdArgs := parseSlashCommand(input)

			switch cmdName {
			case "help", "?":
				showHelp()
				continue
			case "exit", "quit", "q":
				// Save session before exit
				if store != nil {
					sess := &session.Session{
						ID:       sessionID,
						Profile:  cfg.Profile,
						Platform: "cli",
						Messages: aiAgent.GetHistory(),
					}
					store.SaveSession(ctx, sess)
				}
				fmt.Println("Goodbye!")
				return nil
			case "new", "reset":
				// Save current session before reset
				if store != nil {
					sess := &session.Session{
						ID:       sessionID,
						Profile:  cfg.Profile,
						Platform: "cli",
						Messages: aiAgent.GetHistory(),
					}
					store.SaveSession(ctx, sess)
				}

				aiAgent.Reset()
				sessionID = uuid.New().String()
				aiAgent.SetSession(sessionID)
				lastUserInput = ""
				historyBeforeUndo = nil

				// Reload skills (compact list)
				if mgr, err := skills.NewManager(); err == nil {
					if skillsList := mgr.GetSkillsList(); skillsList != "" {
						aiAgent.AddSkillsContext(skillsList)
					}
				}
				fmt.Println("New conversation started.")
				continue
			case "tools":
				fmt.Println("Available tools:")
				for _, tName := range registry.List() {
					t, err := registry.Get(tName)
					if err != nil {
						fmt.Printf("  - %s (error: %v)\n", tName, err)
						continue
					}
					fmt.Printf("  - %s: %s\n", t.Name(), t.Description())
				}
				fmt.Println()
				continue
			case "skills":
				showSkills()
				continue
			case "compress":
				aiAgent.EnableCompression(true)
				fmt.Println("History compression triggered.")
				continue
			case "usage":
				showUsage(aiAgent)
				continue
			case "undo":
				if len(historyBeforeUndo) > 0 {
					aiAgent.SetHistory(historyBeforeUndo)
					historyBeforeUndo = nil
					fmt.Println("Undone. Last response removed.")
				} else {
					fmt.Println("Nothing to undo.")
				}
				continue
			case "retry":
				if lastUserInput != "" {
					historyBeforeUndo = aiAgent.GetHistory()
					// Remove last assistant and tool messages
					history := historyBeforeUndo
					for len(history) > 0 && history[len(history)-1].Role != "user" {
						history = history[:len(history)-1]
					}
					aiAgent.SetHistory(history)
				} else {
					fmt.Println("No message to retry.")
				}
				continue
			case "stream":
				streamingEnabled = !streamingEnabled
				fmt.Printf("Streaming %s.\n", map[bool]string{true: "enabled", false: "disabled"}[streamingEnabled])
				continue
			case "clear":
				aiAgent.Reset()
				historyBeforeUndo = nil
				lastUserInput = ""
				fmt.Println("Conversation cleared.")
				continue
			case "history":
				showHistory(aiAgent)
				continue
			case "model":
				if cmdArgs != "" {
					// Change model
					newModel := strings.TrimSpace(cmdArgs)
					if newModel == "" {
						fmt.Println("Please specify a model name.")
						fmt.Printf("Current model: %s\n", cfg.Model)
						continue
					}

					// Update config
					cfg.Model = newModel
					if err := cfg.Save(); err != nil {
						fmt.Printf("Failed to save config: %v\n", err)
					} else {
						fmt.Printf("Model changed to: %s\n", newModel)
						fmt.Println("Note: Restart the chat to apply the new model.")
					}
				} else {
					fmt.Printf("Current model: %s\n", cfg.Model)
					fmt.Println("Usage: /model <model-name>")
				}
				continue
			case "stop":
				fmt.Println("Stop not supported in legacy mode.")
				continue
			case "save":
				fmt.Println("Save not supported in legacy mode. Use /new to start fresh.")
				continue
			case "load":
				fmt.Println("Load not supported in legacy mode.")
				continue
			default:
				fmt.Printf("Unknown command: %s\n", cmdName)
				fmt.Println("Type /help for available commands.")
				continue
			}
		}

		if input == "" {
			continue
		}

		lastUserInput = input
		historyBeforeUndo = aiAgent.GetHistory()

		logInfo("User input: %s", truncateLogMsg(input, 200))

		// Run conversation with or without streaming
		if streamingEnabled {
			fmt.Print("\n") // Move to new line for output
			err := aiAgent.RunConversationStream(ctx, input, func(content string, done bool) {
				if done {
					fmt.Println()
				} else {
					fmt.Print(content)
				}
			})
			if err != nil {
				fmt.Printf("\nError: %v\n\n", err)
				logError("Stream error: %v", err)
			} else {
				logInfo("Stream completed, history: %d messages", len(aiAgent.GetHistory()))
			}
		} else {
			fmt.Print("Thinking...")
			response, err := aiAgent.RunConversation(ctx, input)
			fmt.Print("\r          \r")

			if err != nil {
				fmt.Printf("Error: %v\n\n", err)
				logError("Chat error: %v", err)
				continue
			}

			logInfo("Chat completed, response: %s", truncateLogMsg(response, 200))

			fmt.Printf("%s\n\n", response)
		}

		// Auto-save session after each exchange
		if store != nil {
			sess := &session.Session{
				ID:       sessionID,
				Profile:  cfg.Profile,
				Platform: "cli",
				Messages: aiAgent.GetHistory(),
			}
			store.SaveSession(ctx, sess)
		}
	}
	return nil
}

// readLineMultiLine reads input from stdin
func readLineMultiLine() (string, error) {
	reader := bufio.NewReaderSize(os.Stdin, 4096)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// parseSlashCommand parses a slash command into name and arguments
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

// showHelp displays help information
func showHelp() {
	fmt.Println("\n=== Magic Agent Commands ===")
	fmt.Println()
	fmt.Println("  /new, /reset  - Start a new conversation")
	fmt.Println("  /model        - Show current model")
	fmt.Println("  /compress     - Manually compress context")
	fmt.Println("  /usage        - Show token usage statistics")
	fmt.Println("  /skills       - List available skills")
	fmt.Println("  /tools        - List available tools")
	fmt.Println("  /undo         - Undo last assistant response")
	fmt.Println("  /retry        - Retry last user message")
	fmt.Println("  /stream       - Toggle streaming mode")
	fmt.Println("  /clear        - Clear conversation history")
	fmt.Println("  /history      - Show conversation history")
	fmt.Println("  /help         - Show this help message")
	fmt.Println("  /exit         - Exit the chat")
	fmt.Println()
}

// showSkills lists available skills
func showSkills() {
	fmt.Println("\n=== Available Skills ===")

	// Load and display skills
	if mgr, err := skills.NewManager(); err == nil {
		skillList := mgr.ListSkills()
		if len(skillList) == 0 {
			fmt.Println("  No skills installed.")
		} else {
			for _, skill := range skillList {
				fmt.Printf("  - %s\n", skill)
			}
		}
	} else {
		fmt.Println("  Skills manager not available.")
	}
	fmt.Println()
}

// showUsage displays usage statistics
func showUsage(a *agent.Agent) {
	historyLen := a.GetHistoryLength()
	fmt.Printf("\n=== Usage Statistics ===\n")
	fmt.Printf("  History length: %d chars\n", historyLen)
	fmt.Printf("  History messages: %d\n", len(a.GetHistory()))
	fmt.Println()
}

// showHistory displays conversation history
func showHistory(a *agent.Agent) {
	fmt.Println("\n=== Conversation History ===")
	history := a.GetHistory()
	for i, msg := range history {
		role := msg.Role
		if role == "system" {
			role = "[System]"
		}
		content := msg.Content
		if len(content) > 100 {
			content = content[:100] + "..."
		}
		if content != "" {
			fmt.Printf("[%d] %s: %s\n", i, role, content)
		}
	}
	fmt.Println()
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