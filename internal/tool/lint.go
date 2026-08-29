package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

// ProjectType represents the detected project type
type ProjectType string

const (
	ProjectTypeGo      ProjectType = "go"
	ProjectTypeNode    ProjectType = "node"
	ProjectTypeRust    ProjectType = "rust"
	ProjectTypePython  ProjectType = "python"
	ProjectTypeUnknown ProjectType = "unknown"
)

// LintLevel represents the severity level of a lint issue
type LintLevel string

const (
	LintLevelError   LintLevel = "error"
	LintLevelWarning LintLevel = "warning"
	LintLevelInfo    LintLevel = "info"
)

// LintIssue represents a single lint issue
type LintIssue struct {
	FilePath string    `json:"file_path"`
	Line     int       `json:"line,omitempty"`
	Column   int       `json:"column,omitempty"`
	Message  string    `json:"message"`
	Level    LintLevel `json:"level"`
	Code     string    `json:"code,omitempty"`
	Fixable  bool      `json:"fixable"`
}

// LintResult represents the result of a lint check
type LintResult struct {
	FilePath string      `json:"file_path"`
	Issues   []LintIssue `json:"issues,omitempty"`
	Success  bool        `json:"success"`
	Error    string      `json:"error,omitempty"`
}

// LintOptions contains options for linting
type LintOptions struct {
	AutoFix     bool
	Format      bool
	StrictMode  bool
	TimeoutSecs int
}

// DefaultLintOptions returns default lint options
func DefaultLintOptions() LintOptions {
	return LintOptions{
		AutoFix:     false,
		Format:      false,
		StrictMode:  false,
		TimeoutSecs: 60,
	}
}

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
	_, err := toml.DecodeFile(path, &struct{}{})
	if err != nil {
		return []string{err.Error()}, nil
	}

	return nil, nil
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
		if err != nil {
			result.Error = err.Error()
		}
		if issues != nil {
			// Convert string issues to LintIssue structs
			for _, msg := range issues {
				result.Issues = append(result.Issues, LintIssue{
					FilePath: path,
					Message:  msg,
					Level:    LintLevelError,
				})
			}
		}
		results = append(results, result)
	}
	return results
}

// DetectProjectType detects the project type based on files in the directory
func DetectProjectType(dir string) ProjectType {
	// Check for Go
	if fileExists(filepath.Join(dir, "go.mod")) {
		return ProjectTypeGo
	}

	// Check for Node.js
	if fileExists(filepath.Join(dir, "package.json")) {
		return ProjectTypeNode
	}

	// Check for Rust
	if fileExists(filepath.Join(dir, "Cargo.toml")) {
		return ProjectTypeRust
	}

	// Check for Python
	if fileExists(filepath.Join(dir, "requirements.txt")) ||
		fileExists(filepath.Join(dir, "pyproject.toml")) ||
		fileExists(filepath.Join(dir, "setup.py")) ||
		fileExists(filepath.Join(dir, "Pipfile")) {
		return ProjectTypePython
	}

	return ProjectTypeUnknown
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// RunLinter runs the appropriate linter for the project type
func RunLinter(ctx context.Context, dir string, opts LintOptions) ([]LintResult, error) {
	projectType := DetectProjectType(dir)

	switch projectType {
	case ProjectTypeGo:
		return runGoLinters(ctx, dir, opts)
	case ProjectTypeNode:
		return runNodeLinters(ctx, dir, opts)
	case ProjectTypeRust:
		return runRustLinters(ctx, dir, opts)
	case ProjectTypePython:
		return runPythonLinters(ctx, dir, opts)
	default:
		return nil, fmt.Errorf("unsupported project type: no recognizable project files found")
	}
}

// runGoLinters runs Go-specific linters
func runGoLinters(ctx context.Context, dir string, opts LintOptions) ([]LintResult, error) {
	var results []LintResult

	// Run go vet
	if issues, err := runGoVet(ctx, dir); err == nil {
		results = append(results, issues...)
	}

	// Run gofmt check
	if issues, err := runGoFmt(ctx, dir, opts.Format); err == nil {
		results = append(results, issues...)
	}

	// Run golint if available
	if issues, err := runGoLint(ctx, dir); err == nil {
		results = append(results, issues...)
	}

	// Run golangci-lint if available
	if issues, err := runGolangCILint(ctx, dir, opts); err == nil {
		results = append(results, issues...)
	}

	return results, nil
}

func runGoVet(ctx context.Context, dir string) ([]LintResult, error) {
	cmd := exec.CommandContext(ctx, "go", "vet", "./...")
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := strings.TrimSpace(stderr.String())

	if output == "" && err == nil {
		return []LintResult{{FilePath: dir, Success: true}}, nil
	}

	// Parse go vet output
	var issues []LintIssue
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		// Parse format: file:line:message
		if idx := strings.Index(line, ":"); idx > 0 {
			parts := strings.SplitN(line[idx+1:], ":", 2)
			if len(parts) >= 2 {
				issues = append(issues, LintIssue{
					FilePath: strings.TrimSpace(line[:idx]),
					Message:  strings.TrimSpace(parts[1]),
					Level:    LintLevelWarning,
				})
			} else {
				issues = append(issues, LintIssue{
					FilePath: dir,
					Message:  line,
					Level:    LintLevelWarning,
				})
			}
		} else {
			issues = append(issues, LintIssue{
				FilePath: dir,
				Message:  line,
				Level:    LintLevelWarning,
			})
		}
	}

	return []LintResult{{FilePath: dir, Issues: issues, Success: len(issues) == 0}}, nil
}

