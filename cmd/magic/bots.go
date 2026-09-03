package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/bot"
	"github.com/magicwubiao/go-magic/pkg/config"
)

var (
	botsFlagModel    string
	botsFlagProvider string
	botsFlagTitle    string
	botsFlagDesc     string
	botsFlagPrompt   string
	botsFlagSchedule string
	botsFlagTools    string   // comma-separated tool allowlist
	botsFlagSkills   string   // comma-separated skill allowlist
	botsFlagMemory   string   // long-term memory block
	botsFlagAvatar   string   // emoji or image URL
	botsFlagEnv      []string // KEY=VALUE pairs (repeatable)
	botsFlagHidden   bool     // hide from dashboard list (keeps running)
)

var botsCmd = &cobra.Command{
	Use:   "bots",
	Short: "Manage Bot Mode agents (named profiles with persistent chats)",
	Long: `Bot Mode turns agent profiles into a roster of named Bots.

Each Bot has its own role, model pin, persona prompt, persistent canonical
chat session, and routines. Bots can message each other with the
message_agent tool.

Quick start:
  magic bots create researcher --title "Research Assistant" --prompt "You find and summarize papers."
  magic bots list
  magic bots chat researcher "Find recent papers on agent memory"
  magic bots routine add researcher daily-digest --schedule "0 9 * * *" --prompt "Summarize yesterday's findings."`,
}

