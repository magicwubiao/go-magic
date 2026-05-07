package migrate

import (
	"fmt"
	"strings"
)

// ToolConverter handles conversion of tools between formats
type ToolConverter struct{}

// NewToolConverter creates a new tool converter
func NewToolConverter() *ToolConverter {
	return &ToolConverter{}
}

// ToolMapping maps OpenClaw tool names to Hermes tool names
var ToolMapping = map[string]ToolConversion{
	"bash": {
		HermesName: "bash",
		Convert:    convertBashTool,
	},
	"shell": {
		HermesName: "bash",
		Convert:    convertBashTool,
	},
	"read_file": {
		HermesName: "read_file",
		Convert:    convertReadFileTool,
	},
	"write_file": {
		HermesName: "write_file",
		Convert:    convertWriteFileTool,
	},
	"edit_file": {
		HermesName: "edit_file",
		Convert:    convertEditFileTool,
	},
	"glob": {
		HermesName: "glob",
		Convert:    convertGlobTool,
	},
	"grep": {
		HermesName: "grep",
		Convert:    convertGrepTool,
	},
	"search": {
		HermesName: "search",
		Convert:    convertSearchTool,
	},
	"fetch": {
		HermesName: "fetch",
		Convert:    convertFetchTool,
	},
	"browser": {
		HermesName: "browser",
		Convert:    convertBrowserTool,
	},
	"code_interpreter": {
		HermesName: "code_interpreter",
		Convert:    convertCodeInterpreterTool,
	},
	"python": {
		HermesName: "code_interpreter",
		Convert:    convertCodeInterpreterTool,
	},
	"memory": {
		HermesName: "memory",
		Convert:    convertMemoryTool,
	},
	"search_memory": {
		HermesName: "memory_search",
		Convert:    convertMemorySearchTool,
	},
}

// ToolConversion represents a tool conversion mapping
type ToolConversion struct {
	HermesName string
	Convert    func(srcTool map[string]interface{}) (map[string]interface{}, error)
}

// ConvertTools converts OpenClaw tools to Hermes format
func (c *ToolConverter) ConvertTools(openclawTools []string) ([]string, []string, error) {
	var hermesTools []string
	var warnings []string

	for _, tool := range openclawTools {
		tool = strings.TrimSpace(tool)

		if mapping, ok := ToolMapping[tool]; ok {
			hermesTools = append(hermesTools, mapping.HermesName)
		} else if strings.HasPrefix(tool, "http") || strings.HasPrefix(tool, "web_") {
			// Web tools -> fetch
			hermesTools = append(hermesTools, "fetch")
		} else if strings.HasPrefix(tool, "file_") || strings.HasPrefix(tool, "io_") {
			// File tools -> read/write
			if strings.Contains(tool, "write") {
				hermesTools = append(hermesTools, "write_file")
			} else {
				hermesTools = append(hermesTools, "read_file")
			}
		} else {
			// Unknown tool - preserve as-is with warning
			hermesTools = append(hermesTools, tool)
			warnings = append(warnings, fmt.Sprintf("Unknown tool '%s' preserved (may need manual verification)", tool))
		}
	}

	// Remove duplicates
	hermesTools = uniqueStrings(hermesTools)

	return hermesTools, warnings, nil
}

// ConvertToolSchema converts OpenClaw tool schema to Hermes format
func (c *ToolConverter) ConvertToolSchema(srcTool map[string]interface{}) (map[string]interface{}, error) {
	toolType, ok := srcTool["type"].(string)
	if !ok {
		return nil, fmt.Errorf("missing tool type")
	}

	if mapping, ok := ToolMapping[toolType]; ok {
		return mapping.Convert(srcTool)
	}

	// Unknown tool type - convert generically
	return c.convertGenericTool(srcTool)
}

// convertGenericTool converts an unknown tool generically
func (c *ToolConverter) convertGenericTool(srcTool map[string]interface{}) (map[string]interface{}, error) {
	hermesTool := make(map[string]interface{})

	// Map common fields
	if name, ok := srcTool["name"].(string); ok {
		hermesTool["name"] = name
	}
	if desc, ok := srcTool["description"].(string); ok {
		hermesTool["description"] = desc
	}

	// Convert parameters
	if params, ok := srcTool["parameters"].(map[string]interface{}); ok {
		hermesTool["parameters"] = c.convertParameters(params)
	}

	return hermesTool, nil
}

// convertParameters converts OpenClaw parameters to Hermes format
func (c *ToolConverter) convertParameters(params map[string]interface{}) map[string]interface{} {
	hermesParams := make(map[string]interface{})

	// Map type
	if typ, ok := params["type"].(string); ok {
		hermesParams["type"] = typ
	}

	// Map properties
	if props, ok := params["properties"].(map[string]interface{}); ok {
		hermesParams["properties"] = props
	}

	// Map required
	if required, ok := params["required"].([]string); ok {
		hermesParams["required"] = required
	}

	return hermesParams
}

