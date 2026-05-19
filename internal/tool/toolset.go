package tool

// ToolsetDefinition defines a named group of tools
type ToolsetDefinition struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tools       []string `json:"tools"`
	Includes    []string `json:"includes"` // Include other toolsets
}

// Toolset registry for managing tool groups
var ToolsetRegistry = make(map[string]*ToolsetDefinition)

// RegisterToolset registers a toolset
func RegisterToolset(ts *ToolsetDefinition) {
	ToolsetRegistry[ts.Name] = ts
}

// GetToolset returns a toolset by name
func GetToolset(name string) *ToolsetDefinition {
	return ToolsetRegistry[name]
}

// ResolveToolset resolves a toolset and all its includes to get all tool names
func ResolveToolset(name string, visited map[string]bool) []string {
	if visited == nil {
		visited = make(map[string]bool)
	}

	// Handle "all" special case
	if name == "all" || name == "*" {
		var allTools []string
		for _, tsName := range GetToolsetNames() {
			allTools = append(allTools, ResolveToolset(tsName, make(map[string]bool))...)
		}
		return deduplicate(allTools)
	}

	// Cycle detection
	if visited[name] {
		return nil
	}
	visited[name] = true

	ts := GetToolset(name)
	if ts == nil {
		return nil
	}

	tools := make([]string, 0, len(ts.Tools))
	tools = append(tools, ts.Tools...)

	// Resolve included toolsets
	for _, included := range ts.Includes {
		tools = append(tools, ResolveToolset(included, visited)...)
	}

	return deduplicate(tools)
}

// GetToolsetNames returns all registered toolset names
func GetToolsetNames() []string {
	names := make([]string, 0, len(ToolsetRegistry))
	for name := range ToolsetRegistry {
		names = append(names, name)
	}
	return names
}

// GetAllToolsets returns all toolsets
func GetAllToolsets() map[string]*ToolsetDefinition {
	return ToolsetRegistry
}

