package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/internal/subagent"
	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/pkg/config"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage subagents for parallel task execution",
	Long: `Spawn and manage subagents that can execute tasks in parallel.
Each subagent operates in isolation with its own context.`,
}

var agentSpawnCmd = &cobra.Command{
	Use:   "spawn <description> <input>",
	Short: "Spawn a new subagent to execute a task",
	Args:  cobra.MinimumNArgs(1),
	Run:   runAgentSpawn,
}

var agentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all active subagents",
	Run:   runAgentList,
}

var agentKillCmd = &cobra.Command{
	Use:   "kill <agent-id>",
	Short: "Terminate a subagent",
	Args:  cobra.ExactArgs(1),
	Run:   runAgentKill,
}

var agentStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show subagent statistics",
	Run:   runAgentStats,
}

var (
	agentTools    []string
	agentTimeout  int
	agentMaxDepth int
)

func init() {
	rootCmd.AddCommand(agentCmd)
	agentCmd.AddCommand(agentSpawnCmd)
	agentCmd.AddCommand(agentListCmd)
	agentCmd.AddCommand(agentKillCmd)
	agentCmd.AddCommand(agentStatsCmd)

	agentSpawnCmd.Flags().StringSliceVarP(&agentTools, "tools", "t", []string{}, "Tools to enable for the subagent")
	agentSpawnCmd.Flags().IntVar(&agentTimeout, "timeout", 120, "Task timeout in seconds")
	agentSpawnCmd.Flags().IntVar(&agentMaxDepth, "max-depth", 2, "Maximum subagent recursion depth")
}