// Tool conversion functions for specific tools
func convertBashTool(srcTool map[string]interface{}) (map[string]interface{}, error) {
	tool := make(map[string]interface{})
	tool["name"] = "bash"
	tool["description"] = "Execute shell commands"

	params := make(map[string]interface{})
	params["type"] = "object"
	params["properties"] = map[string]interface{}{
		"command": map[string]interface{}{
			"type":        "string",
			"description": "The shell command to execute",
		},
		"timeout": map[string]interface{}{
			"type":        "number",
			"description": "Timeout in seconds (optional)",
		},
	}
	params["required"] = []string{"command"}
	tool["parameters"] = params

	return tool, nil
}

func convertReadFileTool(srcTool map[string]interface{}) (map[string]interface{}, error) {
	tool := make(map[string]interface{})
	tool["name"] = "read_file"
	tool["description"] = "Read the contents of a file"

	params := make(map[string]interface{})
	params["type"] = "object"
	params["properties"] = map[string]interface{}{
		"path": map[string]interface{}{
			"type":        "string",
			"description": "Path to the file to read",
		},
	}
	params["required"] = []string{"path"}
	tool["parameters"] = params

	return tool, nil
}

func convertWriteFileTool(srcTool map[string]interface{}) (map[string]interface{}, error) {
	tool := make(map[string]interface{})
	tool["name"] = "write_file"
	tool["description"] = "Write content to a file"

	params := make(map[string]interface{})
	params["type"] = "object"
	params["properties"] = map[string]interface{}{
		"path": map[string]interface{}{
			"type":        "string",
			"description": "Path to the file to write",
		},
		"content": map[string]interface{}{
			"type":        "string",
			"description": "Content to write to the file",
		},
	}
	params["required"] = []string{"path", "content"}
	tool["parameters"] = params

	return tool, nil
}

func convertEditFileTool(srcTool map[string]interface{}) (map[string]interface{}, error) {
	tool := make(map[string]interface{})
	tool["name"] = "edit_file"
	tool["description"] = "Edit a file by replacing specific content"

	params := make(map[string]interface{})
	params["type"] = "object"
	params["properties"] = map[string]interface{}{
		"path": map[string]interface{}{
			"type":        "string",
			"description": "Path to the file to edit",
		},
		"old_string": map[string]interface{}{
			"type":        "string",
			"description": "The string to replace",
		},
		"new_string": map[string]interface{}{
			"type":        "string",
			"description": "The replacement string",
		},
	}
	params["required"] = []string{"path", "old_string", "new_string"}
	tool["parameters"] = params

	return tool, nil
}

func convertGlobTool(srcTool map[string]interface{}) (map[string]interface{}, error) {
	tool := make(map[string]interface{})
	tool["name"] = "glob"
	tool["description"] = "Find files matching a pattern"

	params := make(map[string]interface{})
	params["type"] = "object"
	params["properties"] = map[string]interface{}{
		"pattern": map[string]interface{}{
			"type":        "string",
			"description": "Glob pattern to match files",
		},
	}
	params["required"] = []string{"pattern"}
	tool["parameters"] = params

	return tool, nil
}

func convertGrepTool(srcTool map[string]interface{}) (map[string]interface{}, error) {
	tool := make(map[string]interface{})
	tool["name"] = "grep"
	tool["description"] = "Search for text in files"

	params := make(map[string]interface{})
	params["type"] = "object"
	params["properties"] = map[string]interface{}{
		"pattern": map[string]interface{}{
			"type":        "string",
			"description": "Pattern to search for",
		},
		"path": map[string]interface{}{
			"type":        "string",
			"description": "Directory or file to search in",
		},
	}
	params["required"] = []string{"pattern"}
	tool["parameters"] = params

	return tool, nil
}

func convertSearchTool(srcTool map[string]interface{}) (map[string]interface{}, error) {
	tool := make(map[string]interface{})
	tool["name"] = "search"
	tool["description"] = "Search the web for information"

	params := make(map[string]interface{})
	params["type"] = "object"
	params["properties"] = map[string]interface{}{
		"query": map[string]interface{}{
			"type":        "string",
			"description": "Search query",
		},
	}
	params["required"] = []string{"query"}
	tool["parameters"] = params

	return tool, nil
}

func convertFetchTool(srcTool map[string]interface{}) (map[string]interface{}, error) {
	tool := make(map[string]interface{})
	tool["name"] = "fetch"
	tool["description"] = "Fetch content from a URL"

	params := make(map[string]interface{})
	params["type"] = "object"
	params["properties"] = map[string]interface{}{
		"url": map[string]interface{}{
			"type":        "string",
			"description": "URL to fetch",
		},
	}
	params["required"] = []string{"url"}
	tool["parameters"] = params

	return tool, nil
}

