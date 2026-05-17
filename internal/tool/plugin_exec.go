package tool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// executePluginCommand runs a plugin entrypoint with the given arguments
// and returns structured output.
func executePluginCommand(entrypoint string, args []string) (interface{}, error) {
	cmd := exec.Command(entrypoint, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	output := stdout.String()
	errOutput := stderr.String()

	if err != nil {
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("plugin execution failed: %v", err),
			"stderr":  errOutput,
			"elapsed": elapsed.String(),
		}, nil // Return error as result, don't fail the tool call
	}

	// Try to parse as JSON
	var result interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		// Return raw output if not JSON
		return map[string]interface{}{
			"success": true,
			"output":  output,
			"elapsed": elapsed.String(),
		}, nil
	}

	return result, nil
}
