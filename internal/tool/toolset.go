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

// InitializeDefaultToolsets 注册所有默认工具集。
//
// 单工具分组直接从 ToolCategories（tool_categories.go，唯一事实来源）生成，
// 不再手抄工具名 —— 旧版手抄清单与实际注册名漂移过三次（skill_invoke、
// text_to_speech 都是不存在的名字）。组合预设（magic-cli/safe/...）引用分组 id。
func InitializeDefaultToolsets() {
	for _, cat := range ToolCategories() {
		RegisterToolset(&ToolsetDefinition{
			Name:        cat.ID,
			Description: cat.Description,
			Tools:       cat.Tools,
			Includes:    []string{},
		})
	}

	// =============================================================================
	// 组合预设（引用上面的分组 id，不列具体工具）
	// =============================================================================

	// Full interactive CLI toolset
	RegisterToolset(&ToolsetDefinition{
		Name:        "magic-cli",
		Description: "Full interactive CLI toolset - all default tools",
		Tools:       []string{},
		Includes: []string{
			"file", "web", "browser", "terminal", "coding", "media", "desktop",
			"memory", "task", "kanban", "skills", "interaction", "utility",
		},
	})

	// Safe: No terminal / desktop / code execution access
	RegisterToolset(&ToolsetDefinition{
		Name:        "safe",
		Description: "Safe toolkit without terminal, code execution or desktop control",
		Tools:       []string{},
		Includes: []string{
			"file", "web", "browser", "media",
			"memory", "task", "skills", "interaction", "utility",
		},
	})

	// Research: Web research focused
	RegisterToolset(&ToolsetDefinition{
		Name:        "research",
		Description: "Research and analysis toolkit",
		Tools:       []string{},
		Includes: []string{
			"web", "browser", "memory",
		},
	})

	// Development: Coding focused
	RegisterToolset(&ToolsetDefinition{
		Name:        "development",
		Description: "Software development toolkit",
		Tools:       []string{},
		Includes: []string{
			"file", "terminal", "coding", "web", "task", "memory", "skills",
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
