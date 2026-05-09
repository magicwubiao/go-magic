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
	name := fmt.Sprintf("plugin_%s_%s", pluginID, command)
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

	for _, arg := range t.args {
		properties[arg] = map[string]interface{}{
			"type":        "string",
			"description": fmt.Sprintf("Argument: %s", arg),
		}
		required = append(required, arg)
	}

	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command_args": map[string]interface{}{
				"type":        "string",
				"description": fmt.Sprintf("Arguments for '%s' command. Named args: %s", t.command, strings.Join(t.args, ", ")),
			},
		},
		"required": []string{},
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