func runGoFmt(ctx context.Context, dir string, autoFormat bool) ([]LintResult, error) {
	if autoFormat {
		// Format files
		cmd := exec.CommandContext(ctx, "gofmt", "-w", ".")
		cmd.Dir = dir
		cmd.Run()
		return []LintResult{{FilePath: dir, Success: true}}, nil
	}

	// Check formatting
	cmd := exec.CommandContext(ctx, "gofmt", "-l", ".")
	cmd.Dir = dir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	cmd.Run()
	output := strings.TrimSpace(stdout.String())

	if output == "" {
		return []LintResult{{FilePath: dir, Success: true}}, nil
	}

	// Files that need formatting
	files := strings.Split(output, "\n")
	var issues []LintIssue
	for _, file := range files {
		if file != "" {
			issues = append(issues, LintIssue{
				FilePath: filepath.Join(dir, file),
				Message:  "File needs formatting (run gofmt -w)",
				Level:    LintLevelWarning,
				Fixable:  true,
			})
		}
	}

	return []LintResult{{FilePath: dir, Issues: issues, Success: false}}, nil
}

func runGoLint(ctx context.Context, dir string) ([]LintResult, error) {
	// Check if golint is available
	if _, err := exec.LookPath("golint"); err != nil {
		return nil, nil // golint not installed, skip
	}

	cmd := exec.CommandContext(ctx, "golint", "./...")
	cmd.Dir = dir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	cmd.Run()
	output := strings.TrimSpace(stdout.String())

	if output == "" {
		return []LintResult{{FilePath: dir, Success: true}}, nil
	}

	// Parse golint output
	var issues []LintIssue
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		// Parse format: file:line:column: message
		parts := strings.SplitN(line, ":", 4)
		if len(parts) >= 4 {
			lineNum := 0
			fmt.Sscanf(parts[1], "%d", &lineNum)
			colNum := 0
			fmt.Sscanf(parts[2], "%d", &colNum)
			issues = append(issues, LintIssue{
				FilePath: strings.TrimSpace(parts[0]),
				Line:     lineNum,
				Column:   colNum,
				Message:  strings.TrimSpace(parts[3]),
				Level:    LintLevelWarning,
			})
		} else {
			issues = append(issues, LintIssue{
				FilePath: dir,
				Message:  line,
				Level:    LintLevelWarning,
			})
		}
	}

	return []LintResult{{FilePath: dir, Issues: issues, Success: len(issues) == 0}}, nil
}

