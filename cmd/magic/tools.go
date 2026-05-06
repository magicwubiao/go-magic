package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/pkg/config"
)

var toolsShowSchema bool
var toolsFilterPrefix string

var toolsCmd = &cobra.Command{
	Use:   "tools",
	Short: "Configure and manage tools",
	Long: `Configure which tools are enabled and view tool information.

Tools are organized by category:
  - File operations: read_file, write_file, file_edit, list_files, directory_tree, search_in_files
  - Web tools: web_search, web_extract
  - Code execution: python_execute, node_execute, execute_command
  - Memory tools: memory_store, memory_recall
  - Task management: todo
  - AI capabilities: clarify, vision_analyze, image_gen, tts
  - Skills: skill

Examples:
  magic tools list
  magic tools list --prefix file
  magic tools show read_file
  magic tools show read_file --schema
  magic tools enable browser_navigate
  magic tools disable web_search`,
}

var toolsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all available tools",
	Run:   runToolsList,
}

var toolsShowCmd = &cobra.Command{
	Use:   "show <tool>",
	Short: "Show details about a tool",
	Args:  cobra.ExactArgs(1),
	Run:   runToolsShow,
}

var toolsEnableCmd = &cobra.Command{
	Use:   "enable <tool>",
	Short: "Enable a tool",
	Args:  cobra.ExactArgs(1),
	Run:   runToolsEnable,
}

var toolsDisableCmd = &cobra.Command{
	Use:   "disable <tool>",
	Short: "Disable a tool",
	Args:  cobra.ExactArgs(1),
	Run:   runToolsDisable,
}

var toolsSearchCmd = &cobra.Command{
	Use:   "search <keyword>",
	Short: "Search tools by keyword",
	Args:  cobra.ExactArgs(1),
	Run:   runToolsSearch,
}

var toolsSchemaCmd = &cobra.Command{
	Use:   "schema [tool]",
	Short: "Show tool schemas in OpenAI format",
	Run:   runToolsSchema,
}

func init() {
	toolsCmd.AddCommand(toolsListCmd)
	toolsCmd.AddCommand(toolsShowCmd)
	toolsCmd.AddCommand(toolsEnableCmd)
	toolsCmd.AddCommand(toolsDisableCmd)
	toolsCmd.AddCommand(toolsSearchCmd)
	toolsCmd.AddCommand(toolsSchemaCmd)

	toolsListCmd.Flags().StringVar(&toolsFilterPrefix, "prefix", "", "Filter tools by prefix")
	toolsShowCmd.Flags().BoolVar(&toolsShowSchema, "schema", false, "Show JSON schema")

	rootCmd.AddCommand(toolsCmd)
}

func runToolsList(cmd *cobra.Command, args []string) {
	registry := tool.NewRegistry()
	registry.RegisterAll()

	tools := registry.List()
	if len(tools) == 0 {
		fmt.Println("No tools registered.")
		return
	}

	// Sort tools
	sort.Strings(tools)

	// Filter by prefix if specified
	if toolsFilterPrefix != "" {
		var filtered []string
		for _, t := range tools {
			if strings.HasPrefix(t, toolsFilterPrefix) {
				filtered = append(filtered, t)
			}
		}
		tools = filtered
	}

	fmt.Printf("Available tools (%d total):\n\n", len(tools))

	// Group by category
	byCategory := categorizeTools(tools)

	for category, list := range byCategory {
		fmt.Printf("## %s\n", strings.ToUpper(category))
		for _, name := range list {
			t, _ := registry.Get(name)
			if t != nil {
				desc := t.Description()
				if len(desc) > 60 {
					desc = desc[:57] + "..."
				}
				fmt.Printf("  • %-20s %s\n", name, desc)
			}
		}
		fmt.Println()
	}
}

func categorizeTools(tools []string) map[string][]string {
	categories := map[string][]string{
		"file":   {},
		"web":    {},
		"code":   {},
		"memory": {},
		"ai":     {},
		"task":   {},
		"system": {},
		"other":  {},
	}

	for _, t := range tools {
		switch {
		case strings.HasPrefix(t, "read_file") || strings.HasPrefix(t, "write_file") ||
			strings.HasPrefix(t, "file_") || strings.HasPrefix(t, "list_files") ||
			strings.HasPrefix(t, "directory_tree") || strings.HasPrefix(t, "search_in_files"):
			categories["file"] = append(categories["file"], t)
		case strings.HasPrefix(t, "web_"):
			categories["web"] = append(categories["web"], t)
		case strings.HasPrefix(t, "python_") || strings.HasPrefix(t, "node_") ||
			strings.HasPrefix(t, "execute_"):
			categories["code"] = append(categories["code"], t)
		case strings.HasPrefix(t, "memory_"):
			categories["memory"] = append(categories["memory"], t)
		case strings.HasPrefix(t, "clarify") || strings.HasPrefix(t, "vision_") ||
			strings.HasPrefix(t, "image_") || strings.HasPrefix(t, "tts"):
			categories["ai"] = append(categories["ai"], t)
		case t == "todo":
			categories["task"] = append(categories["task"], t)
		case t == "skill":
			categories["system"] = append(categories["system"], t)
		default:
			categories["other"] = append(categories["other"], t)
		}
	}

	return categories
}

