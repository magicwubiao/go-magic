package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/internal/skills"
	"github.com/magicwubiao/go-magic/internal/subagent"
	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// convertModelStringsToModelInfo converts []string to []provider.ModelInfo
func convertModelStringsToModelInfo(models []string) []provider.ModelInfo {
	if len(models) == 0 {
		return nil
	}
	result := make([]provider.ModelInfo, len(models))
	for i, m := range models {
		result[i] = provider.ModelInfo{ID: m, Name: m}
	}
	return result
}

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
	registry.RegisterAll(cfg.WorkingDir)

	// Register skill invoke tool
	if skillMgr, err := skills.NewManager(); err == nil {
		registry.RegisterSkillTool(skillMgr)
	}

	// Create adapter for tool registry
	registryAdapter := newToolRegistryAdapter(registry)

	// Create agent factory for subagents
	agentFactory := func(p provider.Provider, reg subagent.ToolRegistry, toolsSchema []map[string]interface{}, systemPrompt string) subagent.AgentRunner {
		return &simpleAgentRunner{
			provider:     p,
			registry:     reg,
			toolsSchema:  toolsSchema,
			systemPrompt: systemPrompt,
		}
	}

	mgr := subagent.NewManager(subCfg, prov, registryAdapter, agentFactory)
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

	// Create adapter for tool registry
	registryAdapter := newToolRegistryAdapter(registry)

	// Create agent factory for subagents
	agentFactory := func(p provider.Provider, reg subagent.ToolRegistry, toolsSchema []map[string]interface{}, systemPrompt string) subagent.AgentRunner {
		return &simpleAgentRunner{
			provider:     p,
			registry:     reg,
			toolsSchema:  toolsSchema,
			systemPrompt: systemPrompt,
		}
	}

	mgr := subagent.NewManager(subCfg, prov, registryAdapter, agentFactory)

	stats := mgr.GetStats()

	data, _ := json.MarshalIndent(stats, "", "  ")
	fmt.Println(string(data))
}

func createProvider(name string, cfg config.ProviderConfig) provider.Provider {
	// Use the same model selection logic as config.CreateProvider:
	// Models[0] > Model field > config-level Model (this ensures consistency between chat and gateway modes)
	userModels := convertModelStringsToModelInfo(cfg.Models)
	model := cfg.GetCurrentModel()

	log.Infof("[createProvider] name=%s, baseURL=%s, model=%q, models=%v, apiKeyLen=%d",
		name, cfg.BaseURL, model, cfg.Models, len(cfg.APIKey))

	switch name {
	case "openai":
		return provider.NewOpenAIProvider(cfg.APIKey, cfg.BaseURL, model)
	case "anthropic":
		return provider.NewAnthropicProvider(cfg.APIKey, model)
	case "deepseek":
		return provider.NewDeepSeekProvider(cfg.APIKey, cfg.BaseURL, model, userModels)
	case "dashscope":
		return provider.NewDashScopeProvider(cfg.APIKey, cfg.BaseURL, model)
	case "kimi":
		return provider.NewKimiProvider(cfg.APIKey, cfg.BaseURL, model)
	case "minimax":
		return provider.NewMiniMaxProvider(cfg.APIKey, cfg.BaseURL, model)
	case "ollama":
		return provider.NewOllamaProvider(cfg.BaseURL, model)
	case "openrouter":
		return provider.NewOpenRouterProvider(cfg.APIKey, cfg.BaseURL, model)
	case "vllm":
		return provider.NewVLLMProvider(cfg.BaseURL, model)
	case "zhipu":
		return provider.NewZhipuProvider(cfg.APIKey, cfg.BaseURL, model)
	case "gemini":
		return provider.NewGeminiProvider(cfg.APIKey, cfg.BaseURL, model)
	case "groq":
		return provider.NewGroqProvider(cfg.APIKey, cfg.BaseURL, model)
	case "together":
		return provider.NewTogetherProvider(cfg.APIKey, cfg.BaseURL, model)
	case "mistral":
		return provider.NewMistralProvider(cfg.APIKey, cfg.BaseURL, model)
	case "cohere":
		return provider.NewCohereProvider(cfg.APIKey, cfg.BaseURL, model)
	case "perplexity":
		return provider.NewPerplexityProvider(cfg.APIKey, cfg.BaseURL, model)
	case "doubao":
		return provider.NewDoubaoProvider(cfg.APIKey, cfg.BaseURL, model)
	case "wenxin":
		return provider.NewWenxinProvider(cfg.APIKey, cfg.BaseURL, model)
	case "moonshot":
		return provider.NewMoonshotProvider(cfg.APIKey, cfg.BaseURL, model)
	case "mimo":
		return provider.NewMiMoProvider(cfg.APIKey, cfg.BaseURL, model)
	case "hunyuan":
		return provider.NewHunyuanProvider(cfg.APIKey, cfg.BaseURL, model)
	case "huoshan":
		return provider.NewHuoshanProvider(cfg.APIKey, cfg.BaseURL, model)
	default:
		if cfg.BaseURL != "" {
			return provider.NewOpenAICompatibleProvider(name, cfg.APIKey, cfg.BaseURL, model, userModels)
		}
		return nil
	}
}