func runGolangCILint(ctx context.Context, dir string, opts LintOptions) ([]LintResult, error) {
	// Check if golangci-lint is available
	if _, err := exec.LookPath("golangci-lint"); err != nil {
		return nil, nil // Not installed, skip
	}

	args := []string{"run", "--out-format=json"}
	if opts.AutoFix {
		args = append(args, "--fix")
	}

	cmd := exec.CommandContext(ctx, "golangci-lint", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	cmd.Run()

	// Parse JSON output
	var result struct {
		Issues []struct {
			FromLinter string `json:"FromLinter"`
			Text       string `json:"Text"`
			Severity   string `json:"Severity"`
			Pos        struct {
				Filename string `json:"Filename"`
				Line     int    `json:"Line"`
				Column   int    `json:"Column"`
			} `json:"Pos"`
		} `json:"Issues"`
	}

	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return nil, err
	}

	var issues []LintIssue
	for _, issue := range result.Issues {
		level := LintLevelWarning
		if issue.Severity == "error" {
			level = LintLevelError
		}
		issues = append(issues, LintIssue{
			FilePath: issue.Pos.Filename,
			Line:     issue.Pos.Line,
			Column:   issue.Pos.Column,
			Message:  fmt.Sprintf("%s: %s", issue.FromLinter, issue.Text),
			Level:    level,
			Code:     issue.FromLinter,
		})
	}

	return []LintResult{{FilePath: dir, Issues: issues, Success: len(issues) == 0}}, nil
}

// runNodeLinters runs Node.js/JavaScript/TypeScript linters
func runNodeLinters(ctx context.Context, dir string, opts LintOptions) ([]LintResult, error) {
	var results []LintResult

	// Check for ESLint
	if fileExists(filepath.Join(dir, ".eslintrc.js")) ||
		fileExists(filepath.Join(dir, ".eslintrc.json")) ||
		fileExists(filepath.Join(dir, ".eslintrc.yml")) ||
		fileExists(filepath.Join(dir, ".eslintrc.yaml")) ||
		fileExists(filepath.Join(dir, ".eslintrc")) ||
		fileExists(filepath.Join(dir, "eslint.config.js")) {
		if issues, err := runESLint(ctx, dir, opts); err == nil {
			results = append(results, issues...)
		}
	}

	// Check for Prettier
	if fileExists(filepath.Join(dir, ".prettierrc")) ||
		fileExists(filepath.Join(dir, ".prettierrc.json")) ||
		fileExists(filepath.Join(dir, "prettier.config.js")) {
		if issues, err := runPrettier(ctx, dir, opts.Format); err == nil {
			results = append(results, issues...)
		}
	}

	return results, nil
}

func runESLint(ctx context.Context, dir string, opts LintOptions) ([]LintResult, error) {
	// Check if eslint is available (local or global)
	eslintCmd := findNodeBinary(dir, "eslint")
	if eslintCmd == "" {
		return nil, fmt.Errorf("eslint not found")
	}

	args := []string{"--format", "json"}
	if opts.AutoFix {
		args = append(args, "--fix")
	}
	args = append(args, ".")

	cmd := exec.CommandContext(ctx, eslintCmd, args...)
	cmd.Dir = dir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	cmd.Run() // ESLint exits with non-zero if there are errors

	// Parse JSON output
	var eslintResults []struct {
		FilePath string `json:"filePath"`
		Messages []struct {
			RuleID   string `json:"ruleId"`
			Severity int    `json:"severity"`
			Message  string `json:"message"`
			Line     int    `json:"line"`
			Column   int    `json:"column"`
			Fix      *struct {
				Text string `json:"text"`
			} `json:"fix"`
		} `json:"messages"`
	}

	if err := json.Unmarshal(stdout.Bytes(), &eslintResults); err != nil {
		return nil, err
	}

	var allResults []LintResult
	for _, er := range eslintResults {
		var issues []LintIssue
		for _, msg := range er.Messages {
			level := LintLevelWarning
			if msg.Severity == 2 {
				level = LintLevelError
			}
			issues = append(issues, LintIssue{
				FilePath: er.FilePath,
				Line:     msg.Line,
				Column:   msg.Column,
				Message:  msg.Message,
				Level:    level,
				Code:     msg.RuleID,
				Fixable:  msg.Fix != nil,
			})
		}
		allResults = append(allResults, LintResult{
			FilePath: er.FilePath,
			Issues:   issues,
			Success:  len(issues) == 0,
		})
	}

	return allResults, nil
}