func runToolsShow(cmd *cobra.Command, args []string) {
	toolName := args[0]

	registry := tool.NewRegistry()
	registry.RegisterAll()

	t, err := registry.Get(toolName)
	if err != nil {
		fmt.Printf("Tool '%s' not found.\n", toolName)
		os.Exit(1)
	}

	fmt.Printf("Name:        %s\n", t.Name())
	fmt.Printf("Description: %s\n", t.Description())

	// Show timeout if set
	timeout := registry.GetTimeout(toolName)
	if timeout > 0 {
		fmt.Printf("Timeout:     %v\n", timeout)
	}

	if toolsShowSchema {
		fmt.Println("\n--- Schema ---")
		schema := t.Schema()
		data, err := json.MarshalIndent(schema, "", "  ")
		if err != nil {
			fmt.Printf("Error marshaling schema: %v\n", err)
		} else {
			fmt.Println(string(data))
		}
	} else {
		// Show parameters
		schema := t.Schema()
		if props, ok := schema["properties"].(map[string]interface{}); ok {
			fmt.Println("\n--- Parameters ---")
			for name, prop := range props {
				propMap, _ := prop.(map[string]interface{})
				desc, _ := propMap["description"].(string)
				typ, _ := propMap["type"].(string)

				required, _ := schema["required"].([]interface{})
				isRequired := false
				for _, r := range required {
					if r == name {
						isRequired = true
						break
					}
				}

				reqMark := ""
				if isRequired {
					reqMark = " (required)"
				}

				fmt.Printf("  %s%s: %s - %s\n", name, reqMark, typ, desc)
			}
		}
	}
}

func runToolsSearch(cmd *cobra.Command, args []string) {
	keyword := args[0]

	registry := tool.NewRegistry()
	registry.RegisterAll()

	matched := registry.FilterToolsByKeyword(keyword)
	if len(matched) == 0 {
		fmt.Printf("No tools found matching '%s'\n", keyword)
		return
	}

	fmt.Printf("Found %d tools matching '%s':\n\n", len(matched), keyword)

	ts := &tool.ToolSchema{}
	for _, t := range matched {
		schema := ts.ToOpenAISchema(t)
		fmt.Printf("  • %s: %s\n", t.Name(), t.Description())
	}
}

func runToolsEnable(cmd *cobra.Command, args []string) {
	toolName := args[0]

	// Verify tool exists
	registry := tool.NewRegistry()
	registry.RegisterAll()

	if !registry.HasTool(toolName) {
		fmt.Printf("Tool '%s' does not exist.\n", toolName)
		fmt.Println("Use 'magic tools list' to see available tools.")
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Add to enabled list
	cfg.Tools.Enabled = append(cfg.Tools.Enabled, toolName)

	// Remove from disabled list
	newDisabled := []string{}
	for _, d := range cfg.Tools.Disabled {
		if d != toolName {
			newDisabled = append(newDisabled, d)
		}
	}
	cfg.Tools.Disabled = newDisabled

	if err := cfg.Save(); err != nil {
		fmt.Printf("Failed to save config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Tool '%s' enabled\n", toolName)
}

func runToolsDisable(cmd *cobra.Command, args []string) {
	toolName := args[0]

	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	cfg.Tools.Disabled = append(cfg.Tools.Disabled, toolName)

	// Remove from enabled list
	newEnabled := []string{}
	for _, e := range cfg.Tools.Enabled {
		if e != toolName {
			newEnabled = append(newEnabled, e)
		}
	}
	cfg.Tools.Enabled = newEnabled

	if err := cfg.Save(); err != nil {
		fmt.Printf("Failed to save config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Tool '%s' disabled\n", toolName)
}

func runToolsSchema(cmd *cobra.Command, args []string) {
	registry := tool.NewRegistry()
	registry.RegisterAll()

	ts := &tool.ToolSchema{}

	if len(args) > 0 {
		// Show specific tool schema
		toolName := args[0]
		t, err := registry.Get(toolName)
		if err != nil {
			fmt.Printf("Tool '%s' not found.\n", toolName)
			os.Exit(1)
		}

		schema := ts.ToOpenAISchema(t)
		data, _ := json.MarshalIndent(schema, "", "  ")
		fmt.Println(string(data))
	} else {
		// Show all schemas
		tools := registry.ListWithSchemas()
		data, _ := json.MarshalIndent(tools, "", "  ")
		fmt.Println(string(data))
	}
}

// ToolInfo represents tool information for display
type ToolInfo struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Schema      map[string]interface{} `json:"schema"`
	Timeout     time.Duration          `json:"timeout,omitempty"`
}
