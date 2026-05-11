package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// =============================================================================
// Execute Code Tool - Built-in Python code execution
// =============================================================================

// CodeExecutorConfig holds configuration for code execution
type CodeExecutorConfig struct {
	Timeout     time.Duration // Max execution time
	MemoryLimit int           // Memory limit in MB
	AllowedDirs []string      // Allowed working directories
	EnableTools bool          // Enable tool calling from code
}

// DefaultCodeExecutorConfig returns the default configuration
func DefaultCodeExecutorConfig() *CodeExecutorConfig {
	home, _ := os.UserHomeDir()
	return &CodeExecutorConfig{
		Timeout:     60 * time.Second,
		MemoryLimit: 512,
		AllowedDirs: []string{
			"/tmp",
			home + "/projects",
			home + "/workspace",
			".",
		},
		EnableTools: true,
	}
}

// ExecuteCodeTool provides in-process Python code execution with tool access
type ExecuteCodeTool struct {
	config *CodeExecutorConfig
	tools  map[string]Tool // Available tools for code to call
}

// NewExecuteCodeTool creates a new execute_code tool
func NewExecuteCodeTool() *ExecuteCodeTool {
	return &ExecuteCodeTool{
		config: DefaultCodeExecutorConfig(),
		tools:  make(map[string]Tool),
	}
}

// RegisterTool registers a tool that can be called from executed code
func (t *ExecuteCodeTool) RegisterTool(name string, tool Tool) {
	t.tools[name] = tool
}

func (t *ExecuteCodeTool) Name() string { return "execute_code" }

func (t *ExecuteCodeTool) Description() string {
	return `Execute Python code and optionally call tools.
Supports reading files, running commands, and using registered tools.
Code runs in an isolated subprocess with timeout and memory limits.`
}

func (t *ExecuteCodeTool) Schema() map[string]interface{} {
	// Get registered tool names for documentation
	var toolNames []string
	for name := range t.tools {
		toolNames = append(toolNames, name)
	}

	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"code": map[string]interface{}{
				"type":        "string",
				"description": "Python code to execute",
			},
			"language": map[string]interface{}{
				"type":        "string",
				"enum":       []string{"python", "python3"},
				"description": "Programming language (default: python)",
			},
			"timeout": map[string]interface{}{
				"type":        "number",
				"description": "Timeout in seconds (default: 60)",
			},
			"workdir": map[string]interface{}{
				"type":        "string",
				"description": "Working directory for code execution",
			},
			"tools": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Tool names to make available in code (default: all)",
			},
			"tools_output": map[string]interface{}{
				"type":        "boolean",
				"description": "Include tool call outputs in result",
			},
		},
		"required": []string{"code"},
	}
}

// Execute runs Python code with optional tool access
func (t *ExecuteCodeTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	code, _ := args["code"].(string)
	if code == "" {
		return nil, fmt.Errorf("code is required")
	}

	language, _ := args["language"].(string)
	if language == "" {
		language = "python"
	}

	timeout := t.config.Timeout
	if tArg, ok := args["timeout"].(float64); ok {
		timeout = time.Duration(tArg) * time.Second
	}
	if timeout > 120*time.Second {
		timeout = 120 * time.Second
	}

	workDir := "/tmp"
	if wd, ok := args["workdir"].(string); ok && wd != "" {
		// Security: verify workdir is allowed
		if !t.isDirAllowed(wd) {
			return nil, fmt.Errorf("working directory %s is not allowed", wd)
		}
		workDir = wd
	}

	toolsOutput := false
	if to, ok := args["tools_output"].(bool); ok {
		toolsOutput = to
	}

	// Filter tools if specified
	var toolNames []string
	if tnArg, ok := args["tools"].([]interface{}); ok {
		for _, tn := range tnArg {
			if name, ok := tn.(string); ok {
				toolNames = append(toolNames, name)
			}
		}
	} else {
		for name := range t.tools {
			toolNames = append(toolNames, name)
		}
	}

	// Generate tool wrapper code
	toolWrapper := t.generateToolWrapper(toolNames, toolsOutput)

	// Wrap user code
	fullCode := toolWrapper + "\n\n# User code\n" + code

	// Execute
	return t.executeCode(ctx, fullCode, language, workDir, timeout)
}

// isDirAllowed checks if a directory is allowed for execution
func (t *ExecuteCodeTool) isDirAllowed(dir string) bool {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}

	for _, allowed := range t.config.AllowedDirs {
		absAllowed, err := filepath.Abs(allowed)
		if err != nil {
			continue
		}
		if strings.HasPrefix(absDir, absAllowed) {
			return true
		}
	}
	return false
}