func runPrettier(ctx context.Context, dir string, autoFormat bool) ([]LintResult, error) {
	prettierCmd := findNodeBinary(dir, "prettier")
	if prettierCmd == "" {
		return nil, fmt.Errorf("prettier not found")
	}

	if autoFormat {
		cmd := exec.CommandContext(ctx, prettierCmd, "--write", ".")
		cmd.Dir = dir
		cmd.Run()
		return []LintResult{{FilePath: dir, Success: true}}, nil
	}

	// Check formatting
	cmd := exec.CommandContext(ctx, prettierCmd, "--check", ".")
	cmd.Dir = dir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	cmd.Run()
	output := strings.TrimSpace(stdout.String())

	if strings.Contains(output, "All matched files use Prettier code style") {
		return []LintResult{{FilePath: dir, Success: true}}, nil
	}

	// Files that need formatting
	var issues []LintIssue
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "[warn]") && strings.Contains(line, "Code style issues") {
			continue
		}
		if strings.HasPrefix(line, "[warn]") {
			file := strings.TrimPrefix(line, "[warn] ")
			file = strings.TrimSpace(file)
			if file != "" && !strings.Contains(file, "Code style issues") {
				issues = append(issues, LintIssue{
					FilePath: file,
					Message:  "File needs formatting (run prettier --write)",
					Level:    LintLevelWarning,
					Fixable:  true,
				})
			}
		}
	}

	return []LintResult{{FilePath: dir, Issues: issues, Success: len(issues) == 0}}, nil
}

// runRustLinters runs Rust-specific linters
func runRustLinters(ctx context.Context, dir string, opts LintOptions) ([]LintResult, error) {
	var results []LintResult

	// Run cargo clippy
	if issues, err := runClippy(ctx, dir, opts); err == nil {
		results = append(results, issues...)
	}

	// Run rustfmt
	if issues, err := runRustFmt(ctx, dir, opts.Format); err == nil {
		results = append(results, issues...)
	}

	return results, nil
}

func runClippy(ctx context.Context, dir string, opts LintOptions) ([]LintResult, error) {
	// Check if cargo is available
	if _, err := exec.LookPath("cargo"); err != nil {
		return nil, fmt.Errorf("cargo not found")
	}

	args := []string{"clippy", "--message-format=short"}
	if opts.StrictMode {
		args = append(args, "--", "-D", "warnings")
	}

	cmd := exec.CommandContext(ctx, "cargo", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	cmd.Run()
	output := strings.TrimSpace(stderr.String())

	if output == "" || !strings.Contains(output, "error") && !strings.Contains(output, "warning") {
		return []LintResult{{FilePath: dir, Success: true}}, nil
	}

	// Parse clippy output
	var issues []LintIssue
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		// Parse format: file:line:col: level: message
		if strings.Contains(line, ": error:") || strings.Contains(line, ": warning:") {
			parts := strings.SplitN(line, ":", 5)
			if len(parts) >= 4 {
				level := LintLevelWarning
				if strings.Contains(parts[3], "error") {
					level = LintLevelError
				}
				lineNum := 0
				colNum := 0
				fmt.Sscanf(parts[1], "%d", &lineNum)
				fmt.Sscanf(parts[2], "%d", &colNum)
				issues = append(issues, LintIssue{
					FilePath: strings.TrimSpace(parts[0]),
					Line:     lineNum,
					Column:   colNum,
					Message:  strings.TrimSpace(parts[4]),
					Level:    level,
				})
			}
		}
	}

	return []LintResult{{FilePath: dir, Issues: issues, Success: len(issues) == 0}}, nil
}