func init() {
	rootCmd.AddCommand(botsCmd)

	createCmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new bot",
		Args:  cobra.ExactArgs(1),
		RunE:  runBotsCreate,
	}
	createCmd.Flags().StringVar(&botsFlagTitle, "title", "", "Short role label (e.g. \"Research Assistant\")")
	createCmd.Flags().StringVar(&botsFlagDesc, "desc", "", "What this bot does")
	createCmd.Flags().StringVar(&botsFlagPrompt, "prompt", "", "Persona / standing instructions")
	createCmd.Flags().StringVar(&botsFlagModel, "model", "", "Pin model (default: inherit global)")
	createCmd.Flags().StringVar(&botsFlagProvider, "provider", "", "Pin provider (default: inherit global)")
	createCmd.Flags().StringVar(&botsFlagTools, "tools", "", "Tool allowlist (comma separated; empty = all)")
	createCmd.Flags().StringVar(&botsFlagSkills, "skills", "", "Skill allowlist (comma separated; empty = all)")
	createCmd.Flags().StringVar(&botsFlagMemory, "memory", "", "Long-term memory block (markdown)")
	createCmd.Flags().StringVar(&botsFlagAvatar, "avatar", "", "Avatar: emoji or image URL")
	createCmd.Flags().StringArrayVar(&botsFlagEnv, "env", nil, "Per-bot credential KEY=VALUE (repeatable; written to bots/<name>/.env)")
	createCmd.Flags().BoolVar(&botsFlagHidden, "hidden", false, "Hide from the dashboard list (keeps running)")

	editCmd := &cobra.Command{
		Use:   "edit <name>",
		Short: "Edit an existing bot's config",
		Args:  cobra.ExactArgs(1),
		RunE:  runBotsEdit,
	}
	editCmd.Flags().StringVar(&botsFlagTitle, "title", "", "New title")
	editCmd.Flags().StringVar(&botsFlagDesc, "desc", "", "New description")
	editCmd.Flags().StringVar(&botsFlagPrompt, "prompt", "", "New persona instructions")
	editCmd.Flags().StringVar(&botsFlagModel, "model", "", "New model pin")
	editCmd.Flags().StringVar(&botsFlagProvider, "provider", "", "New provider pin")
	editCmd.Flags().StringVar(&botsFlagTools, "tools", "", "New tool allowlist (comma separated; empty = all)")
	editCmd.Flags().StringVar(&botsFlagSkills, "skills", "", "New skill allowlist (comma separated; empty = all)")
	editCmd.Flags().StringVar(&botsFlagMemory, "memory", "", "New long-term memory block")
	editCmd.Flags().StringVar(&botsFlagAvatar, "avatar", "", "New avatar (emoji or URL)")
	editCmd.Flags().StringArrayVar(&botsFlagEnv, "env", nil, "Replace per-bot credentials (KEY=VALUE, repeatable)")
	editCmd.Flags().BoolVar(&botsFlagHidden, "hidden", false, "Hide from dashboard list; use --hidden=false to show again")

	routineAdd := &cobra.Command{
		Use:   "add <bot> <routine-name> --schedule <cron> --prompt <text>",
		Short: "Attach a recurring routine to a bot",
		Args:  cobra.ExactArgs(2),
		RunE:  runBotsRoutineAdd,
	}
	routineAdd.Flags().StringVar(&botsFlagSchedule, "schedule", "", `Cron schedule (e.g. "0 9 * * *" = daily at 9am)`)
	routineAdd.Flags().StringVar(&botsFlagPrompt, "prompt", "", "Task prompt for each run")

	rootCmdRoutineRemove := &cobra.Command{
		Use:   "remove <bot> <routine-id-or-name>",
		Short: "Remove a bot's routine",
		Args:  cobra.ExactArgs(2),
		RunE:  runBotsRoutineRemove,
	}

	routineCmd := &cobra.Command{Use: "routine", Short: "Manage a bot's recurring routines"}
	routineCmd.AddCommand(routineAdd)
	routineCmd.AddCommand(rootCmdRoutineRemove)
	routineCmd.AddCommand(&cobra.Command{
		Use:   "enable <bot> <routine-id-or-name>",
		Short: "Enable a bot's routine",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBotsRoutineToggle(args[0], args[1], true)
		},
	})
	routineCmd.AddCommand(&cobra.Command{
		Use:   "disable <bot> <routine-id-or-name>",
		Short: "Disable a bot's routine (kept, but not scheduled)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBotsRoutineToggle(args[0], args[1], false)
		},
	})
	routineCmd.AddCommand(&cobra.Command{
		Use:   "list <bot>",
		Short: "List a bot's routines",
		Args:  cobra.ExactArgs(1),
		RunE:  runBotsRoutineList,
	})

	botsCmd.AddCommand(createCmd)
	botsCmd.AddCommand(editCmd)
	botsCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all bots with status",
		RunE:  runBotsList,
	})
	botsCmd.AddCommand(&cobra.Command{
		Use:   "show <name>",
		Short: "Show a bot's full config",
		Args:  cobra.ExactArgs(1),
		RunE:  runBotsShow,
	})
	botsCmd.AddCommand(&cobra.Command{
		Use:   "clone <name> <new-name>",
		Short: "Clone a bot's full profile under a new name (fresh chat history)",
		Args:  cobra.ExactArgs(2),
		RunE:  runBotsClone,
	})
	botsCmd.AddCommand(&cobra.Command{
		Use:   "hide <name>",
		Short: "Hide a bot from the dashboard list (it keeps running)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runBotsSetHidden(args[0], true)
		},
	})
	botsCmd.AddCommand(&cobra.Command{
		Use:   "unhide <name>",
		Short: "Un-hide a bot back into the dashboard list",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runBotsSetHidden(args[0], false)
		},
	})
	botsCmd.AddCommand(&cobra.Command{
		Use:   "chat <name> <message>",
		Short: "Send one message to a bot's canonical chat and print the reply",
		Args:  cobra.MinimumNArgs(2),
		RunE:  runBotsChat,
	})
	botsCmd.AddCommand(&cobra.Command{
		Use:   "message <from-bot> <to-bot> <message>",
		Short: "Deliver a message from one bot to another (fire-and-forget)",
		Args:  cobra.ExactArgs(3),
		RunE:  runBotsMessage,
	})
	botsCmd.AddCommand(&cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"delete", "rm"},
		Short:   "Delete a bot",
		Args:    cobra.ExactArgs(1),
		RunE:    runBotsRemove,
	})
	botsCmd.AddCommand(routineCmd)
}

// openBotStore returns the on-disk bot store.
func openBotStore() (*bot.Store, error) {
	return bot.NewStore(config.GetMagicHome())
}

