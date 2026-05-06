package tool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

type PythonExecuteTool struct {
	timeout time.Duration
}

func NewPythonExecuteTool() *PythonExecuteTool {
	return &PythonExecuteTool{
		timeout: 30 * time.Second,
	}
}

func (t *PythonExecuteTool) Name() string {
	return "python_execute"
}

func (t *PythonExecuteTool) Description() string {
	return "Execute Python code"
}

func (t *PythonExecuteTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"code": map[string]interface{}{
				"type":        "string",
				"description": "Python code to execute",
			},
		},
		"required": []string{"code"},
	}
}

func (t *PythonExecuteTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	code, ok := args["code"].(string)
	if !ok {
		return nil, fmt.Errorf("code argument is required")
	}

	tmpFile, err := os.CreateTemp("", "python_*.py")
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

	cmd := exec.CommandContext(ctx, "python", tmpFile.Name())
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
