package tool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

type NodeExecuteTool struct {
	timeout time.Duration
}

func NewNodeExecuteTool() *NodeExecuteTool {
	return &NodeExecuteTool{
		timeout: 30 * time.Second,
	}
}

func (t *NodeExecuteTool) Name() string {
	return "node_execute"
}

func (t *NodeExecuteTool) Description() string {
	return "Execute Node.js code"
}

func (t *NodeExecuteTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"code": map[string]interface{}{
				"type":        "string",
				"description": "Node.js code to execute",
			},
		},
		"required": []string{"code"},
	}
}

func (t *NodeExecuteTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	code, ok := args["code"].(string)
	if !ok {
		return nil, fmt.Errorf("code argument is required")
	}

	tmpFile, err := os.CreateTemp("", "node_*.js")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(code); err != nil {
		return nil, err
	}
	tmpFile.Close()

	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "node", tmpFile.Name())
	output, err := cmd.CombinedOutput()

	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("execution timed out after %v", t.timeout)
	}

	if err != nil {
		return map[string]interface{}{
			"success": false,
			"output":  string(output),
			"error":   err.Error(),
		}, nil
	}

	return map[string]interface{}{
		"success": true,
		"output":  string(output),
	}, nil
}