func runBotsCreate(cmd *cobra.Command, args []string) error {
	name := args[0]
	if err := bot.ValidateName(name); err != nil {
		return err
	}

	store, err := openBotStore()
	if err != nil {
		return err
	}
	if _, err := store.Load(name); err == nil {
		return fmt.Errorf("bot %q already exists (use 'magic bots edit')", name)
	}

	cfg := &bot.Config{
		Name:         name,
		Title:        botsFlagTitle,
		Description:  botsFlagDesc,
		SystemPrompt: botsFlagPrompt,
		Model:        botsFlagModel,
		Provider:     botsFlagProvider,
		Tools:        parseCSVList(botsFlagTools),
		Skills:       parseCSVList(botsFlagSkills),
		Memory:       botsFlagMemory,
		Avatar:       botsFlagAvatar,
		Env:          parseEnvPairs(botsFlagEnv),
		Hidden:       botsFlagHidden,
		CreatedAt:    time.Now().Unix(),
	}
	if cfg.Title == "" {
		cfg.Title = strings.Title(strings.ReplaceAll(name, "-", " "))
	}
	if err := store.Save(cfg); err != nil {
		return err
	}

	fmt.Printf("✅ Bot %q created (%s)\n", name, storePathHint(name))
	fmt.Println("   Chat it:      magic bots chat " + name + " \"hello\"")
	fmt.Println("   Add routines: magic bots routine add " + name + " <name> --schedule \"0 9 * * *\" --prompt \"...\"")
	return enableBotModeHint()
}

func runBotsEdit(cmd *cobra.Command, args []string) error {
	store, err := openBotStore()
	if err != nil {
		return err
	}
	cfg, err := store.Load(args[0])
	if err != nil {
		return fmt.Errorf("bot not found: %s", args[0])
	}
	if cmd.Flags().Changed("title") {
		cfg.Title = botsFlagTitle
	}
	if cmd.Flags().Changed("desc") {
		cfg.Description = botsFlagDesc
	}
	if cmd.Flags().Changed("prompt") {
		cfg.SystemPrompt = botsFlagPrompt
	}
	if cmd.Flags().Changed("model") {
		cfg.Model = botsFlagModel
	}
	if cmd.Flags().Changed("provider") {
		cfg.Provider = botsFlagProvider
	}
	if cmd.Flags().Changed("tools") {
		cfg.Tools = parseCSVList(botsFlagTools)
	}
	if cmd.Flags().Changed("skills") {
		cfg.Skills = parseCSVList(botsFlagSkills)
	}
	if cmd.Flags().Changed("memory") {
		cfg.Memory = botsFlagMemory
	}
	if cmd.Flags().Changed("avatar") {
		cfg.Avatar = botsFlagAvatar
	}
	if cmd.Flags().Changed("env") {
		cfg.Env = parseEnvPairs(botsFlagEnv)
	}
	if cmd.Flags().Changed("hidden") {
		cfg.Hidden = botsFlagHidden
	}
	if err := store.Save(cfg); err != nil {
		return err
	}
	fmt.Printf("✅ Bot %q updated\n", cfg.Name)
	return nil
}

// runBotsSetHidden toggles the dashboard visibility flag of a bot.
func runBotsSetHidden(name string, hidden bool) error {
	store, err := openBotStore()
	if err != nil {
		return err
	}
	cfg, err := store.Load(name)
	if err != nil {
		return fmt.Errorf("bot not found: %s", name)
	}
	if cfg.Hidden == hidden {
		status := "shown"
		if hidden {
			status = "hidden"
		}
		fmt.Printf("ℹ️  Bot %q is already %s\n", name, status)
		return nil
	}
	cfg.Hidden = hidden
	if err := store.Save(cfg); err != nil {
		return err
	}
	if hidden {
		fmt.Printf("🙈 Bot %q hidden from the dashboard (still running)\n", name)
	} else {
		fmt.Printf("👀 Bot %q is visible in the dashboard again\n", name)
	}
	return nil
}

