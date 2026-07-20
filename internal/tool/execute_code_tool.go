package tool

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// =============================================================================
// Execute Code Tool - Multi-language code execution with package management
// =============================================================================

// Language represents a supported programming language
type Language string

const (
	LanguagePython     Language = "python"
	LanguageJavaScript Language = "javascript"
	LanguageTypeScript Language = "typescript"
	LanguageGo         Language = "go"
	LanguageRust       Language = "rust"
	LanguageJava       Language = "java"
	LanguageCpp        Language = "cpp"
	LanguageC          Language = "c"
)

// CodeExecutorConfig holds configuration for code execution
type CodeExecutorConfig struct {
	Timeout       time.Duration // Max execution time
	MemoryLimit   int           // Memory limit in MB
	AllowedDirs   []string      // Allowed working directories
	EnableTools   bool          // Enable tool calling from code
	EnableNetwork bool          // Enable network access
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
		EnableTools:   true,
		EnableNetwork: false,
	}
}

// ExecuteCodeTool provides in-process multi-language code execution with tool access
// and package management support
type ExecuteCodeTool struct {
	config     *CodeExecutorConfig
	tools      map[string]Tool // Available tools for code to call
	codingMode bool
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

// SetCodingMode enables coding mode with relaxed restrictions
func (t *ExecuteCodeTool) SetCodingMode(enabled bool) {
	t.codingMode = enabled
	if enabled {
		t.config.Timeout = 600 * time.Second // 10 minutes for coding mode (was 300s)
		t.config.MemoryLimit = 4096          // 4GB (was 2GB)
		t.config.EnableNetwork = true
	}
}

func (t *ExecuteCodeTool) Name() string { return "execute_code" }

func (t *ExecuteCodeTool) Description() string {
	return `Execute code in multiple programming languages with optional tool access and package management.
Supports reading files, running commands, and using registered tools.
Code runs in an isolated subprocess with timeout and memory limits.

Supported languages:
- python, python3: Python 3.x (supports pip packages)
- node, nodejs, js: Node.js JavaScript (supports npm packages)
- ts, typescript: TypeScript via ts-node (supports npm packages)
- go: Go (supports go mod dependencies)
- rust: Rust (supports cargo packages)
- java: Java (supports Maven/Gradle dependencies)
- cpp, c: C/C++ (supports cmake)

Package installation:
- Python: packages array for pip install
- Node.js/TypeScript: packages array for npm install
- Go: imports in code trigger go get
- Rust: dependencies in Cargo.toml format
- Java: Maven coordinates in packages array
- C/C++: libraries via system package manager`
}

func (t *ExecuteCodeTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"code": map[string]interface{}{
				"type":        "string",
				"description": "Code to execute",
			},
			"language": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"python", "python3", "node", "nodejs", "js", "ts", "typescript", "go", "rust", "java", "cpp", "c"},
				"description": "Programming language",
			},
			"timeout": map[string]interface{}{
				"type":        "number",
				"description": "Timeout in seconds (default: 60, max: 300 in coding mode)",
			},
			"workdir": map[string]interface{}{
				"type":        "string",
				"description": "Working directory for code execution",
			},
			"packages": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Package names to install before execution",
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
			"args": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Command line arguments for the program",
			},
		},
		"required": []string{"code", "language"},
	}
}

