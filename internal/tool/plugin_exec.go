package tool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// PluginResult represents the standardized result of a plugin execution
type PluginResult struct {
	Success bool        `json:"success"`
	Output  interface{} `json:"output,omitempty"`
	Error   string      `json:"error,omitempty"`
	Stderr  string      `json:"stderr,omitempty"`
	Elapsed string      `json:"elapsed"`
}

// executePluginCommand runs a plugin entrypoint with the given arguments
// and returns structured output. Errors are returned as structured results
// with success=false, not as Go errors, to allow the agent to handle failures gracefully.
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

	result := PluginResult{
		Success: err == nil,
		Elapsed: elapsed.String(),
		Stderr:  errOutput,
	}

	if err != nil {
		result.Error = fmt.Sprintf("plugin execution failed: %v", err)
		// Try to parse any JSON output even on error
		var jsonOut interface{}
		if json.Unmarshal([]byte(output), &jsonOut) == nil {
			result.Output = jsonOut
		} else {
			result.Output = output
		}
		return result, nil
	}

	// Try to parse as JSON
	var jsonResult interface{}
	if err := json.Unmarshal([]byte(output), &jsonResult); err != nil {
		// Return raw output if not JSON
		result.Output = output
		return result, nil
	}

	// If the plugin returned a JSON object that already has a "success" field,
	// merge it with our wrapper
	if m, ok := jsonResult.(map[string]interface{}); ok {
		if _, hasSuccess := m["success"]; hasSuccess {
			return m, nil
		}
	}

	result.Output = jsonResult
	return result, nil
}