// runBotsClone duplicates a bot's full profile under a new name.
func runBotsClone(cmd *cobra.Command, args []string) error {
	store, err := openBotStore()
	if err != nil {
		return err
	}
	src, err := store.Load(args[0])
	if err != nil {
		return fmt.Errorf("bot not found: %s", args[0])
	}
	newName := args[1]
	if err := bot.ValidateName(newName); err != nil {
		return err
	}
	if _, err := store.Load(newName); err == nil {
		return fmt.Errorf("bot %q already exists", newName)
	}
	if strings.EqualFold(src.Name, newName) {
		return fmt.Errorf("new name must differ from the source bot")
	}

	clone := *src
	clone.Name = newName
	clone.Tools = append([]string(nil), src.Tools...)
	clone.Skills = append([]string(nil), src.Skills...)
	if src.Env != nil {
		clone.Env = make(map[string]string, len(src.Env))
		for k, v := range src.Env {
			clone.Env[k] = v
		}
	}
	now := time.Now().Unix()
	clone.CreatedAt = now
	clone.UpdatedAt = now
	if err := store.Save(&clone); err != nil {
		return err
	}
	fmt.Printf("✅ Cloned bot %q -> %q (profile copied, chat history starts fresh)\n", args[0], newName)
	fmt.Println("   The clone comes online on the next gateway restart (or immediately when created via the web UI).")
	return nil
}

// parseCSVList splits a comma-separated flag into a trimmed, non-empty list.
func parseCSVList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// parseEnvPairs converts KEY=VALUE pairs into a map.
func parseEnvPairs(pairs []string) map[string]string {
	if len(pairs) == 0 {
		return nil
	}
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 || strings.TrimSpace(kv[0]) == "" {
			continue
		}
		out[strings.TrimSpace(kv[0])] = kv[1]
	}
	return out
}

func runBotsList(cmd *cobra.Command, args []string) error {
	store, err := openBotStore()
	if err != nil {
		return err
	}
	configs, err := store.List()
	if err != nil {
		return err
	}
	if len(configs) == 0 {
		fmt.Println("No bots defined yet.")
		fmt.Println("Create one: magic bots create my-bot --title \"My Bot\" --prompt \"You are helpful.\"")
		return enableBotModeHint()
	}

	fmt.Printf("%-20s %-25s %-15s %s\n", "NAME", "TITLE", "MODEL", "@TAG")
	for _, c := range configs {
		model := c.Model
		if model == "" {
			model = "(inherit)"
		}
		fmt.Printf("%-20s %-25s %-15s @%s\n", c.Name, truncateStr(c.Title, 24), model, c.MentionTag())
	}
	return nil
}