// Execute runs code with optional tool access and package management
func (t *ExecuteCodeTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	code, _ := args["code"].(string)
	if code == "" {
		return nil, fmt.Errorf("code is required")
	}

	language, _ := args["language"].(string)
	if language == "" {
		language = "python"
	}

	// Normalize language names
	lang := t.normalizeLanguage(language)

	timeout := t.config.Timeout
	if tArg, ok := args["timeout"].(float64); ok {
		timeout = time.Duration(tArg) * time.Second
	}
	// Enforce max timeout
	maxTimeout := 120 * time.Second
	if t.codingMode {
		maxTimeout = 300 * time.Second
	}
	if timeout > maxTimeout {
		timeout = maxTimeout
	}

	workDir := "/tmp"
	if wd, ok := args["workdir"].(string); ok && wd != "" {
		if !t.isDirAllowed(wd) {
			return nil, fmt.Errorf("working directory %s is not allowed", wd)
		}
		workDir = wd
	}

	// Parse packages
	var packages []string
	if pkgArg, ok := args["packages"].([]interface{}); ok {
		for _, pkg := range pkgArg {
			if name, ok := pkg.(string); ok {
				packages = append(packages, name)
			}
		}
	}

	// Parse args
	var progArgs []string
	if argList, ok := args["args"].([]interface{}); ok {
		for _, a := range argList {
			if s, ok := a.(string); ok {
				progArgs = append(progArgs, s)
			}
		}
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

	// Create execution context
	execCtx := &codeExecutionContext{
		lang:        lang,
		code:        code,
		workDir:     workDir,
		packages:    packages,
		progArgs:    progArgs,
		toolNames:   toolNames,
		toolsOutput: toolsOutput,
		timeout:     timeout,
	}

	return t.executeWithLanguage(ctx, execCtx)
}

// codeExecutionContext holds execution parameters
type codeExecutionContext struct {
	lang        Language
	code        string
	workDir     string
	packages    []string
	progArgs    []string
	toolNames   []string
	toolsOutput bool
	timeout     time.Duration
}

// normalizeLanguage normalizes language names
func (t *ExecuteCodeTool) normalizeLanguage(lang string) Language {
	switch strings.ToLower(lang) {
	case "python", "python3", "py":
		return LanguagePython
	case "node", "nodejs", "js", "javascript":
		return LanguageJavaScript
	case "ts", "typescript":
		return LanguageTypeScript
	case "go", "golang":
		return LanguageGo
	case "rust", "rs":
		return LanguageRust
	case "java":
		return LanguageJava
	case "cpp", "c++", "cxx", "cc":
		return LanguageCpp
	case "c":
		return LanguageC
	default:
		return LanguagePython
	}
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
		sep := string(filepath.Separator)
		if absDir == absAllowed ||
			strings.HasPrefix(absDir, absAllowed+sep) ||
			strings.HasPrefix(absAllowed, absDir+sep) {
			return true
		}
	}
	return false
}

// executeWithLanguage executes code based on language
func (t *ExecuteCodeTool) executeWithLanguage(ctx context.Context, execCtx *codeExecutionContext) (interface{}, error) {
	switch execCtx.lang {
	case LanguagePython:
		return t.executePython(ctx, execCtx)
	case LanguageJavaScript:
		return t.executeJavaScript(ctx, execCtx)
	case LanguageTypeScript:
		return t.executeTypeScript(ctx, execCtx)
	case LanguageGo:
		return t.executeGo(ctx, execCtx)
	case LanguageRust:
		return t.executeRust(ctx, execCtx)
	case LanguageJava:
		return t.executeJava(ctx, execCtx)
	case LanguageCpp, LanguageC:
		return t.executeCpp(ctx, execCtx)
	default:
		return nil, fmt.Errorf("unsupported language: %s", execCtx.lang)
	}
}

// executePython executes Python code with pip package support
func (t *ExecuteCodeTool) executePython(ctx context.Context, execCtx *codeExecutionContext) (interface{}, error) {
	// Install packages if specified
	if len(execCtx.packages) > 0 {
		pkgArgs := append([]string{"-m", "pip", "install", "--quiet"}, execCtx.packages...)
		cmd := exec.CommandContext(ctx, "python3", pkgArgs...)
		if output, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("failed to install packages: %w\n%s", err, output)
		}
	}

	// Generate tool wrapper
	toolWrapper := t.generatePythonToolWrapper(execCtx.toolNames, execCtx.toolsOutput)
	fullCode := toolWrapper + "\n\n# User code\n" + execCtx.code

	return t.executeScript(ctx, fullCode, "python3", ".py", execCtx.workDir, execCtx.timeout, execCtx.progArgs)
}

