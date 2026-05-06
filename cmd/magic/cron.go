package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/cron"
)

var cronCmd = &cobra.Command{
	Use:   "cron",
	Short: "Manage scheduled tasks",
}

var cronListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all cron jobs",
	Run:   runCronList,
}

var cronAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Add a new cron job",
	Args:  cobra.ExactArgs(1),
	Run:   runCronAdd,
}

var (
	cronSchedule   string
	cronPrompt     string
	cronEnabled    bool
	cronDeleteCmd  *cobra.Command
	cronEnableCmd  *cobra.Command
	cronDisableCmd *cobra.Command
	cronRunCmd     *cobra.Command
)

var cronRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a cron job",
	Args:  cobra.ExactArgs(1),
	Run:   runCronRemove,
}

var cronEditCmd = &cobra.Command{
	Use:   "edit <name>",
	Short: "Edit a cron job",
	Args:  cobra.ExactArgs(1),
	Run:   runCronEdit,
}

var cronToggleCmd = &cobra.Command{
	Use:   "toggle <name>",
	Short: "Toggle a cron job enabled/disabled",
	Args:  cobra.ExactArgs(1),
	Run:   runCronToggle,
}

var cronTestCmd = &cobra.Command{
	Use:   "test <name>",
	Short: "Test run a cron job immediately",
	Args:  cobra.ExactArgs(1),
	Run:   runCronTest,
}

func init() {
	cronAddCmd.Flags().StringVarP(&cronSchedule, "schedule", "s", "0 9 * * *", "Cron schedule expression")
	cronAddCmd.Flags().StringVarP(&cronPrompt, "prompt", "p", "Daily summary", "Prompt to execute")
	cronAddCmd.Flags().BoolVarP(&cronEnabled, "enabled", "e", false, "Enable the cron job immediately")

	cronRemoveCmd.Flags().BoolP("force", "f", false, "Skip confirmation")

	cronCmd.AddCommand(cronListCmd)
	cronCmd.AddCommand(cronAddCmd)
	cronCmd.AddCommand(cronRemoveCmd)
	cronCmd.AddCommand(cronEditCmd)
	cronCmd.AddCommand(cronToggleCmd)
	cronCmd.AddCommand(cronTestCmd)
	rootCmd.AddCommand(cronCmd)
}

func runCronList(cmd *cobra.Command, args []string) {
	mgr, err := cron.NewManager()
	if err != nil {
		fmt.Printf("Failed to load cron jobs: %v\n", err)
		os.Exit(1)
	}

	jobs := mgr.List()
	if len(jobs) == 0 {
		fmt.Println("No cron jobs configured.")
		fmt.Println("Use 'magic cron add <name>' to create one.")
		return
	}

	fmt.Println("Cron Jobs:")
	for _, job := range jobs {
		status := "disabled"
		if job.Enabled {
			status = "enabled"
		}
		fmt.Printf("  [%s] %s - %s (schedule: %s)\n", status, job.Name, job.Description, job.Schedule)
	}
}

func runCronAdd(cmd *cobra.Command, args []string) {
	name := args[0]

	mgr, err := cron.NewManager()
	if err != nil {
		fmt.Printf("Failed to create manager: %v\n", err)
		os.Exit(1)
	}

	// Generate unique ID based on timestamp
	timestamp := time.Now().UnixMilli()
	job := &cron.Job{
		ID:          fmt.Sprintf("%d", timestamp),
		Name:        name,
		Description: fmt.Sprintf("Cron job: %s", name),
		Schedule:    cronSchedule,
		Prompt:      cronPrompt,
		Enabled:     cronEnabled,
	}

	if err := mgr.Add(job); err != nil {
		fmt.Printf("Failed to add job: %v\n", err)
		os.Exit(1)
	}

	status := "disabled"
	if cronEnabled {
		status = "enabled"
	}

	fmt.Printf("✓ Cron job '%s' added (%s)\n", name, status)
	fmt.Printf("  Schedule: %s\n", cronSchedule)
	fmt.Printf("  Prompt: %s\n", cronPrompt)
	fmt.Printf("\nEdit job details in ~/.magic/cron_jobs.json")
}

