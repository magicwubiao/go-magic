package tool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

// LintFile performs syntax checking on a file after writing.
// Returns issues (non-blocking) or nil if no issues found.
func LintFile(filePath string) ([]string, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".py":
		return lintPython(filePath)
	case ".json":
		return lintJSON(filePath)
	case ".yml", ".yaml":
		return lintYAML(filePath)
	case ".toml":
		return lintTOML(filePath)
	default:
		return nil, nil // Unsupported types, skip lint
	}
}

// lintPython checks Python syntax using py_compile
func lintPython(path string) ([]string, error) {
	// Check if python3 is available
	cmd := exec.Command("which", "python3")
	if err := cmd.Run(); err != nil {
		// python3 not available, skip
		return nil, nil
	}

	// Run py_compile to check syntax
	cmd = exec.Command("python3", "-m", "py_compile", path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		output := strings.TrimSpace(stderr.String())
		if output == "" {
			output = fmt.Sprintf("python syntax error (exit code %d)", cmd.ProcessState.ExitCode())
		}
		return []string{output}, nil
	}

	return nil, nil
}

// lintJSON checks JSON syntax using Go's encoding/json
func lintJSON(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return []string{err.Error()}, nil
	}

	return nil, nil
}

// lintYAML checks YAML syntax using gopkg.in/yaml.v3
func lintYAML(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var v interface{}
	if err := yaml.Unmarshal(data, &v); err != nil {
		return []string{err.Error()}, nil
	}

	return nil, nil
}

// lintTOML checks TOML syntax using github.com/BurntSushi/toml
func lintTOML(path string) ([]string, error) {
	_, err := toml.LoadFile(path)
	if err != nil {
		return []string{err.Error()}, nil
	}

	return nil, nil
}

// LintResult represents the result of a lint check
type LintResult struct {
	FilePath string   `json:"file_path"`
	Issues   []string `json:"issues,omitempty"`
	Success  bool     `json:"success"`
}

// LintFiles checks multiple files and returns aggregated results
func LintFiles(paths []string) []LintResult {
	var results []LintResult
	for _, path := range paths {
		issues, err := LintFile(path)
		result := LintResult{
			FilePath: path,
			Success:  err == nil && len(issues) == 0,
		}
		if issues != nil {
			result.Issues = issues
		}
		results = append(results, result)
	}
	return results
}
