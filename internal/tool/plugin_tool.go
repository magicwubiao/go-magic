package tool

import (
	"context"
	"fmt"
	"strings"
)

// PluginTool wraps a plugin command as a Tool that the agent can call.
// When the agent invokes this tool, it executes the plugin's entrypoint
// with the command name and arguments.
type PluginTool struct {
	name        string
	description string
	pluginID    string
	command     string
	entrypoint  string // resolved path to the plugin executable/script
	args        []string
}

// NewPluginTool creates a tool from a plugin command spec
func NewPluginTool(pluginID, entrypoint string, command string, desc string, argNames []string) *PluginTool {
	// Sanitize name: only alphanumeric and underscores allowed by LLM APIs
	safeID := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + 32 // to lowercase
		}
		return '_'
	}, pluginID)
	safeCmd := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + 32
		}
		return '_'
	}, command)
	name := fmt.Sprintf("plugin_%s_%s", safeID, safeCmd)

	if desc == "" {
		desc = fmt.Sprintf("Plugin %s: %s", pluginID, command)
	}

	return &PluginTool{
		name:        name,
		description: desc,
		pluginID:    pluginID,
		command:     command,
		entrypoint:  entrypoint,
		args:        argNames,
	}
}

func (t *PluginTool) Name() string        { return t.name }
func (t *PluginTool) Description() string { return t.description }

func (t *PluginTool) Schema() map[string]interface{} {
	properties := make(map[string]interface{})
	required := make([]string, 0)

	// Add named arguments from manifest
	for _, arg := range t.args {
		properties[arg] = map[string]interface{}{
			"type":        "string",
			"description": fmt.Sprintf("Argument: %s", arg),
		}
		// Mark named args as optional (agent can also use command_args)
	}

	// Add a catch-all command_args field
	properties["command_args"] = map[string]interface{}{
		"type":        "string",
		"description": fmt.Sprintf("Arguments for '%s' command. Named args: %s. Can also pass all args as a single string.", t.command, strings.Join(t.args, ", ")),
	}

	return map[string]interface{}{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}
}

func (t *PluginTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	// Build command: entrypoint <command> [args...]
	cmdArgs := []string{t.command}

	// Add arguments from params
	if cmdArgsStr, ok := params["command_args"].(string); ok && cmdArgsStr != "" {
		cmdArgs = append(cmdArgs, strings.Split(cmdArgsStr, " ")...)
	} else {
		// Also support named args from params
		for _, argName := range t.args {
			if val, ok := params[argName].(string); ok {
				cmdArgs = append(cmdArgs, val)
			}
		}
	}

	return executePluginCommand(t.entrypoint, cmdArgs)
}