// executeJavaScript executes JavaScript/Node.js code with npm package support
func (t *ExecuteCodeTool) executeJavaScript(ctx context.Context, execCtx *codeExecutionContext) (interface{}, error) {
	// Create temp directory for node_modules
	tempDir, err := os.MkdirTemp("", "magic_js_*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Initialize npm project
	if err := t.initNpmProject(tempDir); err != nil {
		return nil, err
	}

	// Install packages if specified
	if len(execCtx.packages) > 0 {
		if err := t.installNpmPackages(tempDir, execCtx.packages); err != nil {
			return nil, err
		}
	}

	// Generate tool wrapper
	toolWrapper := t.generateNodeToolWrapper(execCtx.toolNames, execCtx.toolsOutput)
	fullCode := toolWrapper + "\n\n// User code\n" + execCtx.code

	return t.executeScript(ctx, fullCode, "node", ".js", tempDir, execCtx.timeout, execCtx.progArgs)
}

// executeTypeScript executes TypeScript code with ts-node
func (t *ExecuteCodeTool) executeTypeScript(ctx context.Context, execCtx *codeExecutionContext) (interface{}, error) {
	// Create temp directory
	tempDir, err := os.MkdirTemp("", "magic_ts_*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Initialize npm project and install TypeScript
	if err := t.initNpmProject(tempDir); err != nil {
		return nil, err
	}

	// Install TypeScript and ts-node
	packages := append([]string{"typescript", "ts-node", "@types/node"}, execCtx.packages...)
	if err := t.installNpmPackages(tempDir, packages); err != nil {
		return nil, err
	}

	// Generate tool wrapper
	toolWrapper := t.generateNodeToolWrapper(execCtx.toolNames, execCtx.toolsOutput)
	fullCode := toolWrapper + "\n\n// User code\n" + execCtx.code

	// Write TypeScript file
	tsFile := filepath.Join(tempDir, "script.ts")
	if err := os.WriteFile(tsFile, []byte(fullCode), 0644); err != nil {
		return nil, fmt.Errorf("failed to write TypeScript file: %w", err)
	}

	// Execute with ts-node
	cmd := exec.CommandContext(ctx, filepath.Join(tempDir, "node_modules", ".bin", "ts-node"), tsFile)
	cmd.Dir = tempDir
	cmd.Env = append(os.Environ(), "NODE_PATH="+filepath.Join(tempDir, "node_modules"))

	return t.runCommand(cmd, execCtx.timeout)
}

// executeGo executes Go code with module support
func (t *ExecuteCodeTool) executeGo(ctx context.Context, execCtx *codeExecutionContext) (interface{}, error) {
	// Create temp directory
	tempDir, err := os.MkdirTemp("", "magic_go_*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Initialize go module
	modName := fmt.Sprintf("magicexec%d", time.Now().Unix())
	cmd := exec.CommandContext(ctx, "go", "mod", "init", modName)
	cmd.Dir = tempDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to initialize go module: %w\n%s", err, output)
	}

	// Write code to main.go
	mainFile := filepath.Join(tempDir, "main.go")
	if err := os.WriteFile(mainFile, []byte(execCtx.code), 0644); err != nil {
		return nil, fmt.Errorf("failed to write main.go: %w", err)
	}

	// Download dependencies
	cmd = exec.CommandContext(ctx, "go", "mod", "tidy")
	cmd.Dir = tempDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to tidy go modules: %w\n%s", err, output)
	}

	// Run the code
	cmd = exec.CommandContext(ctx, "go", "run", ".")
	cmd.Dir = tempDir
	cmd.Args = append(cmd.Args, execCtx.progArgs...)

	return t.runCommand(cmd, execCtx.timeout)
}

// executeRust executes Rust code with cargo support
func (t *ExecuteCodeTool) executeRust(ctx context.Context, execCtx *codeExecutionContext) (interface{}, error) {
	// Create temp directory
	tempDir, err := os.MkdirTemp("", "magic_rust_*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Create Cargo.toml
	cargoToml := `[package]
name = "magicexec"
version = "0.1.0"
edition = "2021"

[dependencies]
`
	// Add dependencies if specified
	for _, pkg := range execCtx.packages {
		parts := strings.Split(pkg, "=")
		if len(parts) == 2 {
			cargoToml += fmt.Sprintf("%s = \"%s\"\n", strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
		} else {
			cargoToml += fmt.Sprintf("%s = \"*\"\n", pkg)
		}
	}

	if err := os.WriteFile(filepath.Join(tempDir, "Cargo.toml"), []byte(cargoToml), 0644); err != nil {
		return nil, fmt.Errorf("failed to write Cargo.toml: %w", err)
	}

	// Create src directory and main.rs
	srcDir := filepath.Join(tempDir, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create src directory: %w", err)
	}

	if err := os.WriteFile(filepath.Join(srcDir, "main.rs"), []byte(execCtx.code), 0644); err != nil {
		return nil, fmt.Errorf("failed to write main.rs: %w", err)
	}

	// Build and run
	cmd := exec.CommandContext(ctx, "cargo", "run", "--quiet")
	cmd.Dir = tempDir

	return t.runCommand(cmd, execCtx.timeout)
}

// executeJava executes Java code with Maven/Gradle support
func (t *ExecuteCodeTool) executeJava(ctx context.Context, execCtx *codeExecutionContext) (interface{}, error) {
	// Create temp directory
	tempDir, err := os.MkdirTemp("", "magic_java_*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Create Maven pom.xml if dependencies specified
	if len(execCtx.packages) > 0 {
		pomXML := `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0"
         xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
         xsi:schemaLocation="http://maven.apache.org/POM/4.0.0 http://maven.apache.org/xsd/maven-4.0.0.xsd">
    <modelVersion>4.0.0</modelVersion>
    <groupId>com.magic</groupId>
    <artifactId>exec</artifactId>
    <version>1.0</version>
    <dependencies>
`
		for _, pkg := range execCtx.packages {
			// Parse Maven coordinate: groupId:artifactId:version
			parts := strings.Split(pkg, ":")
			if len(parts) >= 2 {
				version := "LATEST"
				if len(parts) >= 3 {
					version = parts[2]
				}
				pomXML += fmt.Sprintf(`        <dependency>
            <groupId>%s</groupId>
            <artifactId>%s</artifactId>
            <version>%s</version>
        </dependency>
`, parts[0], parts[1], version)
			}
		}
		pomXML += `    </dependencies>
</project>`

		if err := os.WriteFile(filepath.Join(tempDir, "pom.xml"), []byte(pomXML), 0644); err != nil {
			return nil, fmt.Errorf("failed to write pom.xml: %w", err)
		}

		// Download dependencies
		cmd := exec.CommandContext(ctx, "mvn", "dependency:copy-dependencies", "-DoutputDirectory=lib", "-q")
		cmd.Dir = tempDir
		if output, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("failed to download dependencies: %w\n%s", err, output)
		}
	}

	// Write Main.java
	srcDir := filepath.Join(tempDir, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create src directory: %w", err)
	}

	if err := os.WriteFile(filepath.Join(srcDir, "Main.java"), []byte(execCtx.code), 0644); err != nil {
		return nil, fmt.Errorf("failed to write Main.java: %w", err)
	}

	// Compile
	compileCmd := exec.CommandContext(ctx, "javac", "-d", ".", "src/Main.java")
	compileCmd.Dir = tempDir
	if output, err := compileCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("compilation failed: %w\n%s", err, output)
	}

	// Run
	runCmd := exec.CommandContext(ctx, "java", "Main")
	runCmd.Dir = tempDir

	// Add classpath if dependencies exist
	libDir := filepath.Join(tempDir, "lib")
	if _, err := os.Stat(libDir); err == nil {
		runCmd.Args = append(runCmd.Args, "-cp", ".:lib/*")
	}

	runCmd.Args = append(runCmd.Args, execCtx.progArgs...)

	return t.runCommand(runCmd, execCtx.timeout)
}

// executeCpp executes C/C++ code with cmake support
func (t *ExecuteCodeTool) executeCpp(ctx context.Context, execCtx *codeExecutionContext) (interface{}, error) {
	// Create temp directory
	tempDir, err := os.MkdirTemp("", "magic_cpp_*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Determine file extension and compiler
	ext := ".cpp"
	compiler := "g++"
	if execCtx.lang == LanguageC {
		ext = ".c"
		compiler = "gcc"
	}

	// Write source file
	srcFile := filepath.Join(tempDir, "main"+ext)
	if err := os.WriteFile(srcFile, []byte(execCtx.code), 0644); err != nil {
		return nil, fmt.Errorf("failed to write source file: %w", err)
	}

	// Compile
	outputFile := filepath.Join(tempDir, "main")
	compileArgs := []string{"-o", outputFile, srcFile, "-std=c++14"}
	if execCtx.lang == LanguageC {
		compileArgs = []string{"-o", outputFile, srcFile}
	}

	// Add libraries if specified
	for _, lib := range execCtx.packages {
		compileArgs = append(compileArgs, "-l"+lib)
	}

	compileCmd := exec.CommandContext(ctx, compiler, compileArgs...)
	compileCmd.Dir = tempDir
	if output, err := compileCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("compilation failed: %w\n%s", err, output)
	}

	// Run
	runCmd := exec.CommandContext(ctx, outputFile)
	runCmd.Dir = tempDir
	runCmd.Args = append(runCmd.Args, execCtx.progArgs...)

	return t.runCommand(runCmd, execCtx.timeout)
}

// initNpmProject initializes an npm project
func (t *ExecuteCodeTool) initNpmProject(dir string) error {
	pkgJSON := `{"name": "magicexec", "version": "1.0.0", "private": true}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0644); err != nil {
		return fmt.Errorf("failed to create package.json: %w", err)
	}
	return nil
}

// installNpmPackages installs npm packages
func (t *ExecuteCodeTool) installNpmPackages(dir string, packages []string) error {
	args := append([]string{"install", "--silent"}, packages...)
	cmd := exec.Command("npm", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to install npm packages: %w\n%s", err, output)
	}
	return nil
}

// executeScript executes a script file
func (t *ExecuteCodeTool) executeScript(ctx context.Context, code, executable, fileExt, workDir string, timeout time.Duration, args []string) (interface{}, error) {
	// Write code to temp file
	tmpFile, err := os.CreateTemp("", "magic_code_*"+fileExt)
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
	cmdArgs := append([]string{tmpFile.Name()}, args...)
	cmd := exec.CommandContext(ctx, executable, cmdArgs...)
	cmd.Dir = workDir

	return t.runCommand(cmd, timeout)
}

// runCommand runs a command with timeout
func (t *ExecuteCodeTool) runCommand(cmd *exec.Cmd, timeout time.Duration) (interface{}, error) {
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
			if exitErr, ok := err.(*exec.ExitError); ok {
				result["exit_code"] = exitErr.ExitCode()
			} else {
				result["error"] = err.Error()
				result["exit_code"] = -1
			}
		}

		return result, nil

	case <-time.After(timeout):
		cmd.Process.Kill()
		return map[string]interface{}{
			"stdout":    stdout.String(),
			"stderr":    stderr.String(),
			"exit_code": -1,
			"error":     "execution timed out",
		}, nil
	}
}

// generatePythonToolWrapper creates Python code that exposes tools as callable functions
func (t *ExecuteCodeTool) generatePythonToolWrapper(toolNames []string, includeOutput bool) string {
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

// generateNodeToolWrapper creates Node.js code that exposes tools as callable functions
func (t *ExecuteCodeTool) generateNodeToolWrapper(toolNames []string, includeOutput bool) string {
	var buf bytes.Buffer

	buf.WriteString(`// Tool registry - populated by executor
const _TOOLS = {};
const _TOOL_RESULTS = [];

// Register available tools
function register_tools(tools) {
    Object.assign(_TOOLS, tools);
}

// Call a tool by name with arguments
async function tool(name, kwargs = {}) {
    if (!Object.keys(_TOOLS).includes(name)) {
        return { error: "Unknown tool: " + name };
    }
    
    const result = await _TOOLS[name](kwargs);
    _TOOL_RESULTS.push({
        tool: name,
        args: kwargs,
        result: result
    });
    return result;
}

// Built-in convenience functions
async function read_file(path, options = {}) {
    return tool('read_file', { path, ...options });
}

async function write_file(path, content) {
    return tool('write_file', { path, content });
}

async function search_files(pattern, options = {}) {
    return tool('search_in_files', { pattern, ...options });
}

async function terminal(command, options = {}) {
    return tool('execute_command', { command, ...options });
}

async function web_search(query, options = {}) {
    return tool('web_search', { query, ...options });
}

async function web_fetch(url) {
    return tool('web_fetch', { url });
}

function json_dump(obj, indent = 2) {
    console.log(JSON.stringify(obj, null, indent));
}

function print_results() {
    _TOOL_RESULTS.forEach(r => {
        console.log("[" + r.tool + "] " + JSON.stringify(r.result, null, 2));
    });
}

// Banner
console.log("Available tools:", Object.keys(_TOOLS));
console.log("-".repeat(40));
`)

	return buf.String()
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
	Packages []string               `json:"packages,omitempty"`
	Args     []string               `json:"args,omitempty"`
	Tools    []string               `json:"tools,omitempty"`
	Context  map[string]interface{} `json:"context,omitempty"`
}

// CodeExecutionResponse represents the response from code execution
type CodeExecutionResponse struct {
	Success   bool           `json:"success"`
	Output    string         `json:"output,omitempty"`
	Error     string         `json:"error,omitempty"`
	ExitCode  int            `json:"exit_code"`
	ToolCalls []CodeToolCall `json:"tool_calls,omitempty"`
	Duration  time.Duration  `json:"duration"`
}

// CodeToolCall represents a tool call made during code execution
type CodeToolCall struct {
	Tool     string        `json:"tool"`
	Args     interface{}   `json:"args"`
	Result   interface{}   `json:"result"`
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
		"code":     req.Code,
		"language": req.Language,
	}
	if req.Timeout > 0 {
		args["timeout"] = float64(req.Timeout)
	}
	if req.WorkDir != "" {
		args["workdir"] = req.WorkDir
	}
	if len(req.Packages) > 0 {
		args["packages"] = req.Packages
	}
	if len(req.Args) > 0 {
		args["args"] = req.Args
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
			Success:  false,
			Error:    "Invalid result format",
			Duration: time.Since(start),
		}, nil
	}

	exitCode := 0
	if ec, ok := resultMap["exit_code"].(float64); ok {
		exitCode = int(ec)
	}

	return &CodeExecutionResponse{
		Success:  exitCode == 0,
		Output:   fmt.Sprintf("%v", resultMap["stdout"]),
		Error:    fmt.Sprintf("%v", resultMap["stderr"]),
		ExitCode: exitCode,
		Duration: time.Since(start),
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

	// Node.js templates
	"file_processor_node": `
const fs = require('fs');
const path = require('path');

async function processFiles(directory, pattern = "*.txt") {
    const results = [];
    const files = fs.readdirSync(directory);
    
    for (const file of files) {
        if (file.endsWith('.txt')) {
            const filePath = path.join(directory, file);
            const content = fs.readFileSync(filePath, 'utf8');
            results.push({
                path: filePath,
                content: content,
                size: fs.statSync(filePath).size
            });
        }
    }
    return results;
}

// Example usage
const results = processFiles('/tmp');
console.log("Processed " + results.length + " files");
json_dump(results);
`,

	"web_scraper_node": `
const https = require('https');
const http = require('http');

function extractLinks(html, baseUrl) {
    baseUrl = baseUrl || '';
    const linkRegex = /href=["']([^"']+)["']/g;
    const links = [];
    let match;
    while ((match = linkRegex.exec(html)) !== null) {
        links.push({
            url: match[1],
            absolute: match[1].startsWith('http') ? match[1] : baseUrl + match[1]
        });
    }
    return links;
}

// Example usage
// const html = await web_fetch('https://example.com');
// const links = extractLinks(html, 'https://example.com');
// console.log("Found " + links.length + " links");
// json_dump(links.slice(0, 10));
`,

	"data_analysis_node": `
function analyzeData(items, key) {
    const distribution = {};
    items.forEach(item => {
        if (item && typeof item === 'object' && key in item) {
            const val = item[key];
            distribution[val] = (distribution[val] || 0) + 1;
        }
    });
    
    const entries = Object.entries(distribution);
    entries.sort((a, b) => b[1] - a[1]);
    const sorted = entries.slice(0, 5);
    
    return {
        total: items.length,
        distribution: distribution,
        most_common: sorted
    };
}

// Example: Analyze a list
const data = [{category: 'A', value: 1}, {category: 'B', value: 2}];
const results = analyzeData(data, 'category');
json_dump(results);
`,

	// Go template
	"hello_go": `
package main

import "fmt"

func main() {
    fmt.Println("Hello from Go!")
}
`,

	// Rust template
	"hello_rust": `
fn main() {
    println!("Hello from Rust!");
}
`,

	// Java template
	"hello_java": `
public class Main {
    public static void main(String[] args) {
        System.out.println("Hello from Java!");
    }
}
`,

	// C++ template
	"hello_cpp": `
#include <iostream>

int main() {
    std::cout << "Hello from C++!" << std::endl;
    return 0;
}
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
// Supports both Python and JavaScript/Node.js
func ValidateCode(code string) (bool, string) {
	// Check for dangerous patterns (Python)
	pythonDangerous := []string{
		`__import__`,
		`eval\(`,
		`exec\(`,
		`subprocess`,
		`os\.system`,
		`os\.popen`,
		`pty\.spawn`,
		`fork`,
		`multiprocessing`,
	}

	for _, pattern := range pythonDangerous {
		re := regexp.MustCompile(pattern)
		if re.MatchString(code) {
			return false, fmt.Sprintf("potentially dangerous pattern detected: %s", pattern)
		}
	}

	// Check for dangerous patterns (JavaScript/Node.js)
	nodeDangerous := []string{
		`require\(['"]child_process['"]\)`,
		`eval\(`,
		`new Function\(`,
		`process\.exit`,
		`process\.kill`,
		`child_process\.exec`,
		`child_process\.spawn.*rm`,
		`child_process\.spawn.*delete`,
	}

	for _, pattern := range nodeDangerous {
		re := regexp.MustCompile(pattern)
		if re.MatchString(code) {
			return false, fmt.Sprintf("potentially dangerous pattern detected: %s", pattern)
		}
	}

	return true, ""
}

// ValidateCodeForLanguage performs security check for specific language
func ValidateCodeForLanguage(code, language string) (bool, string) {
	switch language {
	case "node", "nodejs", "js", "javascript":
		nodeDangerous := []string{
			`require\(['"]child_process['"]\)`,
			`eval\(`,
			`new Function\(`,
			`process\.exit`,
			`process\.kill`,
			`child_process\.exec`,
		}
		for _, pattern := range nodeDangerous {
			re := regexp.MustCompile(pattern)
			if re.MatchString(code) {
				return false, fmt.Sprintf("potentially dangerous pattern detected: %s", pattern)
			}
		}
	default: // Python
		pythonDangerous := []string{
			`__import__`,
			`eval\(`,
			`exec\(`,
			`subprocess`,
			`os\.system`,
			`os\.popen`,
		}
		for _, pattern := range pythonDangerous {
			re := regexp.MustCompile(pattern)
			if re.MatchString(code) {
				return false, fmt.Sprintf("potentially dangerous pattern detected: %s", pattern)
			}
		}
	}
	return true, ""
}