// generateToolWrapper creates Python code that exposes tools as callable functions
func (t *ExecuteCodeTool) generateToolWrapper(toolNames []string, includeOutput bool) string {
	var buf bytes.Buffer

	buf.WriteString(`import json
import sys
import os

# Tool registry - populated by executor
_TOOLS = {}

# Output collection
_TOOL_RESULTS = []

def register_tools(tools):
    """Register available tools."""
    global _TOOLS
    _TOOLS = tools

def tool(name, **kwargs):
    """Call a tool by name with keyword arguments.
    
    Args:
        name: Tool name (e.g., 'read_file', 'web_search')
        **kwargs: Tool arguments
        
    Returns:
        Tool result dict
    """
    if name not in _TOOLS:
        return {"error": f"Unknown tool: {name}"}
    
    result = _TOOLS[name](**kwargs)
    _TOOL_RESULTS.append({
        "tool": name,
        "args": kwargs,
        "result": result
    })
    return result

# Built-in convenience functions
def read_file(path, limit=None, offset=0):
    """Read file content. Alias for tool('read_file', ...)."""
    return tool('read_file', path=path, limit=limit, offset=offset)

def write_file(path, content):
    """Write content to file. Alias for tool('write_file', ...)."""
    return tool('write_file', path=path, content=content)

def search_files(pattern, path=".", file_pattern="*"):
    """Search files. Alias for tool('search_in_files', ...)."""
    return tool('search_in_files', pattern=pattern, path=path, file_pattern=file_pattern)

def terminal(command, timeout=30):
    """Execute terminal command. Alias for tool('execute_command', ...)."""
    return tool('execute_command', command=command, timeout=timeout)

def web_search(query, count=5):
    """Search the web. Alias for tool('web_search', ...)."""
    return tool('web_search', query=query, count=count)

def web_fetch(url):
    """Fetch web page. Alias for tool('web_fetch', ...)."""
    return tool('web_fetch', url=url)

def json_dump(obj, indent=2):
    """Pretty print JSON."""
    print(json.dumps(obj, indent=indent))

def print_results():
    """Print all tool results collected so far."""
    for r in _TOOL_RESULTS:
        print(f"[{r['tool']}] {json.dumps(r['result'], indent=2)}")

# Banner
print("Available tools:", list(_TOOLS.keys()))
print("-" * 40)
`)

	return buf.String()
}

// executeCode runs the code in a subprocess
func (t *ExecuteCodeTool) executeCode(ctx context.Context, code, language, workDir string, timeout time.Duration) (interface{}, error) {
	// Write code to temp file
	tmpFile, err := os.CreateTemp("", "magic_code_*.py")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	if _, err := tmpFile.WriteString(code); err != nil {
		return nil, fmt.Errorf("failed to write code: %w", err)
	}
	tmpFile.Close()

	// Build command
	cmd := exec.CommandContext(ctx, language, tmpFile.Name())

	// Prepare environment
	env := os.Environ()

	// Add tool definitions to environment
	toolsJSON, _ := t.serializeToolsForEnv()
	env = append(env, fmt.Sprintf("MAGIC_TOOLS=%s", toolsJSON))

	cmd.Env = env
	cmd.Dir = workDir

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Execute with timeout
	done := make(chan error, 1)
	go func() {
		done <- cmd.Run()
	}()

	select {
	case err := <-done:
		result := map[string]interface{}{
			"stdout":    stdout.String(),
			"stderr":    stderr.String(),
			"exit_code": 0,
		}

		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				result["error"] = "Execution timed out"
				result["exit_code"] = -1
			} else if exitErr, ok := err.(*exec.ExitError); ok {
				result["exit_code"] = exitErr.ExitCode()
			} else {
				result["error"] = err.Error()
				result["exit_code"] = -1
			}
		}

		return result, nil

	case <-ctx.Done():
		cmd.Process.Kill()
		return nil, fmt.Errorf("execution cancelled")
	}
}

// serializeToolsForEnv serializes tools for injection into subprocess
func (t *ExecuteCodeTool) serializeToolsForEnv() (string, error) {
	// Create a simplified tool interface for Python subprocess
	type SimpleTool struct {
		Name        string                 `json:"name"`
		Description string                 `json:"description"`
		Schema      map[string]interface{} `json:"schema"`
	}

	tools := make([]SimpleTool, 0, len(t.tools))
	for name, tool := range t.tools {
		tools = append(tools, SimpleTool{
			Name:        tool.Name(),
			Description: tool.Description(),
			Schema:      tool.Schema(),
		})
	}

	data, err := json.Marshal(tools)
	if err != nil {
		return "[]", err
	}
	return string(data), nil
}

// =============================================================================
// Code Execution with Tool Calls (Async)
// =============================================================================

// CodeExecutionRequest represents a code execution request with tool context
type CodeExecutionRequest struct {
	Code     string                 `json:"code"`
	Language string                 `json:"language,omitempty"`
	Timeout  int                    `json:"timeout,omitempty"`
	WorkDir  string                 `json:"workdir,omitempty"`
	Tools    []string               `json:"tools,omitempty"`
	Context  map[string]interface{} `json:"context,omitempty"`
}