func runCronRemove(cmd *cobra.Command, args []string) {
	name := args[0]
	force, _ := cmd.Flags().GetBool("force")

	if !force {
		fmt.Printf("Are you sure you want to remove cron job '%s'? (y/N): ", name)
		var confirm string
		fmt.Scanln(&confirm)
		if confirm != "y" && confirm != "Y" {
			fmt.Println("Removal cancelled.")
			return
		}
	}

	mgr, err := cron.NewManager()
	if err != nil {
		fmt.Printf("Failed to load manager: %v\n", err)
		os.Exit(1)
	}

	if err := mgr.Remove(name); err != nil {
		fmt.Printf("Failed to remove job: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Cron job '%s' removed.\n", name)
}

func runCronEdit(cmd *cobra.Command, args []string) {
	name := args[0]

	mgr, err := cron.NewManager()
	if err != nil {
		fmt.Printf("Failed to load manager: %v\n", err)
		os.Exit(1)
	}

	job := mgr.Get(name)
	if job == nil {
		fmt.Printf("Cron job '%s' not found.\n", name)
		os.Exit(1)
	}

	fmt.Printf("Editing cron job '%s'\n\n", name)
	fmt.Println("Leave field empty to keep current value.")
	fmt.Println("Type 'quit' to cancel.\n")

	reader := bufio.NewReader(os.Stdin)

	// Edit description
	fmt.Printf("Description [%s]: ", job.Description)
	if line, _ := reader.ReadString('\n'); strings.TrimSpace(line) != "" && line != "quit\n" {
		job.Description = strings.TrimSpace(line)
	}

	// Edit schedule
	fmt.Printf("Schedule [%s]: ", job.Schedule)
	if line, _ := reader.ReadString('\n'); strings.TrimSpace(line) != "" && line != "quit\n" {
		job.Schedule = strings.TrimSpace(line)
	}

	// Edit prompt
	fmt.Printf("Prompt [%s]: ", job.Prompt)
	if line, _ := reader.ReadString('\n'); strings.TrimSpace(line) != "" && line != "quit\n" {
		job.Prompt = strings.TrimSpace(line)
	}

	// Edit enabled
	enabledStr := "disabled"
	if job.Enabled {
		enabledStr = "enabled"
	}
	fmt.Printf("Enabled [%s]: ", enabledStr)
	if line, _ := reader.ReadString('\n'); strings.TrimSpace(line) != "" && line != "quit\n" {
		val := strings.TrimSpace(strings.ToLower(line))
		job.Enabled = val == "yes" || val == "y" || val == "true" || val == "1" || val == "enabled"
	}

	// Save changes
	if err := mgr.Update(job); err != nil {
		fmt.Printf("\nFailed to update job: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n✓ Cron job '%s' updated.\n", name)
}

func runCronToggle(cmd *cobra.Command, args []string) {
	name := args[0]

	mgr, err := cron.NewManager()
	if err != nil {
		fmt.Printf("Failed to load manager: %v\n", err)
		os.Exit(1)
	}

	job := mgr.Get(name)
	if job == nil {
		fmt.Printf("Cron job '%s' not found.\n", name)
		os.Exit(1)
	}

	job.Enabled = !job.Enabled
	if err := mgr.Update(job); err != nil {
		fmt.Printf("Failed to update job: %v\n", err)
		os.Exit(1)
	}

	status := "enabled"
	if !job.Enabled {
		status = "disabled"
	}
	fmt.Printf("Cron job '%s' is now %s.\n", name, status)
}

func runCronTest(cmd *cobra.Command, args []string) {
	name := args[0]

	mgr, err := cron.NewManager()
	if err != nil {
		fmt.Printf("Failed to load manager: %v\n", err)
		os.Exit(1)
	}

	job := mgr.Get(name)
	if job == nil {
		fmt.Printf("Cron job '%s' not found.\n", name)
		os.Exit(1)
	}

	fmt.Printf("Running cron job '%s'...\n", name)
	fmt.Printf("Schedule: %s\n", job.Schedule)
	fmt.Printf("Prompt: %s\n\n", job.Prompt)

	// Load config and create a temporary agent to execute the job
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Create provider
	provCfg, ok := cfg.Providers[cfg.Provider]
	if !ok {
		fmt.Printf("Provider %s not configured\n", cfg.Provider)
		os.Exit(1)
	}

	var prov provider.Provider
	switch cfg.Provider {
	case "openai":
		prov = provider.NewOpenAIProvider(provCfg.APIKey, provCfg.BaseURL, provCfg.Model)
	case "anthropic":
		prov = provider.NewAnthropicProvider(provCfg.APIKey, provCfg.Model)
	case "deepseek":
		prov = provider.NewDeepSeekProvider(provCfg.APIKey, provCfg.Model)
	default:
		fmt.Printf("Unsupported provider: %s\n", cfg.Provider)
		os.Exit(1)
	}

	// Create a simple tool registry (no tools for cron test)
	registry := tool.NewRegistry()
	registry.RegisterAll()

	// Create agent with minimal system prompt
	systemPrompt := "You are a helpful assistant running a scheduled task. Complete the given task concisely and accurately."
	aiAgent := agent.NewAIAgent(prov, registry, nil, systemPrompt)

	// Execute the job prompt
	ctx := context.Background()
	fmt.Println("Executing...")
	response, err := aiAgent.RunConversation(ctx, job.Prompt)

	if err != nil {
		fmt.Printf("\n❌ Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n✅ Execution completed!")
	fmt.Println("\n--- Result ---")
	fmt.Println(response)
}