func runRustFmt(ctx context.Context, dir string, autoFormat bool) ([]LintResult, error) {
	if _, err := exec.LookPath("cargo"); err != nil {
		return nil, fmt.Errorf("cargo not found")
	}

	if autoFormat {
		cmd := exec.CommandContext(ctx, "cargo", "fmt")
		cmd.Dir = dir
		cmd.Run()
		return []LintResult{{FilePath: dir, Success: true}}, nil
	}

	// Check formatting
	cmd := exec.CommandContext(ctx, "cargo", "fmt", "--", "--check")
	cmd.Dir = dir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	cmd.Run()
	output := strings.TrimSpace(stdout.String())

	if output == "" {
		return []LintResult{{FilePath: dir, Success: true}}, nil
	}

	// Files that need formatting
	var issues []LintIssue
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if line != "" && strings.HasSuffix(line, "diff") {
			file := strings.TrimSuffix(line, " diff")
			issues = append(issues, LintIssue{
				FilePath: file,
				Message:  "File needs formatting (run cargo fmt)",
				Level:    LintLevelWarning,
				Fixable:  true,
			})
		}
	}

	return []LintResult{{FilePath: dir, Issues: issues, Success: len(issues) == 0}}, nil
}

// runPythonLinters runs Python-specific linters
func runPythonLinters(ctx context.Context, dir string, opts LintOptions) ([]LintResult, error) {
	var results []LintResult

	// Run pylint if available
	if issues, err := runPylint(ctx, dir); err == nil {
		results = append(results, issues...)
	}

	// Run flake8 if available
	if issues, err := runFlake8(ctx, dir); err == nil {
		results = append(results, issues...)
	}

	// Run black
	if issues, err := runBlack(ctx, dir, opts.Format); err == nil {
		results = append(results, issues...)
	}

	return results, nil
}

func runPylint(ctx context.Context, dir string) ([]LintResult, error) {
	if _, err := exec.LookPath("pylint"); err != nil {
		return nil, nil // pylint not installed
	}

	cmd := exec.CommandContext(ctx, "pylint", "--output-format=json", dir)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	cmd.Run() // pylint exits with non-zero if there are issues

	var pylintResults []struct {
		Type      string `json:"type"`
		Module    string `json:"module"`
		Obj       string `json:"obj"`
		Line      int    `json:"line"`
		Column    int    `json:"column"`
		Path      string `json:"path"`
		Symbol    string `json:"symbol"`
		Message   string `json:"message"`
		MessageID string `json:"message-id"`
	}

	if err := json.Unmarshal(stdout.Bytes(), &pylintResults); err != nil {
		return nil, err
	}

	var issues []LintIssue
	for _, pr := range pylintResults {
		level := LintLevelWarning
		if pr.Type == "error" || pr.Type == "fatal" {
			level = LintLevelError
		} else if pr.Type == "convention" || pr.Type == "refactor" {
			level = LintLevelInfo
		}
		issues = append(issues, LintIssue{
			FilePath: pr.Path,
			Line:     pr.Line,
			Column:   pr.Column,
			Message:  fmt.Sprintf("%s: %s", pr.Symbol, pr.Message),
			Level:    level,
			Code:     pr.MessageID,
		})
	}

	return []LintResult{{FilePath: dir, Issues: issues, Success: len(issues) == 0}}, nil
}