func runAgentSpawn(cmd *cobra.Command, args []string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	provCfg, ok := cfg.Providers[cfg.Provider]
	if !ok {
		fmt.Printf("Provider %s not configured\n", cfg.Provider)
		os.Exit(1)
	}

	// Create provider (simplified - in real impl would use proper provider factory)
	prov := createProvider(cfg.Provider, provCfg)
	if prov == nil {
		fmt.Printf("Failed to create provider: %s\n", cfg.Provider)
		os.Exit(1)
	}

	// Create subagent manager
	subCfg := &subagent.Config{
		MaxConcurrent: agentMaxDepth,
		MaxDepth:      agentMaxDepth,
		Timeout:       time.Duration(agentTimeout) * time.Second,
	}

	if cfg.SubAgent != nil {
		subCfg.MaxConcurrent = cfg.SubAgent.MaxConcurrent
		subCfg.MaxDepth = cfg.SubAgent.MaxDepth
		if cfg.SubAgent.Timeout > 0 {
			subCfg.Timeout = cfg.SubAgent.Timeout
		}
	}

	registry := tool.NewRegistry()
	registry.RegisterAll()

	mgr := subagent.NewManager(subCfg, prov, registry)
	mgr.Start()
	defer mgr.Stop()

	// Get input (either as args or stdin)
	input := ""
	if len(args) > 1 {
		input = args[1]
	}

	taskID, err := mgr.SpawnTask(args[0], input, agentTools)
	if err != nil {
		fmt.Printf("Failed to spawn task: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Spawned task: %s\n", taskID)

	// Wait for result
	result, err := mgr.WaitForResult(taskID, subCfg.Timeout)
	if err != nil {
		fmt.Printf("Error waiting for result: %v\n", err)
		os.Exit(1)
	}

	if result.Success {
		fmt.Printf("\nResult:\n%s\n", result.Output)
	} else {
		fmt.Printf("\nError: %s\n", result.Error)
		os.Exit(1)
	}
}

func runAgentList(cmd *cobra.Command, args []string) {
	// Create a manager just to get stats
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// This will show stats (which includes active agents)
	fmt.Println("Active subagents:")

	subCfg := subagent.DefaultConfig()
	if cfg.SubAgent != nil {
		subCfg = &subagent.Config{
			MaxConcurrent: cfg.SubAgent.MaxConcurrent,
			MaxDepth:      cfg.SubAgent.MaxDepth,
			Timeout:       cfg.SubAgent.Timeout,
		}
	}

	fmt.Printf("\nConfiguration:")
	fmt.Printf("  Max Concurrent: %d\n", subCfg.MaxConcurrent)
	fmt.Printf("  Max Depth: %d\n", subCfg.MaxDepth)
	fmt.Printf("  Timeout: %v\n", subCfg.Timeout)
}

func runAgentKill(cmd *cobra.Command, args []string) {
	agentID := args[0]

	fmt.Printf("Killing subagent: %s\n", agentID)
	fmt.Println("(Note: In a running session, this would kill the actual agent)")
}

func runAgentStats(cmd *cobra.Command, args []string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Create temporary manager for stats
	provCfg, ok := cfg.Providers[cfg.Provider]
	if !ok {
		fmt.Printf("Provider %s not configured\n", cfg.Provider)
		os.Exit(1)
	}

	prov := createProvider(cfg.Provider, provCfg)
	registry := tool.NewRegistry()

	subCfg := subagent.DefaultConfig()
	if cfg.SubAgent != nil {
		subCfg = &subagent.Config{
			MaxConcurrent: cfg.SubAgent.MaxConcurrent,
			MaxDepth:      cfg.SubAgent.MaxDepth,
			Timeout:       cfg.SubAgent.Timeout,
		}
	}

	mgr := subagent.NewManager(subCfg, prov, registry)

	stats := mgr.GetStats()

	data, _ := json.MarshalIndent(stats, "", "  ")
	fmt.Println(string(data))
}

func createProvider(name string, cfg config.ProviderConfig) provider.Provider {
	switch name {
	case "openai":
		return provider.NewOpenAIProvider(cfg.APIKey, cfg.BaseURL, cfg.Model)
	case "anthropic":
		return provider.NewAnthropicProvider(cfg.APIKey, cfg.Model)
	case "deepseek":
		return provider.NewDeepSeekProvider(cfg.APIKey, cfg.Model)
	case "huoshan":
		return provider.NewHuoshanProvider(cfg.APIKey, cfg.Model)
	case "dashscope":
		return provider.NewDashScopeProvider(cfg.APIKey, cfg.Model)
	case "kimi":
		return provider.NewKimiProvider(cfg.APIKey, cfg.Model)
	case "minimax":
		return provider.NewMinimaxProvider(cfg.APIKey, cfg.Model)
	case "ollama":
		return provider.NewOllamaProvider(cfg.APIKey, cfg.Model)
	case "openrouter":
		return provider.NewOpenRouterProvider(cfg.APIKey, cfg.Model)
	case "vllm":
		return provider.NewVLLMProvider(cfg.APIKey, cfg.Model)
	case "zhipu":
		return provider.NewZhipuProvider(cfg.APIKey, cfg.Model)
	case "gemini":
		return provider.NewGeminiProvider(cfg.APIKey, cfg.Model)
	case "groq":
		return provider.NewGroqProvider(cfg.APIKey, cfg.Model)
	case "together":
		return provider.NewTogetherProvider(cfg.APIKey, cfg.Model)
	case "mistral":
		return provider.NewMistralProvider(cfg.APIKey, cfg.Model)
	case "cohere":
		return provider.NewCohereProvider(cfg.APIKey, cfg.Model)
	case "perplexity":
		return provider.NewPerplexityProvider(cfg.APIKey, cfg.Model)
	case "doubao":
		return provider.NewDoubaoProvider(cfg.APIKey, cfg.Model)
	case "wenxin":
		return provider.NewWenxinProvider(cfg.APIKey, cfg.APIKey, cfg.Model)
	case "moonshot":
		return provider.NewMoonshotProvider(cfg.APIKey, cfg.Model)
	case "mimo":
		return provider.NewMiMoProvider(cfg.APIKey, cfg.Model)
	case "hunyuan":
		return provider.NewHunyuanProvider(cfg.APIKey, cfg.Model)
	default:
		// Fallback: try to use openai-compatible endpoint
		if cfg.BaseURL != "" {
			return provider.NewOpenAIProvider(cfg.APIKey, cfg.BaseURL, cfg.Model)
		}
		return nil
	}
}
