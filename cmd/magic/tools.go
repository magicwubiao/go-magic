package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// toolsCmd represents the tools command
var toolsCmd = &cobra.Command{
	Use:   "tools",
	Short: "Tool and toolset management",
	Long:  `Manage available tools and toolsets.`,
}

var toolsListAll bool

func init() {
	toolsListCmd.Flags().BoolVar(&toolsListAll, "json", false, "Output in JSON format")
	toolsCmd.AddCommand(toolsListCmd)
	rootCmd.AddCommand(toolsCmd)

	// Toolset commands
	toolsetsCmd.AddCommand(toolsetsListCmd)
	toolsetsCmd.AddCommand(toolsetsShowCmd)
	toolsetsCmd.AddCommand(toolsetsEnableCmd)
	toolsetsCmd.AddCommand(toolsetsDisableCmd)
	toolsCmd.AddCommand(toolsetsCmd)
}

var toolsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all available tools",
	Long:  `List all available tools with their descriptions.`,
	Run:   runToolsList,
}

var toolsetsCmd = &cobra.Command{
	Use:   "toolsets",
	Short: "Toolset management",
}

var toolsetsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all available toolsets",
	Run:   runToolsetsList,
}

var toolsetsShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show details of a specific toolset",
	Args:  cobra.ExactArgs(1),
	Run:   runToolsetsShow,
}

var toolsetsEnableCmd = &cobra.Command{
	Use:   "enable <name>",
	Short: "Enable a toolset",
	Args:  cobra.ExactArgs(1),
	Run:   runToolsetsEnable,
}

var toolsetsDisableCmd = &cobra.Command{
	Use:   "disable <name>",
	Short: "Disable a toolset",
	Args:  cobra.ExactArgs(1),
	Run:   runToolsetsDisable,
}

func runToolsList(cmd *cobra.Command, args []string) {
	tools := getAllTools()

	if toolsListAll {
		// JSON output
		data, _ := json.MarshalIndent(map[string]interface{}{
			"tools": tools,
			"count": len(tools),
		}, "", "  ")
		fmt.Println(string(data))
	} else {
		// Formatted output
		fmt.Printf("Available tools (%d total):\n\n", len(tools))

		// Group by category
		byCategory := make(map[string][]string)
		for _, t := range tools {
			parts := strings.SplitN(t, "_", 2)
			cat := "other"
			if len(parts) > 1 {
				cat = parts[0]
			}
			byCategory[cat] = append(byCategory[cat], t)
		}

		for _, cat := range []string{"web", "browser", "terminal", "file", "code_execution", "skills", "memory", "delegation", "homeassistant", "cron", "utility", "mcp", "other"} {
			if tools, ok := byCategory[cat]; ok && len(tools) > 0 {
				fmt.Printf("### %s\n", cat)
				for _, toolName := range tools {
					desc := getToolDescription(toolName)
					fmt.Printf("  %-30s %s\n", toolName, desc)
				}
				fmt.Println()
			}
		}
	}
}

func getAllTools() []string {
	return []string{
		// Web tools
		"web_search",
		"web_extract",
		// Browser tools
		"browser_navigate",
		"browser_snapshot",
		"browser_click",
		"browser_type",
		"browser_scroll",
		"browser_back",
		"browser_get_images",
		"browser_console",
		// Terminal tools
		"execute_command",
		"terminal",
		"process",
		// File tools
		"read_file",
		"write_file",
		"file_edit",
		"list_files",
		"search_in_files",
		// Code execution
		"execute_code",
		// Skills
		"skill_list",
		"skill_view",
		"skill_manage",
		"skill_create",
		"skill_delete",
		// Memory
		"memory_store",
		"memory_recall",
		// Session
		"session_search",
		// Delegation
		"delegate_task",
		"poll_task",
		"list_tasks",
		"cancel_task",
		// Home Assistant
		"ha_list_entities",
		"ha_get_state",
		"ha_list_services",
		"ha_call_service",
		"ha_events",
		"ha_config",
		// Cron
		"cronjob",
		// Utility
		"json",
		"yaml",
		"string",
		"hash",
		"uuid",
		"random",
		"time",
		"math",
		"csv",
		"env",
		"system_info",
	}
}

func getToolDescription(name string) string {
	descriptions := map[string]string{
		"web_search":         "Search the web",
		"web_extract":        "Extract content from web pages",
		"browser_navigate":   "Navigate to a URL",
		"browser_snapshot":   "Get page snapshot",
		"browser_click":      "Click page element",
		"browser_type":       "Type into input",
		"browser_scroll":     "Scroll page",
		"browser_back":       "Go back",
		"browser_get_images": "Extract image URLs",
		"browser_console":    "Get console messages",
		"execute_command":    "Execute shell command",
		"terminal":           "Terminal operations",
		"process":            "Process management",
		"read_file":          "Read file contents",
		"write_file":         "Write file contents",
		"file_edit":          "Edit file",
		"list_files":         "List directory",
		"search_in_files":    "Search in files",
		"execute_code":       "Execute Python code",
		"skill_list":         "List skills",
		"skill_view":         "View skill details",
		"skill_manage":       "Manage skills",
		"skill_create":       "Create skill",
		"skill_delete":       "Delete skill",
		"memory_store":       "Store to memory",
		"memory_recall":      "Recall from memory",
		"session_search":     "Search sessions",
		"delegate_task":      "Delegate to sub-agent",
		"poll_task":          "Poll task status",
		"list_tasks":         "List tasks",
		"cancel_task":        "Cancel task",
		"ha_list_entities":   "List HA entities",
		"ha_get_state":       "Get HA entity state",
		"ha_list_services":   "List HA services",
		"ha_call_service":    "Call HA service",
		"ha_events":          "HA events",
		"ha_config":          "HA config",
		"cronjob":            "Schedule task",
		"json":               "JSON utilities",
		"yaml":               "YAML utilities",
		"string":             "String utilities",
		"hash":               "Hash utilities",
		"uuid":               "UUID generation",
		"random":             "Random values",
		"time":               "Time utilities",
		"math":               "Math utilities",
		"csv":                "CSV utilities",
		"env":                "Environment variables",
		"system_info":        "System information",
	}
	if desc, ok := descriptions[name]; ok {
		return desc
	}
	return "Tool description"
}