func runFlake8(ctx context.Context, dir string) ([]LintResult, error) {
	if _, err := exec.LookPath("flake8"); err != nil {
		return nil, nil // flake8 not installed
	}

	cmd := exec.CommandContext(ctx, "flake8", "--format=%(path)s:%(row)d:%(col)d: %(code)s %(text)s", dir)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	cmd.Run()
	output := strings.TrimSpace(stdout.String())

	if output == "" {
		return []LintResult{{FilePath: dir, Success: true}}, nil
	}

	var issues []LintIssue
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		// Parse format: file:line:col: code message
		parts := strings.SplitN(line, ":", 4)
		if len(parts) >= 4 {
			lineNum := 0
			colNum := 0
			fmt.Sscanf(parts[1], "%d", &lineNum)
			fmt.Sscanf(parts[2], "%d", &colNum)
			msg := strings.TrimSpace(parts[3])
			code := ""
			if spaceIdx := strings.Index(msg, " "); spaceIdx > 0 {
				code = msg[:spaceIdx]
				msg = strings.TrimSpace(msg[spaceIdx:])
			}
			issues = append(issues, LintIssue{
				FilePath: strings.TrimSpace(parts[0]),
				Line:     lineNum,
				Column:   colNum,
				Message:  msg,
				Level:    LintLevelWarning,
				Code:     code,
			})
		}
	}

	return []LintResult{{FilePath: dir, Issues: issues, Success: len(issues) == 0}}, nil
}

func runBlack(ctx context.Context, dir string, autoFormat bool) ([]LintResult, error) {
	if _, err := exec.LookPath("black"); err != nil {
		return nil, nil // black not installed
	}

	if autoFormat {
		cmd := exec.CommandContext(ctx, "black", dir)
		cmd.Run()
		return []LintResult{{FilePath: dir, Success: true}}, nil
	}

	// Check formatting
	cmd := exec.CommandContext(ctx, "black", "--check", "--diff", dir)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	cmd.Run()
	output := strings.TrimSpace(stdout.String())

	if output == "" || strings.Contains(output, "would reformat") {
		return []LintResult{{FilePath: dir, Success: true}}, nil
	}

	return []LintResult{{
		FilePath: dir,
		Issues: []LintIssue{{
			FilePath: dir,
			Message:  "Code needs formatting (run black)",
			Level:    LintLevelWarning,
			Fixable:  true,
		}},
		Success: false,
	}}, nil
}

// findNodeBinary finds a node binary in local node_modules or globally
func findNodeBinary(dir, binary string) string {
	// Check local node_modules
	localPath := filepath.Join(dir, "node_modules", ".bin", binary)
	if fileExists(localPath) {
		return localPath
	}

	// Check global
	if path, err := exec.LookPath(binary); err == nil {
		return path
	}

	return ""
}

// LintTool is a tool for running linters
type LintTool struct {
	*BaseTool
}

// NewLintTool creates a new lint tool
func NewLintTool() *LintTool {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the file or directory to lint",
			},
			"auto_fix": map[string]interface{}{
				"type":        "boolean",
				"description": "Automatically fix issues where possible",
				"default":     false,
			},
			"format": map[string]interface{}{
				"type":        "boolean",
				"description": "Format code",
				"default":     false,
			},
			"strict_mode": map[string]interface{}{
				"type":        "boolean",
				"description": "Enable strict mode (treat warnings as errors)",
				"default":     false,
			},
		},
		"required": []string{"path"},
	}

	return &LintTool{
		BaseTool: NewBaseTool(
			"lint",
			"Run linters on code files or projects to check for issues",
			schema,
		),
	}
}

// Execute runs the lint tool
func (t *LintTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	path, ok := params["path"].(string)
	if !ok || path == "" {
		return nil, fmt.Errorf("path parameter is required")
	}

	opts := DefaultLintOptions()
	if autoFix, ok := params["auto_fix"].(bool); ok {
		opts.AutoFix = autoFix
	}
	if format, ok := params["format"].(bool); ok {
		opts.Format = format
	}
	if strictMode, ok := params["strict_mode"].(bool); ok {
		opts.StrictMode = strictMode
	}

	// Check if path is a file or directory
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to stat path: %w", err)
	}

	var results []LintResult
	if info.IsDir() {
		results, err = RunLinter(ctx, path, opts)
		if err != nil {
			return nil, err
		}
	} else {
		// Single file linting
		issues, err := LintFile(path)
		if err != nil {
			return nil, err
		}
		lintIssues := make([]LintIssue, len(issues))
		for i, msg := range issues {
			lintIssues[i] = LintIssue{
				FilePath: path,
				Message:  msg,
				Level:    LintLevelError,
			}
		}
		results = []LintResult{{
			FilePath: path,
			Issues:   lintIssues,
			Success:  len(lintIssues) == 0,
		}}
	}

	return results, nil
}