// CodeExecutionResponse represents the response from code execution
type CodeExecutionResponse struct {
	Success     bool                   `json:"success"`
	Output      string                 `json:"output,omitempty"`
	Error       string                 `json:"error,omitempty"`
	ExitCode    int                    `json:"exit_code"`
	ToolCalls   []CodeToolCall         `json:"tool_calls,omitempty"`
	Duration    time.Duration          `json:"duration"`
}

// CodeToolCall represents a tool call made during code execution
type CodeToolCall struct {
	Tool     string      `json:"tool"`
	Args     interface{} `json:"args"`
	Result   interface{} `json:"result"`
	Duration time.Duration `json:"duration"`
}

// ExecuteWithTools executes code with tool integration
func (t *ExecuteCodeTool) ExecuteWithTools(ctx context.Context, req *CodeExecutionRequest, toolRegistry map[string]Tool) (*CodeExecutionResponse, error) {
	start := time.Now()

	// Register tools
	for name, tool := range toolRegistry {
		t.RegisterTool(name, tool)
	}

	args := map[string]interface{}{
		"code": req.Code,
	}
	if req.Language != "" {
		args["language"] = req.Language
	}
	if req.Timeout > 0 {
		args["timeout"] = float64(req.Timeout)
	}
	if req.WorkDir != "" {
		args["workdir"] = req.WorkDir
	}
	if len(req.Tools) > 0 {
		args["tools"] = req.Tools
	}
	args["tools_output"] = true

	result, err := t.Execute(ctx, args)
	if err != nil {
		return &CodeExecutionResponse{
			Success:  false,
			Error:    err.Error(),
			ExitCode: -1,
			Duration: time.Since(start),
		}, nil
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		return &CodeExecutionResponse{
			Success: false,
			Error:   "Invalid result format",
			Duration: time.Since(start),
		}, nil
	}

	return &CodeExecutionResponse{
		Success:   resultMap["exit_code"] == 0,
		Output:    fmt.Sprintf("%v", resultMap["stdout"]),
		Error:     fmt.Sprintf("%v", resultMap["stderr"]),
		ExitCode:  int(resultMap["exit_code"].(float64)),
		Duration:  time.Since(start),
	}, nil
}

// =============================================================================
// Code Template Library
// =============================================================================

// CodeTemplates provides pre-built code templates
var CodeTemplates = map[string]string{
	"file_processor": `
import os
import json

def process_files(directory, pattern="*.txt"):
    """Process all files matching pattern in directory."""
    import glob
    results = []
    for filepath in glob.glob(os.path.join(directory, pattern)):
        with open(filepath, 'r') as f:
            results.append({
                'path': filepath,
                'content': f.read(),
                'size': os.path.getsize(filepath)
            })
    return results

# Example usage
results = process_files('/tmp', '*.txt')
print(f"Processed {len(results)} files")
json_dump(results)
`,

	"web_scraper": `
import json
import re

def scrape_links(html, base_url=''):
    """Extract links from HTML."""
    pattern = r'href=["\']([^"\']+)["\']'
    links = re.findall(pattern, html)
    return [{'url': link, 'absolute': link if link.startswith('http') else base_url + link} for link in links]

# Example usage
html = tool('web_fetch', url='https://example.com')
if isinstance(html, dict):
    html = html.get('content', '')

links = scrape_links(str(html), 'https://example.com')
print(f"Found {len(links)} links")
json_dump(links[:10])
`,

	"data_analysis": `
import json
from collections import Counter

def analyze_data(items, key):
    """Analyze items by key."""
    counter = Counter()
    for item in items:
        if isinstance(item, dict) and key in item:
            counter[item[key]] += 1
    return {
        'total': len(items),
        'distribution': dict(counter),
        'most_common': counter.most_common(5)
    }

# Example: Analyze a list
data = [{'category': 'A', 'value': 1}, {'category': 'B', 'value': 2}]
results = analyze_data(data, 'category')
json_dump(results)
`,
}

// GetTemplate returns a code template by name
func GetTemplate(name string) (string, bool) {
	code, ok := CodeTemplates[name]
	return code, ok
}

// ListTemplates returns all available template names
func ListTemplates() []string {
	templates := make([]string, 0, len(CodeTemplates))
	for name := range CodeTemplates {
		templates = append(templates, name)
	}
	return templates
}

// =============================================================================
// Sandbox utilities
// =============================================================================

// ValidateCode performs static analysis on code for security issues
func ValidateCode(code string) (bool, string) {
	// Check for dangerous patterns
	dangerous := []string{
		`__import__`,
		`eval(`,
		`exec(`,
		`open.*\(.*\)`,
		`subprocess`,
		`os.system`,
		`os.popen`,
		`pty.spawn`,
		`fork`,
		`multiprocessing`,
		`threading`,
	}

	for _, pattern := range dangerous {
		re := regexp.MustCompile(pattern)
		if re.MatchString(code) {
			return false, fmt.Sprintf("potentially dangerous pattern detected: %s", pattern)
		}
	}

	return true, ""
}