// deduplicate removes duplicate strings from a slice
func deduplicate(slice []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(slice))
	for _, s := range slice {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

// GetEnabledTools returns tools based on enabled toolsets
func GetEnabledTools(registry *Registry, enabledToolsets []string) []Tool {
	if len(enabledToolsets) == 0 {
		// Return all tools if no toolsets specified
		return getAllRegisteredTools(registry)
	}

	// Collect all tool names from enabled toolsets
	var enabledToolNames []string
	for _, tsName := range enabledToolsets {
		tools := ResolveToolset(tsName, nil)
		enabledToolNames = append(enabledToolNames, tools...)
	}
	enabledToolNames = deduplicate(enabledToolNames)

	// Get Tool instances for enabled names
	tools := make([]Tool, 0, len(enabledToolNames))
	for _, name := range enabledToolNames {
		tool, err := registry.Get(name)
		if err == nil {
			tools = append(tools, tool)
		}
	}

	return tools
}

// getAllRegisteredTools returns all tools from the registry
func getAllRegisteredTools(registry *Registry) []Tool {
	names := registry.List()
	tools := make([]Tool, 0, len(names))
	for _, name := range names {
		tool, _ := registry.Get(name)
		if tool != nil {
			tools = append(tools, tool)
		}
	}
	return tools
}

// InitializeDefaultToolsets registers all default toolsets
func InitializeDefaultToolsets() {
	// Core web tools
	RegisterToolset(&ToolsetDefinition{
		Name:        "web",
		Description: "Web research and content extraction tools",
		Tools:       []string{"web_search", "web_extract"},
		Includes:    []string{},
	})

	// File manipulation tools
	RegisterToolset(&ToolsetDefinition{
		Name:        "file",
		Description: "File manipulation tools: read, write, edit, search",
		Tools:       []string{"read_file", "write_file", "file_edit", "list_files", "search_in_files", "directory_tree"},
		Includes:    []string{},
	})

	// Terminal execution (local, docker, ssh backends)
	RegisterToolset(&ToolsetDefinition{
		Name:        "terminal",
		Description: "Terminal/command execution with multiple backends (local, docker, ssh)",
		Tools:       []string{"execute_command", "terminal", "process"},
		Includes:    []string{},
	})

	// Browser automation
	RegisterToolset(&ToolsetDefinition{
		Name:        "browser",
		Description: "Browser automation for web interaction (navigate, click, type, scroll, snapshot, vision)",
		Tools: []string{
			"browser_navigate", "browser_snapshot", "browser_click",
			"browser_type", "browser_scroll", "browser_back",
			"browser_get_images", "browser_console",
			"web_fetch", "web_select", "web_search",
		},
		Includes:    []string{},
	})

	// Vision and image
	RegisterToolset(&ToolsetDefinition{
		Name:        "vision",
		Description: "Image analysis and generation tools",
		Tools:       []string{"image_gen", "image_edit", "video_analyze"},
		Includes:    []string{},
	})

	// TTS
	RegisterToolset(&ToolsetDefinition{
		Name:        "tts",
		Description: "Text-to-speech tools",
		Tools:       []string{"text_to_speech"},
		Includes:    []string{},
	})

	// Planning and memory
	RegisterToolset(&ToolsetDefinition{
		Name:        "todo",
		Description: "Task planning and tracking",
		Tools:       []string{"todo"},
		Includes:    []string{},
	})

	RegisterToolset(&ToolsetDefinition{
		Name:        "memory",
		Description: "Persistent memory across sessions",
		Tools:       []string{"memory_store", "memory_recall"},
		Includes:    []string{},
	})

	// Session search
	RegisterToolset(&ToolsetDefinition{
		Name:        "session_search",
		Description: "Search and recall past conversations",
		Tools:       []string{"session_search"},
		Includes:    []string{},
	})

	// Clarification
	RegisterToolset(&ToolsetDefinition{
		Name:        "clarify",
		Description: "Ask clarifying questions",
		Tools:       []string{"clarify"},
		Includes:    []string{},
	})

	// Cron jobs
	RegisterToolset(&ToolsetDefinition{
		Name:        "cronjob",
		Description: "Cronjob management for scheduled tasks",
		Tools:       []string{"cronjob"},
		Includes:    []string{},
	})

	// Skills
	RegisterToolset(&ToolsetDefinition{
		Name:        "skills",
		Description: "Skill management tools",
		Tools:       []string{"skill_invoke", "skill_list", "skill_info"},
		Includes:    []string{},
	})

	// MCP toolsets (dynamically added per server)
	// These are added when MCP servers connect

	// Utility tools
	RegisterToolset(&ToolsetDefinition{
		Name:        "utility",
		Description: "Utility tools: JSON, YAML, string manipulation",
		Tools:       []string{"json", "yaml", "string", "hash", "uuid", "random", "time", "math", "csv", "env", "system_info"},
		Includes:    []string{},
	})

	// Kanban tools
	RegisterToolset(&ToolsetDefinition{
		Name:        "kanban",
		Description: "Kanban task management",
		Tools:       []string{"kanban_show", "kanban_complete", "kanban_block", "kanban_heartbeat", "kanban_comment", "kanban_create", "kanban_link"},
		Includes:    []string{},
	})

	// Plugin tools
	RegisterToolset(&ToolsetDefinition{
		Name:        "plugins",
		Description: "Plugin execution tools",
		Tools:       []string{"plugin_exec", "plugin_tool"},
		Includes:    []string{},
	})

	// =============================================================================
	// Full platform toolsets
	// =============================================================================

	// Full interactive CLI toolset
	RegisterToolset(&ToolsetDefinition{
		Name:        "magic-cli",
		Description: "Full interactive CLI toolset - all default tools",
		Tools:       []string{},
		Includes: []string{
			"web", "file", "terminal", "browser", "vision", "tts",
			"todo", "memory", "session_search", "clarify", "cronjob",
			"skills", "utility", "kanban", "plugins",
		},
	})

	// Safe: No terminal access
	RegisterToolset(&ToolsetDefinition{
		Name:        "safe",
		Description: "Safe toolkit without terminal access",
		Tools:       []string{},
		Includes: []string{
			"web", "file", "browser", "vision", "tts",
			"todo", "memory", "session_search", "clarify",
			"skills", "utility",
		},
	})

	// Research: Web research focused
	RegisterToolset(&ToolsetDefinition{
		Name:        "research",
		Description: "Research and analysis toolkit",
		Tools:       []string{},
		Includes: []string{
			"web", "browser", "session_search", "memory",
		},
	})

	// Development: Coding focused
	RegisterToolset(&ToolsetDefinition{
		Name:        "development",
		Description: "Software development toolkit",
		Tools:       []string{},
		Includes: []string{
			"file", "terminal", "todo", "session_search",
		},
	})

	// Minimal: Essential tools only
	RegisterToolset(&ToolsetDefinition{
		Name:        "minimal",
		Description: "Minimal toolset with essential tools only",
		Tools:       []string{"web_search", "read_file", "write_file", "todo"},
		Includes:    []string{},
	})

	// All: Everything
	RegisterToolset(&ToolsetDefinition{
		Name:        "all",
		Description: "All available tools",
		Tools:       []string{},
		Includes: []string{
			"magic-cli",
		},
	})
}

// RegisterMCPServerToolset registers a toolset for an MCP server
func RegisterMCPServerToolset(serverName string, toolNames []string) {
	toolsetName := "mcp-" + serverName
	RegisterToolset(&ToolsetDefinition{
		Name:        toolsetName,
		Description: "MCP server " + serverName + " tools",
		Tools:       toolNames,
		Includes:    []string{},
	})
}

// UnregisterMCPServerToolset removes an MCP server toolset
func UnregisterMCPServerToolset(serverName string) {
	toolsetName := "mcp-" + serverName
	delete(ToolsetRegistry, toolsetName)
}