// FormatTool is a tool for formatting code
type FormatTool struct {
	*BaseTool
}

// NewFormatTool creates a new format tool
func NewFormatTool() *FormatTool {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the file or directory to format",
			},
			"language": map[string]interface{}{
				"type":        "string",
				"description": "Language to format (go, rust, python, javascript, typescript). Auto-detected if not specified.",
			},
		},
		"required": []string{"path"},
	}

	return &FormatTool{
		BaseTool: NewBaseTool(
			"format_code",
			"Format code files using language-specific formatters",
			schema,
		),
	}
}

// Execute runs the format tool
func (t *FormatTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	path, ok := params["path"].(string)
	if !ok || path == "" {
		return nil, fmt.Errorf("path parameter is required")
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to stat path: %w", err)
	}

	language := ""
	if lang, ok := params["language"].(string); ok {
		language = lang
	}

	if language == "" {
		// Auto-detect language
		if info.IsDir() {
			projectType := DetectProjectType(path)
			switch projectType {
			case ProjectTypeGo:
				language = "go"
			case ProjectTypeNode:
				language = "javascript"
			case ProjectTypeRust:
				language = "rust"
			case ProjectTypePython:
				language = "python"
			}
		} else {
			ext := strings.ToLower(filepath.Ext(path))
			switch ext {
			case ".go":
				language = "go"
			case ".rs":
				language = "rust"
			case ".py":
				language = "python"
			case ".js", ".jsx":
				language = "javascript"
			case ".ts", ".tsx":
				language = "typescript"
			}
		}
	}

	switch language {
	case "go":
		if info.IsDir() {
			cmd := exec.CommandContext(ctx, "gofmt", "-w", ".")
			cmd.Dir = path
			err := cmd.Run()
			if err != nil {
				return nil, fmt.Errorf("gofmt failed: %w", err)
			}
		} else {
			cmd := exec.CommandContext(ctx, "gofmt", "-w", path)
			err := cmd.Run()
			if err != nil {
				return nil, fmt.Errorf("gofmt failed: %w", err)
			}
		}
	case "rust":
		cmd := exec.CommandContext(ctx, "rustfmt", path)
		if info.IsDir() {
			cmd = exec.CommandContext(ctx, "cargo", "fmt")
			cmd.Dir = path
		}
		err := cmd.Run()
		if err != nil {
			return nil, fmt.Errorf("rustfmt failed: %w", err)
		}
	case "python":
		if _, err := exec.LookPath("black"); err == nil {
			cmd := exec.CommandContext(ctx, "black", path)
			err := cmd.Run()
			if err != nil {
				return nil, fmt.Errorf("black failed: %w", err)
			}
		} else if _, err := exec.LookPath("autopep8"); err == nil {
			cmd := exec.CommandContext(ctx, "autopep8", "--in-place", "--aggressive", path)
			err := cmd.Run()
			if err != nil {
				return nil, fmt.Errorf("autopep8 failed: %w", err)
			}
		} else {
			return nil, fmt.Errorf("no Python formatter found (install black or autopep8)")
		}
	case "javascript", "typescript":
		prettierCmd := findNodeBinary(path, "prettier")
		if prettierCmd == "" {
			return nil, fmt.Errorf("prettier not found")
		}
		cmd := exec.CommandContext(ctx, prettierCmd, "--write", path)
		if info.IsDir() {
			cmd.Dir = path
		}
		err := cmd.Run()
		if err != nil {
			return nil, fmt.Errorf("prettier failed: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported language: %s", language)
	}

	return map[string]interface{}{
		"success":  true,
		"path":     path,
		"language": language,
	}, nil
}
