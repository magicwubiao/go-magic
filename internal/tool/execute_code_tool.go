package tool

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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

// ExecuteCodeTool provides in-process multi-language code execution
// with package management support
type ExecuteCodeTool struct {
	config     *CodeExecutorConfig
	codingMode bool
}

// NewExecuteCodeTool creates a new execute_code tool
func NewExecuteCodeTool() *ExecuteCodeTool {
	return &ExecuteCodeTool{
		config: DefaultCodeExecutorConfig(),
	}
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
	return `Execute code in multiple programming languages.
Code runs in an isolated subprocess with timeout and memory limits.

NOTE: code cannot call back into agent tools — the subprocess is isolated
and there is no tool bridge. Use execute_command, read_file etc. as
separate tool calls instead.

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
				"description": "Reserved. Tool names intended for use inside code; the tool bridge is not wired up in the current build, so this has no effect.",
			},
			"tools_output": map[string]interface{}{
				"type":        "boolean",
				"description": "Reserved. Intended to include tool call outputs in the result; the tool bridge is not wired up in the current build, so this has no effect.",
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

	// Create execution context
	execCtx := &codeExecutionContext{
		lang:     lang,
		code:     code,
		workDir:  workDir,
		packages: packages,
		progArgs: progArgs,
		timeout:  timeout,
	}

	return t.executeWithLanguage(ctx, execCtx)
}

// codeExecutionContext holds execution parameters
type codeExecutionContext struct {
	lang     Language
	code     string
	workDir  string
	packages []string
	progArgs []string
	timeout  time.Duration
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
	// Resolve a real interpreter. Hardcoding "python3" used to break Windows:
	// there is no python3 command there (only the Microsoft Store shim, which
	// exits 9009), so every call failed with "command not found".
	bin, prefix, ok := ResolvePythonCommand()
	if !ok {
		return nil, fmt.Errorf("python interpreter not found (tried python, python3, py -3); install Python 3 and make sure it is on PATH")
	}

	// Install packages if specified
	if len(execCtx.packages) > 0 {
		pkgArgs := append(append([]string{}, prefix...), "-m", "pip", "install", "--quiet")
		pkgArgs = append(pkgArgs, execCtx.packages...)
		cmd := exec.CommandContext(ctx, bin, pkgArgs...)
		if output, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("failed to install packages: %w\n%s", err, decodeConsoleOutput(output))
		}
	}

	return t.executeScript(ctx, execCtx.code, bin, prefix, ".py", execCtx.workDir, execCtx.timeout, execCtx.progArgs)
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

	return t.executeScript(ctx, execCtx.code, "node", nil, ".js", tempDir, execCtx.timeout, execCtx.progArgs)
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

	// Write TypeScript file
	tsFile := filepath.Join(tempDir, "script.ts")
	if err := os.WriteFile(tsFile, []byte(execCtx.code), 0644); err != nil {
		return nil, fmt.Errorf("failed to write TypeScript file: %w", err)
	}

	// Execute with ts-node
	cmd := exec.CommandContext(ctx, filepath.Join(tempDir, "node_modules", ".bin", "ts-node"), tsFile)
	cmd.Dir = tempDir
	cmd.Env = append(os.Environ(), "NODE_PATH="+filepath.Join(tempDir, "node_modules"))

	return t.runCommand(ctx, cmd, execCtx.timeout)
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
		return nil, fmt.Errorf("failed to initialize go module: %w\n%s", err, decodeConsoleOutput(output))
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
		return nil, fmt.Errorf("failed to tidy go modules: %w\n%s", err, decodeConsoleOutput(output))
	}

	// Run the code
	cmd = exec.CommandContext(ctx, "go", "run", ".")
	cmd.Dir = tempDir
	cmd.Args = append(cmd.Args, execCtx.progArgs...)

	return t.runCommand(ctx, cmd, execCtx.timeout)
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

	return t.runCommand(ctx, cmd, execCtx.timeout)
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
			return nil, fmt.Errorf("failed to download dependencies: %w\n%s", err, decodeConsoleOutput(output))
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
		return nil, fmt.Errorf("compilation failed: %w\n%s", err, decodeConsoleOutput(output))
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

	return t.runCommand(ctx, runCmd, execCtx.timeout)
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
		return nil, fmt.Errorf("compilation failed: %w\n%s", err, decodeConsoleOutput(output))
	}

	// Run
	runCmd := exec.CommandContext(ctx, outputFile)
	runCmd.Dir = tempDir
	runCmd.Args = append(runCmd.Args, execCtx.progArgs...)

	return t.runCommand(ctx, runCmd, execCtx.timeout)
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
		return fmt.Errorf("failed to install npm packages: %w\n%s", err, decodeConsoleOutput(output))
	}
	return nil
}

// executeScript executes a script file. prefixArgs are inserted before the
// script path — e.g. Windows' "py" launcher needs "-3" there. Pass nil for
// interpreters invoked directly (node, python.exe).
func (t *ExecuteCodeTool) executeScript(ctx context.Context, code, executable string, prefixArgs []string, fileExt, workDir string, timeout time.Duration, args []string) (interface{}, error) {
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
	cmdArgs := append(append([]string{}, prefixArgs...), tmpFile.Name())
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.CommandContext(ctx, executable, cmdArgs...)
	cmd.Dir = workDir

	return t.runCommand(ctx, cmd, timeout)
}

// runCommand runs a command with timeout. The command is killed when either
// its own `timeout` elapses or the parent context (session/turn deadline) is
// finished — whichever comes first. All three outcomes are returned as result
// payloads (not Go errors) so the agent can keep the tool-call/reply
// conversation flowing instead of treating a cancellation as a hard failure.
// syncBuffer 是互斥保护的字节缓冲。exec.Cmd 在 Stdout/Stderr 为非 *os.File 时
// 会启动内部 io 拷贝 goroutine 持续写入；普通 bytes.Buffer 非并发安全，任何
// 并发读取都会构成数据竞争（CI -race 已实测命中）。加锁后读写两侧均安全。
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	// 子进程输出按控制台编码解码：Python 在 Windows 管道下默认按 ANSI 代码页
	// （中文系统即 GBK）写 stdout，直接 string() 会得到替换字符。
	return decodeConsoleOutput(b.buf.Bytes())
}

// defaultCommandWaitDelay 进程被 Kill 后等待管道排空的上限。
// 若子进程的子孙进程继承了 stdout/stderr 管道且拒不退出，WaitDelay 到期后
// cmd.Wait 强制关闭管道返回，避免整条执行路径悬挂在收割阶段。
const defaultCommandWaitDelay = 3 * time.Second

// runCommand 执行已构造好的 exec.Cmd，并以 payload 形式（而非 Go error）
// 报告取消/超时，保证上层对话循环可以继续而非中断。
//
// 并发正确性说明（修复 CI TestExecuteCodeContextCancel 的两处 data race）：
//
//  1. exec.Cmd 结构体文档明确「not safe for concurrent use」。旧实现里
//     select 的 ctx.Done/timeout 分支直接读 cmd.Process 字段并调用 Kill，
//     与后台 goroutine 中 Run()→Start() 对该字段的写入构成竞争。
//     新实现改为显式 Start()：主协程完成 Start 后把 *os.Process 快照进
//     局部变量交给 killer goroutine —— os.Process 的 Kill 是 syscall 级
//     线程安全操作，且此后无人再写 Cmd 结构体字段，竞争面彻底消失。
//
//  2. 旧实现在 Kill 之后不等待 Run() 返回就读 stdout/stderr
//     （bytes.Buffer 与 Cmd 内部 IO 拷贝 goroutine 并发读写）。
//     新实现令所有终止路径统一收敛到阻塞的 cmd.Wait()：Wait 返回即保证
//     子进程已被收割且内部 IO goroutine 全部结束，随后读缓冲绝对安全；
//     配合 syncBuffer 双保险，即使未来引入流式读取也不再踩雷。
func (t *ExecuteCodeTool) runCommand(ctx context.Context, cmd *exec.Cmd, timeout time.Duration) (interface{}, error) {
	stdout, stderr := &syncBuffer{}, &syncBuffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if timeout <= 0 {
		timeout = 5 * time.Minute // 防御：历史调用方始终传 >0
	}
	if cmd.WaitDelay <= 0 {
		cmd.WaitDelay = defaultCommandWaitDelay
	}

	// 已经过期的 ctx 直接快失败，不启动子进程：上层（turn 超时/用户取消）已经
	// 放弃这一轮了，再拉一个解释器起来纯属浪费——Windows 上首次进程创建还要付
	// 冷启动代价（实测首个 python 子进程可达数秒），会把"取消延迟"从 ~0.8s 拖到
	// 十几秒。返回值仍是 payload 形态，与取消路径的文案保持一致。
	if err := ctx.Err(); err != nil {
		return map[string]interface{}{
			"stdout":    "",
			"stderr":    "",
			"exit_code": -1,
			"error":     fmt.Sprintf("execution cancelled: %v", err),
		}, nil
	}

	// Start（非 Run）：成功后进程句柄可用；失败镜像旧行为以 payload 返回。
	if err := cmd.Start(); err != nil {
		return map[string]interface{}{
			"stdout":    "",
			"stderr":    "",
			"exit_code": -1,
			"error":     err.Error(),
		}, nil
	}
	proc := cmd.Process // 仅此处读取一次；Start 已先行发生

	// Killer：唯一的终止触发器，只持有 Process 快照，绝不触碰 exec.Cmd。
	stop := make(chan struct{})
	reasonCh := make(chan string, 1)
	go func() {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-stop:
		case <-ctx.Done():
			_ = proc.Kill() // 幂等：进程已退出时为 ErrProcessDone，忽略
			reasonCh <- fmt.Sprintf("execution cancelled: %v", ctx.Err())
		case <-timer.C:
			_ = proc.Kill()
			reasonCh <- "execution timed out"
		}
	}()

	// 统一收割点：正常退出 / 被 Kill 的子进程都在此回收，
	// 返回后内部 IO 拷贝 goroutine 必然已结束 → 读缓冲无竞争。
	waitErr := cmd.Wait()
	close(stop)

	result := map[string]interface{}{
		"stdout":    stdout.String(),
		"stderr":    stderr.String(),
		"exit_code": 0,
	}

	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			result["exit_code"] = exitErr.ExitCode()
		} else {
			result["error"] = waitErr.Error()
			result["exit_code"] = -1
		}
	}

	// 取消/超时语义优先：归一化为 payload 级错误（与旧版文案完全一致）。
	select {
	case reason := <-reasonCh:
		result["error"] = reason
		result["exit_code"] = -1
	default:
	}

	return result, nil
}