var toolsetDefinitions = map[string]struct {
	Description string
	Tools       []string
	Tags        []string
}{
	"web": {
		Description: "Web search and content extraction",
		Tools:       []string{"web_search", "web_extract"},
		Tags:        []string{"search", "web"},
	},
	"browser": {
		Description: "Browser automation",
		Tools:       []string{"browser_navigate", "browser_snapshot", "browser_click", "browser_type", "browser_scroll", "browser_back", "browser_get_images", "browser_console"},
		Tags:        []string{"browser", "automation"},
	},
	"terminal": {
		Description: "Terminal command execution",
		Tools:       []string{"execute_command", "terminal", "process"},
		Tags:        []string{"shell", "system"},
	},
	"file": {
		Description: "File read/write/edit operations",
		Tools:       []string{"read_file", "write_file", "file_edit", "list_files", "search_in_files"},
		Tags:        []string{"filesystem", "io"},
	},
	"code_execution": {
		Description: "Python code execution",
		Tools:       []string{"execute_code"},
		Tags:        []string{"code", "python"},
	},
	"skills": {
		Description: "Skill management",
		Tools:       []string{"skill_list", "skill_view", "skill_manage", "skill_create", "skill_delete"},
		Tags:        []string{"skills", "management"},
	},
	"memory": {
		Description: "Persistent memory",
		Tools:       []string{"memory_store", "memory_recall"},
		Tags:        []string{"memory", "storage"},
	},
	"delegation": {
		Description: "Sub-agent delegation",
		Tools:       []string{"delegate_task", "poll_task", "list_tasks", "cancel_task"},
		Tags:        []string{"delegation", "agent"},
	},
	"homeassistant": {
		Description: "Smart home control",
		Tools:       []string{"ha_list_entities", "ha_get_state", "ha_list_services", "ha_call_service", "ha_events", "ha_config"},
		Tags:        []string{"homeassistant", "iot"},
	},
	"cron": {
		Description: "Scheduled tasks",
		Tools:       []string{"cronjob"},
		Tags:        []string{"scheduler", "tasks"},
	},
	"utility": {
		Description: "Utility functions",
		Tools:       []string{"json", "yaml", "string", "hash", "uuid", "random", "time", "math", "csv", "env", "system_info"},
		Tags:        []string{"utility", "helper"},
	},
	"session": {
		Description: "Session management",
		Tools:       []string{"session_search"},
		Tags:        []string{"session", "chat"},
	},
}

func runToolsetsList(cmd *cobra.Command, args []string) {
	fmt.Println("Available toolsets:")
	fmt.Println()

	for name, ts := range toolsetDefinitions {
		fmt.Printf("## %s\n", name)
		fmt.Printf("  %s\n", ts.Description)
		fmt.Printf("  Tools: %d\n", len(ts.Tools))
		if len(ts.Tools) <= 6 {
			fmt.Printf("  %s\n", strings.Join(ts.Tools, ", "))
		} else {
			fmt.Printf("  %s\n", strings.Join(ts.Tools[:6], ", ")+"...")
		}
		fmt.Printf("  Tags: %s\n", strings.Join(ts.Tags, ", "))
		fmt.Println()
	}
}

func runToolsetsShow(cmd *cobra.Command, args []string) {
	name := args[0]
	ts, ok := toolsetDefinitions[name]
	if !ok {
		fmt.Printf("Toolset '%s' not found.\n", name)
		os.Exit(1)
	}

	fmt.Printf("## %s\n", name)
	fmt.Printf("  %s\n\n", ts.Description)
	fmt.Printf("Tools (%d):\n", len(ts.Tools))
	for _, t := range ts.Tools {
		desc := getToolDescription(t)
		fmt.Printf("  • %s - %s\n", t, desc)
	}
	fmt.Println()
	fmt.Printf("Tags: %s\n", strings.Join(ts.Tags, ", "))
}

func runToolsetsEnable(cmd *cobra.Command, args []string) {
	name := args[0]
	if _, ok := toolsetDefinitions[name]; !ok {
		fmt.Printf("Toolset '%s' not found.\n", name)
		fmt.Println("\nAvailable toolsets:")
		for n := range toolsetDefinitions {
			fmt.Printf("  • %s\n", n)
		}
		os.Exit(1)
	}
	fmt.Printf("Toolset '%s' enabled.\n", name)
	fmt.Println("(Run 'magic config' to update your configuration)")
}

func runToolsetsDisable(cmd *cobra.Command, args []string) {
	name := args[0]
	if _, ok := toolsetDefinitions[name]; !ok {
		fmt.Printf("Toolset '%s' not found.\n", name)
		os.Exit(1)
	}
	fmt.Printf("Toolset '%s' disabled.\n", name)
	fmt.Println("(Run 'magic config' to update your configuration)")
}
