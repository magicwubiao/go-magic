package plugin

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/magicwubiao/go-magic/internal/tool"
)

type ShellPlugin struct {
	timeout time.Duration
}

func NewShellPlugin() *ShellPlugin {
	return &ShellPlugin{
		timeout: 30 * time.Second,
	}
}

func (p *ShellPlugin) Name() string {
	return "shell"
}

func (p *ShellPlugin) Version() string {
	return "1.0.0"
}

func (p *ShellPlugin) Description() string {
	return "Shell command execution plugin"
}

func (p *ShellPlugin) Tools() []tool.Tool {
	return []tool.Tool{
		&ShellTool{timeout: p.timeout},
	}
}

func (p *ShellPlugin) Initialize() error {
	fmt.Println("Shell plugin initialized")
	return nil
}

func (p *ShellPlugin) Shutdown() error {
	fmt.Println("Shell plugin shutdown")
	return nil
}

type ShellTool struct {
	timeout time.Duration
}

func (t *ShellTool) Name() string {
	return "shell_execute"
}

func (t *ShellTool) Description() string {
	return "Execute shell commands"
}

func (t *ShellTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type":        "string",
				"description": "Shell command to execute",
			},
		},
		"required": []string{"command"},
	}
}

func (t *ShellTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	command, ok := args["command"].(string)
	if !ok {
		return nil, fmt.Errorf("command argument is required")
	}

	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	output, err := cmd.CombinedOutput()

	result := map[string]interface{}{
		"output": string(output),
	}

	if err != nil {
		result["exit_code"] = 1
		result["error"] = err.Error()
	} else {
		result["exit_code"] = 0
	}

	return result, nil
}