func runBotsShow(cmd *cobra.Command, args []string) error {
	store, err := openBotStore()
	if err != nil {
		return err
	}
	cfg, err := store.Load(args[0])
	if err != nil {
		return fmt.Errorf("bot not found: %s", args[0])
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	fmt.Println(string(data))

	routines, _ := store.LoadRoutines(cfg.Name)
	if len(routines) > 0 {
		fmt.Println("\nRoutines:")
		for _, r := range routines {
			status := "disabled"
			if r.Enabled {
				status = "enabled"
			}
			last := "-"
			if r.LastRun != nil {
				last = time.Unix(*r.LastRun, 0).Format("2006-01-02 15:04")
			}
			fmt.Printf("  [%s] %-20s %-12s last: %s (%s)\n", status, r.Name, r.Schedule, last, r.ID)
		}
	}
	return nil
}

func runBotsChat(cmd *cobra.Command, args []string) error {
	botName := args[0]
	message := strings.Join(args[1:], " ")

	cfg, err := loadConfigForBots()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	mgr, err := bot.NewManager(cfg, nil)
	if err != nil {
		return err
	}
	if mgr == nil {
		return fmt.Errorf("bot manager unavailable (no bots defined?)")
	}
	if err := mgr.Start(context.Background()); err != nil {
		return err
	}
	defer mgr.Stop()

	reply, err := mgr.SendToBot(botName, message)
	if err != nil {
		return err
	}
	fmt.Println(reply)
	return nil
}

func runBotsMessage(cmd *cobra.Command, args []string) error {
	from, to, message := args[0], args[1], args[2]

	store, err := openBotStore()
	if err != nil {
		return err
	}
	fromCfg, err := store.Load(from)
	if err != nil {
		return fmt.Errorf("sender bot not found: %s", from)
	}
	toCfg, err := store.Load(to)
	if err != nil {
		return fmt.Errorf("target bot not found: %s", to)
	}

	cfg, err := loadConfigForBots()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	mgr, err := bot.NewManager(cfg, nil)
	if err != nil {
		return err
	}
	if mgr == nil {
		return fmt.Errorf("bot manager unavailable")
	}
	if err := mgr.Start(context.Background()); err != nil {
		return err
	}
	defer mgr.Stop()

	if err := mgr.SendMessageAgent(fromCfg.MentionTag(), toCfg.MentionTag(), message); err != nil {
		return err
	}
	fmt.Printf("📨 Delivered to @%s (fire-and-forget; reply lands in @%s's canonical chat when processed)\n",
		toCfg.MentionTag(), toCfg.MentionTag())
	return nil
}

func runBotsRemove(cmd *cobra.Command, args []string) error {
	store, err := openBotStore()
	if err != nil {
		return err
	}
	if err := store.Delete(args[0]); err != nil {
		return err
	}
	fmt.Printf("🗑️  Bot %q removed\n", args[0])
	return nil
}

func runBotsRoutineAdd(cmd *cobra.Command, args []string) error {
	botName, routineName := args[0], args[1]
	if botsFlagSchedule == "" || botsFlagPrompt == "" {
		return fmt.Errorf("--schedule and --prompt are required")
	}

	cfgLocal, err := config.Load()
	if err != nil && cfgLocal == nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	mgr, err := bot.NewManager(cfgLocal, nil)
	if err != nil {
		return err
	}

	routine := &bot.RoutineConfig{
		Name:     routineName,
		Schedule: botsFlagSchedule,
		Prompt:   botsFlagPrompt,
		Enabled:  true,
	}

	if mgr == nil {
		// Bots not running (gateway down): validate + persist only.
		store, err := openBotStore()
		if err != nil {
			return err
		}
		if _, err := store.Load(botName); err != nil {
			return fmt.Errorf("bot not found: %s", botName)
		}
		routine.ID = bot.NewRoutineID(botName)
		routines, _ := store.LoadRoutines(botName)
		routines = append(routines, routine)
		if err := store.SaveRoutines(botName, routines); err != nil {
			return err
		}
	} else {
		// Live path: start the manager so schedulers register, then add.
		if err := mgr.Start(context.Background()); err != nil {
			return fmt.Errorf("failed to start bot manager: %w", err)
		}
		defer mgr.Stop()
		if err := mgr.AddRoutine(botName, routine); err != nil {
			return err
		}
	}

	fmt.Printf("✅ Routine %q added to bot %q\n", RoutineJobLabel(botName, routineName), botName)
	fmt.Printf("   Schedule: %s\n", botsFlagSchedule)
	return nil
}

// runBotsRoutineToggle enables or disables a bot's routine. When the gateway
// is live it goes through the manager (which also re-registers the cron
// entry); otherwise it falls back to file-only persistence.
func runBotsRoutineToggle(botName, idOrName string, enable bool) error {
	cfgLocal, err := config.Load()
	if err != nil && cfgLocal == nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	mgr, _ := bot.NewManager(cfgLocal, nil)
	if mgr != nil {
		if _, err := mgr.UpdateRoutine(botName, idOrName, func(r *bot.RoutineConfig) {
			r.Enabled = enable
		}); err == nil {
			printRoutineToggled(enable, botName, idOrName)
			return nil
		} else if !strings.Contains(err.Error(), "not found") {
			return err
		}
	}

	// Fall back to file-only update (gateway down or manager unavailable).
	store, err := openBotStore()
	if err != nil {
		return err
	}
	if _, err := store.Load(botName); err != nil {
		return fmt.Errorf("bot not found: %s", botName)
	}
	routines, err := store.LoadRoutines(botName)
	if err != nil {
		return err
	}
	found := false
	for _, r := range routines {
		if r.ID == idOrName || strings.EqualFold(r.Name, idOrName) {
			r.Enabled = enable
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("routine not found: %s", idOrName)
	}
	if err := store.SaveRoutines(botName, routines); err != nil {
		return err
	}
	printRoutineToggled(enable, botName, idOrName)
	return nil
}

func printRoutineToggled(enable bool, botName, idOrName string) {
	action := "disabled"
	if enable {
		action = "enabled"
	}
	fmt.Printf("%s Routine %q %s for %q\n",
		map[bool]string{true: "✅", false: "⏸️"}[enable], idOrName, action, botName)
}

func runBotsRoutineRemove(cmd *cobra.Command, args []string) error {
	botName, idOrName := args[0], args[1]

	cfgLocal, err := config.Load()
	if err != nil && cfgLocal == nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	mgr, _ := bot.NewManager(cfgLocal, nil)

	if mgr != nil {
		if err := mgr.RemoveRoutine(botName, idOrName); err == nil {
			fmt.Printf("🗑️  Routine %q removed from %q\n", idOrName, botName)
			return nil
		}
	}

	// Fall back to file-only removal.
	store, err := openBotStore()
	if err != nil {
		return err
	}
	routines, err := store.LoadRoutines(botName)
	if err != nil {
		return err
	}
	var kept []*bot.RoutineConfig
	found := false
	for _, r := range routines {
		if r.ID == idOrName || strings.EqualFold(r.Name, idOrName) {
			found = true
			continue
		}
		kept = append(kept, r)
	}
	if !found {
		return fmt.Errorf("routine not found: %s", idOrName)
	}
	if err := store.SaveRoutines(botName, kept); err != nil {
		return err
	}
	fmt.Printf("🗑️  Routine %q removed from %q\n", idOrName, botName)
	return nil
}

func runBotsRoutineList(cmd *cobra.Command, args []string) error {
	store, err := openBotStore()
	if err != nil {
		return err
	}
	routines, err := store.LoadRoutines(args[0])
	if err != nil {
		return err
	}
	if len(routines) == 0 {
		fmt.Printf("No routines for bot %q.\n", args[0])
		return nil
	}
	for _, r := range routines {
		status := "disabled"
		if r.Enabled {
			status = "enabled "
		}
		fmt.Printf("[%s] %-22s %-14s %s\n", status, r.Name, r.Schedule, r.ID)
	}
	return nil
}

// --- helpers ---

func storePathHint(name string) string {
	return fmt.Sprintf("~/.magic/bots/%s.json", name)
}

func RoutineJobLabel(botName, routineName string) string {
	return fmt.Sprintf("[bot:%s] %s", botName, routineName)
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// enableBotModeHint prints guidance when bots exist but bot_mode is off.
func enableBotModeHint() error {
	cfg, err := config.Load()
	if err == nil && cfg.BotMode != nil && cfg.BotMode.Enabled {
		return nil
	}
	fmt.Println("\n⚠️  Bot Mode is currently disabled — bots will only run inside the gateway once enabled:")
	fmt.Println("   magic config set bot_mode.enabled true")
	return nil
}

// loadConfigForBots loads config, ensuring Bot Mode is enabled so a Manager
// can be constructed for one-shot CLI commands (chat/message).
func loadConfigForBots() (*config.Config, error) {
	cfg, err := config.Load()
	if err != nil && cfg == nil {
		return nil, err
	}
	if cfg.BotMode == nil {
		cfg.BotMode = config.DefaultBotModeConfig()
	}
	if !cfg.BotMode.Enabled {
		cfg.BotMode.Enabled = true
		_ = cfg.Save() // Best-effort persist; in-memory override is enough for one-shot runs
	}
	return cfg, nil
}