func convertBrowserTool(srcTool map[string]interface{}) (map[string]interface{}, error) {
	tool := make(map[string]interface{})
	tool["name"] = "browser"
	tool["description"] = "Control a web browser"

	params := make(map[string]interface{})
	params["type"] = "object"
	params["properties"] = map[string]interface{}{
		"action": map[string]interface{}{
			"type": "string",
			"enum": []string{"goto", "click", "type", "screenshot", "evaluate"},
		},
		"url": map[string]interface{}{
			"type":        "string",
			"description": "URL for goto action",
		},
	}
	params["required"] = []string{"action"}
	tool["parameters"] = params

	return tool, nil
}

func convertCodeInterpreterTool(srcTool map[string]interface{}) (map[string]interface{}, error) {
	tool := make(map[string]interface{})
	tool["name"] = "code_interpreter"
	tool["description"] = "Execute code in a sandboxed environment"

	params := make(map[string]interface{})
	params["type"] = "object"
	params["properties"] = map[string]interface{}{
		"code": map[string]interface{}{
			"type":        "string",
			"description": "Code to execute",
		},
		"language": map[string]interface{}{
			"type": "string",
			"enum": []string{"python", "javascript", "go"},
		},
	}
	params["required"] = []string{"code"}
	tool["parameters"] = params

	return tool, nil
}

func convertMemoryTool(srcTool map[string]interface{}) (map[string]interface{}, error) {
	tool := make(map[string]interface{})
	tool["name"] = "memory"
	tool["description"] = "Store and retrieve information from memory"

	params := make(map[string]interface{})
	params["type"] = "object"
	params["properties"] = map[string]interface{}{
		"action": map[string]interface{}{
			"type": "string",
			"enum": []string{"store", "retrieve"},
		},
		"content": map[string]interface{}{
			"type":        "string",
			"description": "Content to store or query",
		},
	}
	params["required"] = []string{"action"}
	tool["parameters"] = params

	return tool, nil
}

func convertMemorySearchTool(srcTool map[string]interface{}) (map[string]interface{}, error) {
	tool := make(map[string]interface{})
	tool["name"] = "memory_search"
	tool["description"] = "Search through stored memory"

	params := make(map[string]interface{})
	params["type"] = "object"
	params["properties"] = map[string]interface{}{
		"query": map[string]interface{}{
			"type":        "string",
			"description": "Search query",
		},
		"limit": map[string]interface{}{
			"type":        "number",
			"description": "Maximum results to return",
		},
	}
	params["required"] = []string{"query"}
	tool["parameters"] = params

	return tool, nil
}

// uniqueStrings removes duplicate strings from a slice
func uniqueStrings(slice []string) []string {
	seen := make(map[string]bool)
	result := []string{}
	for _, s := range slice {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

// ConfigConverter handles configuration conversion
type ConfigConverter struct{}

// NewConfigConverter creates a new config converter
func NewConfigConverter() *ConfigConverter {
	return &ConfigConverter{}
}

// ConvertConfig converts OpenClaw config to Hermes format
func (c *ConfigConverter) ConvertConfig(srcConfig map[string]interface{}) (map[string]interface{}, error) {
	hermesConfig := make(map[string]interface{})

	// Map common settings
	if timeout, ok := srcConfig["timeout"].(float64); ok {
		hermesConfig["timeout"] = int(timeout)
	}
	if maxTokens, ok := srcConfig["max_tokens"].(float64); ok {
		hermesConfig["max_tokens"] = int(maxTokens)
	}
	if model, ok := srcConfig["model"].(string); ok {
		hermesConfig["model"] = model
	}
	if temperature, ok := srcConfig["temperature"].(float64); ok {
		hermesConfig["temperature"] = temperature
	}

	return hermesConfig, nil
}

// DependencyConverter handles dependency conversion
type DependencyConverter struct{}

// NewDependencyConverter creates a new dependency converter
func NewDependencyConverter() *DependencyConverter {
	return &DependencyConverter{}
}

// ConvertDependencies converts OpenClaw dependencies to Hermes format
func (c *DependencyConverter) ConvertDependencies(deps []string) ([]string, []string, error) {
	var hermesDeps []string
	var warnings []string

	for _, dep := range deps {
		dep = strings.TrimSpace(dep)

		// Map common dependencies
		switch dep {
		case "openai":
			hermesDeps = append(hermesDeps, "openai")
		case "anthropic":
			hermesDeps = append(hermesDeps, "anthropic")
		case "requests":
			hermesDeps = append(hermesDeps, "requests")
		case "node-fetch":
			hermesDeps = append(hermesDeps, "fetch")
		default:
			// Preserve unknown dependencies
			hermesDeps = append(hermesDeps, dep)
			warnings = append(warnings, fmt.Sprintf("Dependency '%s' preserved (verify compatibility)", dep))
		}
	}

	return hermesDeps, warnings, nil
}
