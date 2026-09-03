package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/config"
)

// Color styles using lipgloss
var (
	cyanStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	greenStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	yellowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	redStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	blueStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	whiteStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
)

var codingCmd = &cobra.Command{
	Use:   "coding",
	Short: "Coding assistant commands",
	Long:  "A set of coding assistant commands for project initialization, code analysis, linting, testing, building, and debugging.",
}

var codingInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize coding environment",
	Long:  "Initialize the coding environment for the current project, detecting project type and setting up necessary files.",
	RunE:  runCodingInit,
}

var codingAnalyzeCmd = &cobra.Command{
	Use:   "analyze [file]",
	Short: "Analyze code file",
	Long:  "Analyze a code file for syntax errors, potential bugs, performance issues, and best practices.",
	Args:  cobra.ExactArgs(1),
	RunE:  runCodingAnalyze,
}

var codingLintCmd = &cobra.Command{
	Use:   "lint [file]",
	Short: "Lint code file",
	Long:  "Run linter on a code file or directory to check code style and potential issues.",
	Args:  cobra.ExactArgs(1),
	RunE:  runCodingLint,
}

var codingTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Run tests",
	Long:  "Run tests for the current project based on project type.",
	RunE:  runCodingTest,
}

var codingBuildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build project",
	Long:  "Build the current project based on project type.",
	RunE:  runCodingBuild,
}

var codingDebugCmd = &cobra.Command{
	Use:   "debug",
	Short: "Launch debug mode",
	Long:  "Launch interactive debug mode with enhanced coding assistance.",
	RunE:  runCodingDebug,
}

func init() {
	rootCmd.AddCommand(codingCmd)
	codingCmd.AddCommand(codingInitCmd)
	codingCmd.AddCommand(codingAnalyzeCmd)
	codingCmd.AddCommand(codingLintCmd)
	codingCmd.AddCommand(codingTestCmd)
	codingCmd.AddCommand(codingBuildCmd)
	codingCmd.AddCommand(codingDebugCmd)

	// Flags for init command
	codingInitCmd.Flags().BoolP("force", "f", false, "Force reinitialize even if already initialized")
	codingInitCmd.Flags().StringP("type", "t", "", "Project type (go, python, node, rust, java, cpp)")

	// Flags for analyze command
	codingAnalyzeCmd.Flags().BoolP("ai", "a", false, "Use AI for deep analysis")
	codingAnalyzeCmd.Flags().BoolP("fix", "f", false, "Suggest fixes for issues found")

	// Flags for lint command
	codingLintCmd.Flags().BoolP("fix", "f", false, "Auto-fix issues when possible")
	codingLintCmd.Flags().StringP("format", "F", "", "Output format (text, json, xml)")

	// Flags for test command
	// 注意：本命令的本地标志不要占用 -p。全局持久化标志 --profile 已占用 -p，
	// cobra 合并 flagset 时会因简写重复直接 panic，且改用长名也无法绕过。
	codingTestCmd.Flags().BoolP("verbose", "v", false, "Verbose output")
	codingTestCmd.Flags().String("pattern", "", "Test pattern to run")
	codingTestCmd.Flags().BoolP("coverage", "c", false, "Generate coverage report")

	// Flags for build command
	codingBuildCmd.Flags().StringP("output", "o", "", "Output binary name")
	codingBuildCmd.Flags().StringP("target", "t", "", "Build target (e.g., linux/amd64)")
	codingBuildCmd.Flags().BoolP("release", "r", false, "Release build with optimizations")

	// Flags for debug command
	codingDebugCmd.Flags().BoolP("chat", "c", false, "Start in chat mode with coding context")
}

// ProjectType represents the detected project type
type ProjectType string

const (
	ProjectTypeGo      ProjectType = "go"
	ProjectTypePython  ProjectType = "python"
	ProjectTypeNode    ProjectType = "node"
	ProjectTypeRust    ProjectType = "rust"
	ProjectTypeJava    ProjectType = "java"
	ProjectTypeCpp     ProjectType = "cpp"
	ProjectTypeUnknown ProjectType = "unknown"
)

// detectProjectType detects the project type based on files in the directory
func detectProjectType(dir string) ProjectType {
	files, err := os.ReadDir(dir)
	if err != nil {
		return ProjectTypeUnknown
	}

	for _, file := range files {
		name := file.Name()
		switch {
		case name == "go.mod":
			return ProjectTypeGo
		case name == "package.json":
			return ProjectTypeNode
		case name == "Cargo.toml":
			return ProjectTypeRust
		case name == "pom.xml" || name == "build.gradle":
			return ProjectTypeJava
		case name == "CMakeLists.txt" || name == "Makefile":
			return ProjectTypeCpp
		case strings.HasSuffix(name, ".py") || name == "requirements.txt" || name == "setup.py" || name == "pyproject.toml":
			return ProjectTypePython
		}
	}

	return ProjectTypeUnknown
}

// runCodingInit initializes the coding environment
func runCodingInit(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")
	projectType, _ := cmd.Flags().GetString("type")

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Detect or use provided project type
	var pType ProjectType
	if projectType != "" {
		pType = ProjectType(projectType)
	} else {
		pType = detectProjectType(cwd)
	}

	if pType == ProjectTypeUnknown {
		return fmt.Errorf("could not detect project type. Use --type flag to specify (go, python, node, rust, java, cpp)")
	}

	fmt.Println(cyanStyle.Render(fmt.Sprintf("Initializing coding environment for %s project...", pType)))

	// Check if already initialized
	magicDir := filepath.Join(cwd, ".magic")
	if _, err := os.Stat(magicDir); err == nil && !force {
		return fmt.Errorf("coding environment already initialized. Use --force to reinitialize")
	}

	// Create .magic directory
	if err := os.MkdirAll(magicDir, 0755); err != nil {
		return fmt.Errorf("failed to create .magic directory: %w", err)
	}

	// Initialize based on project type
	switch pType {
	case ProjectTypeGo:
		if err := initGoProject(cwd, force); err != nil {
			return err
		}
	case ProjectTypePython:
		if err := initPythonProject(cwd, force); err != nil {
			return err
		}
	case ProjectTypeNode:
		if err := initNodeProject(cwd, force); err != nil {
			return err
		}
	case ProjectTypeRust:
		if err := initRustProject(cwd, force); err != nil {
			return err
		}
	case ProjectTypeJava:
		if err := initJavaProject(cwd, force); err != nil {
			return err
		}
	case ProjectTypeCpp:
		if err := initCppProject(cwd, force); err != nil {
			return err
		}
	}

	// Create .gitignore if it doesn't exist
	gitignorePath := filepath.Join(cwd, ".gitignore")
	if _, err := os.Stat(gitignorePath); os.IsNotExist(err) {
		if err := createGitignore(pType, gitignorePath); err != nil {
			fmt.Println(yellowStyle.Render(fmt.Sprintf("Warning: could not create .gitignore: %v", err)))
		}
	}

	// Create README.md if it doesn't exist
	readmePath := filepath.Join(cwd, "README.md")
	if _, err := os.Stat(readmePath); os.IsNotExist(err) {
		if err := createReadme(pType, readmePath); err != nil {
			fmt.Println(yellowStyle.Render(fmt.Sprintf("Warning: could not create README.md: %v", err)))
		}
	}

	fmt.Println(greenStyle.Render("Coding environment initialized successfully!"))
	fmt.Println(whiteStyle.Render(fmt.Sprintf("Project type: %s", pType)))
	fmt.Println(whiteStyle.Render(fmt.Sprintf("Configuration: %s", magicDir)))

	return nil
}

// initGoProject initializes a Go project
func initGoProject(dir string, force bool) error {
	// Check if go.mod exists
	goModPath := filepath.Join(dir, "go.mod")
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		// Initialize go module
		cmd := exec.Command("go", "mod", "init", filepath.Base(dir))
		cmd.Dir = dir
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to initialize go module: %w\n%s", err, output)
		}
		fmt.Println(greenStyle.Render("Created go.mod"))
	}

	// Create main.go if it doesn't exist
	mainPath := filepath.Join(dir, "main.go")
	if _, err := os.Stat(mainPath); os.IsNotExist(err) {
		content := `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}
`
		if err := os.WriteFile(mainPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to create main.go: %w", err)
		}
		fmt.Println(greenStyle.Render("Created main.go"))
	}

	return nil
}

// initPythonProject initializes a Python project
func initPythonProject(dir string, force bool) error {
	// Create virtual environment
	venvPath := filepath.Join(dir, "venv")
	if _, err := os.Stat(venvPath); os.IsNotExist(err) || force {
		cmd := exec.Command("python3", "-m", "venv", "venv")
		cmd.Dir = dir
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to create virtual environment: %w\n%s", err, output)
		}
		fmt.Println(greenStyle.Render("Created virtual environment"))
	}

	// Create requirements.txt if it doesn't exist
	reqPath := filepath.Join(dir, "requirements.txt")
	if _, err := os.Stat(reqPath); os.IsNotExist(err) {
		content := `# Project dependencies
`
		if err := os.WriteFile(reqPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to create requirements.txt: %w", err)
		}
		fmt.Println(greenStyle.Render("Created requirements.txt"))
	}

	// Create main.py if it doesn't exist
	mainPath := filepath.Join(dir, "main.py")
	if _, err := os.Stat(mainPath); os.IsNotExist(err) {
		content := `#!/usr/bin/env python3
"""Main module."""


def main():
    """Main entry point."""
    print("Hello, World!")


if __name__ == "__main__":
    main()
`
		if err := os.WriteFile(mainPath, []byte(content), 0755); err != nil {
			return fmt.Errorf("failed to create main.py: %w", err)
		}
		fmt.Println(greenStyle.Render("Created main.py"))
	}

	return nil
}

// initNodeProject initializes a Node.js project
func initNodeProject(dir string, force bool) error {
	// Check if package.json exists
	pkgPath := filepath.Join(dir, "package.json")
	if _, err := os.Stat(pkgPath); os.IsNotExist(err) {
		// Initialize npm project
		cmd := exec.Command("npm", "init", "-y")
		cmd.Dir = dir
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to initialize npm project: %w\n%s", err, output)
		}
		fmt.Println(greenStyle.Render("Created package.json"))
	}

	// Create index.js if it doesn't exist
	indexPath := filepath.Join(dir, "index.js")
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		content := `#!/usr/bin/env node
"use strict";

function main() {
    console.log("Hello, World!");
}

main();
`
		if err := os.WriteFile(indexPath, []byte(content), 0755); err != nil {
			return fmt.Errorf("failed to create index.js: %w", err)
		}
		fmt.Println(greenStyle.Render("Created index.js"))
	}

	return nil
}

// initRustProject initializes a Rust project
func initRustProject(dir string, force bool) error {
	// Check if Cargo.toml exists
	cargoPath := filepath.Join(dir, "Cargo.toml")
	if _, err := os.Stat(cargoPath); os.IsNotExist(err) {
		// Initialize cargo project
		cmd := exec.Command("cargo", "init")
		cmd.Dir = dir
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to initialize cargo project: %w\n%s", err, output)
		}
		fmt.Println(greenStyle.Render("Initialized Cargo project"))
	}

	return nil
}

// initJavaProject initializes a Java project
func initJavaProject(dir string, force bool) error {
	// Create src directory structure
	srcPath := filepath.Join(dir, "src", "main", "java")
	if err := os.MkdirAll(srcPath, 0755); err != nil {
		return fmt.Errorf("failed to create src directory: %w", err)
	}

	// Create Main.java if it doesn't exist
	mainPath := filepath.Join(srcPath, "Main.java")
	if _, err := os.Stat(mainPath); os.IsNotExist(err) {
		content := `public class Main {
    public static void main(String[] args) {
        System.out.println("Hello, World!");
    }
}
`
		if err := os.WriteFile(mainPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to create Main.java: %w", err)
		}
		fmt.Println(greenStyle.Render("Created Main.java"))
	}

	return nil
}

// initCppProject initializes a C/C++ project
func initCppProject(dir string, force bool) error {
	// Create src directory
	srcPath := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcPath, 0755); err != nil {
		return fmt.Errorf("failed to create src directory: %w", err)
	}

	// Create main.cpp if it doesn't exist
	mainPath := filepath.Join(srcPath, "main.cpp")
	if _, err := os.Stat(mainPath); os.IsNotExist(err) {
		content := `#include <iostream>

int main() {
    std::cout << "Hello, World!" << std::endl;
    return 0;
}
`
		if err := os.WriteFile(mainPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to create main.cpp: %w", err)
		}
		fmt.Println(greenStyle.Render("Created main.cpp"))
	}

	// Create CMakeLists.txt if it doesn't exist
	cmakePath := filepath.Join(dir, "CMakeLists.txt")
	if _, err := os.Stat(cmakePath); os.IsNotExist(err) {
		content := `cmake_minimum_required(VERSION 3.10)
project(MyProject)

set(CMAKE_CXX_STANDARD 14)

add_executable(myapp src/main.cpp)
`
		if err := os.WriteFile(cmakePath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to create CMakeLists.txt: %w", err)
		}
		fmt.Println(greenStyle.Render("Created CMakeLists.txt"))
	}

	return nil
}

// createGitignore creates a .gitignore file for the project type
func createGitignore(pType ProjectType, path string) error {
	var content string
	switch pType {
	case ProjectTypeGo:
		content = `# Binaries for programs and plugins
*.exe
*.exe~
*.dll
*.so
*.dylib

# Test binary, built with go test -c
*.test

# Output of the go coverage tool
*.out

# Dependency directories
vendor/

# Go workspace file
go.work

# IDE
.idea/
.vscode/
*.swp
*.swo
*~

# OS
.DS_Store
Thumbs.db
`
	case ProjectTypePython:
		content = `# Byte-compiled / optimized / DLL files
__pycache__/
*.py[cod]
*$py.class

# C extensions
*.so

# Distribution / packaging
.Python
build/
develop-eggs/
dist/
downloads/
eggs/
.eggs/
lib/
lib64/
parts/
sdist/
var/
wheels/
*.egg-info/
.installed.cfg
*.egg

# PyInstaller
*.manifest
*.spec

# Installer logs
pip-log.txt
pip-delete-this-directory.txt

# Unit test / coverage reports
htmlcov/
.tox/
.nox/
.coverage
.coverage.*
.cache
nosetests.xml
coverage.xml
*.cover
.hypothesis/
.pytest_cache/

# Virtual environments
venv/
env/
ENV/
env.bak/
venv.bak/

# IDE
.idea/
.vscode/
*.swp
*.swo
*~

# OS
.DS_Store
Thumbs.db
`
	case ProjectTypeNode:
		content = `# Dependencies
node_modules/

# Production build
build/
dist/

# Logs
logs
*.log
npm-debug.log*
yarn-debug.log*
yarn-error.log*

# Runtime data
pids
*.pid
*.seed
*.pid.lock

# Coverage directory
coverage/

# Environment variables
.env
.env.local
.env.*.local

# IDE
.idea/
.vscode/
*.swp
*.swo
*~

# OS
.DS_Store
Thumbs.db
`
	case ProjectTypeRust:
		content = `# Generated by Cargo
/target/

# Remove Cargo.lock from gitignore if creating an executable
Cargo.lock

# IDE
.idea/
.vscode/
*.swp
*.swo
*~

# OS
.DS_Store
Thumbs.db
`
	case ProjectTypeJava:
		content = `# Compiled class files
*.class

# Log files
*.log

# Package files
*.jar
*.war
*.nar
*.ear
*.zip
*.tar.gz
*.rar

# Maven
target/
pom.xml.tag
pom.xml.releaseBackup
pom.xml.versionsBackup
pom.xml.next
release.properties
dependency-reduced-pom.xml

# Gradle
.gradle/
build/

# IDE
.idea/
*.iml
*.ipr
*.iws
.classpath
.project
.settings/
.vscode/

# OS
.DS_Store
Thumbs.db
`
	case ProjectTypeCpp:
		content = `# Prerequisites
*.d

# Compiled Object files
*.slo
*.lo
*.o
*.obj

# Precompiled Headers
*.gch
*.pch

# Compiled Dynamic libraries
*.so
*.dylib
*.dll

# Fortran module files
*.mod
*.smod

# Compiled Static libraries
*.lai
*.la
*.a
*.lib

# Executables
*.exe
*.out
*.app

# Build directories
build/
cmake-build-*/

# IDE
.idea/
.vscode/
*.swp
*.swo
*~

# OS
.DS_Store
Thumbs.db
`
	}

	if content != "" {
		return os.WriteFile(path, []byte(content), 0644)
	}
	return nil
}

// createReadme creates a README.md file for the project type
func createReadme(pType ProjectType, path string) error {
	projectName := filepath.Base(filepath.Dir(path))
	content := fmt.Sprintf(`# %s

%s project created with magic coding.

## Getting Started

`, projectName, strings.ToUpper(string(pType)))

	switch pType {
	case ProjectTypeGo:
		content += `### Prerequisites
- Go 1.18 or later

### Build
` + "```bash\ngo build -o myapp\n```\n\n### Run\n" + "```bash\ngo run .\n```\n\n### Test\n" + "```bash\ngo test ./...\n```\n"
	case ProjectTypePython:
		content += `### Prerequisites
- Python 3.8 or later

### Setup
` + "```bash\npython3 -m venv venv\nsource venv/bin/activate  # On Windows: venv\\Scripts\\activate\npip install -r requirements.txt\n```\n\n### Run\n" + "```bash\npython main.py\n```\n"
	case ProjectTypeNode:
		content += `### Prerequisites
- Node.js 16 or later

### Setup
` + "```bash\nnpm install\n```\n\n### Run\n" + "```bash\nnode index.js\n```\n\n### Test\n" + "```bash\nnpm test\n```\n"
	case ProjectTypeRust:
		content += `### Prerequisites
- Rust toolchain

### Build
` + "```bash\ncargo build\n```\n\n### Run\n" + "```bash\ncargo run\n```\n\n### Test\n" + "```bash\ncargo test\n```\n"
	case ProjectTypeJava:
		content += `### Prerequisites
- JDK 11 or later
- Maven or Gradle

### Build with Maven
` + "```bash\nmvn compile\n```\n\n### Run\n" + "```bash\nmvn exec:java -Dexec.mainClass=Main\n```\n\n### Test\n" + "```bash\nmvn test\n```\n"
	case ProjectTypeCpp:
		content += `### Prerequisites
- CMake 3.10 or later
- C++14 compatible compiler

### Build
` + "```bash\nmkdir build && cd build\ncmake ..\nmake\n```\n\n### Run\n" + "```bash\n./myapp\n```\n"
	}

	content += `
## Project Structure

- Source files in the main directory
- Configuration files for the build system
- .magic/ - Magic coding configuration

## License

Add your license information here.
`

	return os.WriteFile(path, []byte(content), 0644)
}

// runCodingAnalyze analyzes a code file
func runCodingAnalyze(cmd *cobra.Command, args []string) error {
	filePath := args[0]
	useAI, _ := cmd.Flags().GetBool("ai")
	suggestFix, _ := cmd.Flags().GetBool("fix")

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Errorf("file not found: %s", filePath)
	}

	fmt.Println(cyanStyle.Render(fmt.Sprintf("Analyzing: %s", filePath)))

	// Detect language from file extension
	ext := filepath.Ext(filePath)
	language := detectLanguage(ext)

	// Read file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Basic analysis
	issues := analyzeCode(string(content), language, filePath)

	if len(issues) > 0 {
		fmt.Println(yellowStyle.Render(fmt.Sprintf("Found %d issue(s):", len(issues))))
		for _, issue := range issues {
			switch issue.Severity {
			case "error":
				fmt.Println(redStyle.Render(fmt.Sprintf("  [ERROR] Line %d: %s", issue.Line, issue.Message)))
			case "warning":
				fmt.Println(yellowStyle.Render(fmt.Sprintf("  [WARN] Line %d: %s", issue.Line, issue.Message)))
			case "info":
				fmt.Println(blueStyle.Render(fmt.Sprintf("  [INFO] Line %d: %s", issue.Line, issue.Message)))
			}
			if issue.Suggestion != "" {
				fmt.Println(whiteStyle.Render(fmt.Sprintf("    Suggestion: %s", issue.Suggestion)))
			}
		}
	} else {
		fmt.Println(greenStyle.Render("No issues found!"))
	}

	// AI-powered deep analysis
	if useAI {
		fmt.Println(cyanStyle.Render("\nPerforming AI-powered deep analysis..."))
		if err := runAIAnalysis(filePath, string(content), language, suggestFix); err != nil {
			fmt.Println(yellowStyle.Render(fmt.Sprintf("AI analysis failed: %v", err)))
		}
	}

	return nil
}

// Issue represents a code issue
type Issue struct {
	Line       int
	Severity   string // error, warning, info
	Message    string
	Suggestion string
}

// detectLanguage detects programming language from file extension
func detectLanguage(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js":
		return "javascript"
	case ".ts":
		return "typescript"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".cpp", ".cc", ".cxx":
		return "cpp"
	case ".c":
		return "c"
	case ".h", ".hpp":
		return "cpp"
	default:
		return "unknown"
	}
}

// analyzeCode performs basic code analysis
func analyzeCode(content, language, filePath string) []Issue {
	var issues []Issue
	lines := strings.Split(content, "\n")

	switch language {
	case "go":
		issues = analyzeGoCode(lines)
	case "python":
		issues = analyzePythonCode(lines)
	case "javascript", "typescript":
		issues = analyzeJavaScriptCode(lines)
	case "rust":
		issues = analyzeRustCode(lines)
	case "java":
		issues = analyzeJavaCode(lines)
	case "cpp", "c":
		issues = analyzeCppCode(lines)
	}

	return issues
}

// analyzeGoCode analyzes Go code
func analyzeGoCode(lines []string) []Issue {
	var issues []Issue
	importFound := false
	packageFound := false

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "package ") {
			packageFound = true
		}
		if strings.HasPrefix(trimmed, "import ") {
			importFound = true
		}

		// Check for common issues
		if strings.Contains(line, "fmt.Println") && !importFound {
			issues = append(issues, Issue{
				Line:       lineNum,
				Severity:   "warning",
				Message:    "Possible missing import for fmt package",
				Suggestion: "Add 'import \"fmt\"' to imports",
			})
		}

		if strings.Contains(line, "defer ") && !strings.Contains(line, "()") {
			issues = append(issues, Issue{
				Line:       lineNum,
				Severity:   "warning",
				Message:    "defer should be used with function call",
				Suggestion: "Ensure defer is followed by a function call",
			})
		}
	}

	if !packageFound {
		issues = append(issues, Issue{
			Line:       1,
			Severity:   "error",
			Message:    "Missing package declaration",
			Suggestion: "Add 'package main' or appropriate package name",
		})
	}

	return issues
}

// analyzePythonCode analyzes Python code
func analyzePythonCode(lines []string) []Issue {
	var issues []Issue
	_ = false // shebangFound placeholder
	_ = false // encodingFound placeholder

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if i == 0 && strings.HasPrefix(trimmed, "#!/") {
			_ = true
		}
		if strings.Contains(trimmed, "# -*- coding:") || strings.Contains(trimmed, "# coding=") {
			_ = true
		}

		// Check for mutable default arguments
		if strings.Contains(line, "def ") && strings.Contains(line, "=[]") {
			issues = append(issues, Issue{
				Line:       lineNum,
				Severity:   "warning",
				Message:    "Mutable default argument detected",
				Suggestion: "Use None as default and initialize mutable object inside function",
			})
		}

		// Check for bare except
		if trimmed == "except:" || trimmed == "except :" {
			issues = append(issues, Issue{
				Line:       lineNum,
				Severity:   "warning",
				Message:    "Bare except clause detected",
				Suggestion: "Use 'except Exception:' instead of bare 'except:'",
			})
		}

		// Check for print statements (should use logging in production)
		if strings.HasPrefix(trimmed, "print(") {
			issues = append(issues, Issue{
				Line:       lineNum,
				Severity:   "info",
				Message:    "Print statement found",
				Suggestion: "Consider using logging module for production code",
			})
		}
	}

	return issues
}

// analyzeJavaScriptCode analyzes JavaScript/TypeScript code
func analyzeJavaScriptCode(lines []string) []Issue {
	var issues []Issue

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		// Check for var usage
		if strings.HasPrefix(trimmed, "var ") {
			issues = append(issues, Issue{
				Line:       lineNum,
				Severity:   "info",
				Message:    "Using 'var' instead of 'let' or 'const'",
				Suggestion: "Use 'const' for values that don't change, 'let' for those that do",
			})
		}

		// Check for == instead of ===
		if strings.Contains(line, " == ") && !strings.Contains(line, " === ") {
			issues = append(issues, Issue{
				Line:       lineNum,
				Severity:   "warning",
				Message:    "Using '==' instead of '==='",
				Suggestion: "Use '===' for strict equality comparison",
			})
		}

		// Check for console.log
		if strings.Contains(line, "console.log(") {
			issues = append(issues, Issue{
				Line:       lineNum,
				Severity:   "info",
				Message:    "console.log found",
				Suggestion: "Remove debug console.log statements before production",
			})
		}
	}

	return issues
}

// analyzeRustCode analyzes Rust code
func analyzeRustCode(lines []string) []Issue {
	var issues []Issue

	for i, line := range lines {
		lineNum := i + 1
		_ = strings.TrimSpace(line) // trimmed placeholder

		// Check for unwrap() usage
		if strings.Contains(line, ".unwrap()") {
			issues = append(issues, Issue{
				Line:       lineNum,
				Severity:   "warning",
				Message:    "Using unwrap() may cause panic",
				Suggestion: "Consider using match, if let, or expect() with meaningful message",
			})
		}

		// Check for expect() without message
		if strings.Contains(line, ".expect(") && strings.Contains(line, "\"") {
			// This is a simple check, real implementation would be more sophisticated
			continue
		}
	}

	return issues
}

// analyzeJavaCode analyzes Java code
func analyzeJavaCode(lines []string) []Issue {
	var issues []Issue

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		// Check for System.out.println
		if strings.Contains(line, "System.out.println") {
			issues = append(issues, Issue{
				Line:       lineNum,
				Severity:   "info",
				Message:    "System.out.println found",
				Suggestion: "Consider using a logging framework like SLF4J or Log4j",
			})
		}

		// Check for empty catch blocks
		if strings.Contains(trimmed, "catch") && strings.Contains(trimmed, "{") {
			// Simple check - real implementation would be more sophisticated
			continue
		}
	}

	return issues
}

// analyzeCppCode analyzes C/C++ code
func analyzeCppCode(lines []string) []Issue {
	var issues []Issue

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		// Check for raw pointers
		if strings.Contains(line, "new ") && !strings.Contains(line, "delete") {
			issues = append(issues, Issue{
				Line:       lineNum,
				Severity:   "info",
				Message:    "Raw pointer allocation without corresponding delete",
				Suggestion: "Consider using smart pointers (std::unique_ptr, std::shared_ptr)",
			})
		}

		// Check for using namespace std
		if trimmed == "using namespace std;" {
			issues = append(issues, Issue{
				Line:       lineNum,
				Severity:   "info",
				Message:    "Using 'using namespace std;'",
				Suggestion: "Consider using explicit std:: prefix or selective using declarations",
			})
		}
	}

	return issues
}

// runAIAnalysis performs AI-powered code analysis
func runAIAnalysis(filePath, content, language string, suggestFix bool) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	prov, err := config.CreateProvider(cfg)
	if err != nil {
		return fmt.Errorf("failed to create provider: %w", err)
	}

	ctx := context.Background()

	prompt := fmt.Sprintf(`Analyze the following %s code file (%s) for:
1. Syntax errors and logical bugs
2. Performance issues and bottlenecks
3. Security vulnerabilities
4. Code style and best practice violations
5. Potential refactoring opportunities
6. Missing error handling

Code:
`+"```"+`%s
`+"```"+`

%s`, language, filePath, content,
		func() string {
			if suggestFix {
				return "Please provide specific suggestions for fixing any issues found."
			}
			return ""
		}())

	resp, err := prov.Chat(ctx, []provider.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return err
	}

	fmt.Println(resp.Content)
	return nil
}

// runCodingLint runs linter on code
func runCodingLint(cmd *cobra.Command, args []string) error {
	filePath := args[0]
	autoFix, _ := cmd.Flags().GetBool("fix")
	format, _ := cmd.Flags().GetString("format")

	// Check if file exists
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return fmt.Errorf("file not found: %s", filePath)
	}

	// Detect language
	ext := filepath.Ext(filePath)
	if info.IsDir() {
		ext = ".dir"
	}
	language := detectLanguage(ext)

	fmt.Println(cyanStyle.Render(fmt.Sprintf("Linting: %s", filePath)))

	var linterCmd *exec.Cmd
	switch language {
	case "go":
		args := []string{"run", "golang.org/x/lint/golint@latest"}
		if autoFix {
			// golint doesn't support auto-fix, use gofmt instead
			linterCmd = exec.Command("gofmt", "-w", filePath)
		} else {
			args = append(args, filePath)
			linterCmd = exec.Command("go", args...)
		}
	case "python":
		if autoFix {
			linterCmd = exec.Command("autopep8", "--in-place", "--aggressive", filePath)
		} else {
			linterCmd = exec.Command("pylint", filePath)
		}
	case "javascript", "typescript":
		if autoFix {
			linterCmd = exec.Command("eslint", "--fix", filePath)
		} else {
			linterCmd = exec.Command("eslint", filePath)
		}
	case "rust":
		if autoFix {
			linterCmd = exec.Command("cargo", "clippy", "--fix", "--allow-dirty")
		} else {
			linterCmd = exec.Command("cargo", "clippy")
		}
	case "java":
		// Check for checkstyle
		if _, err := exec.LookPath("checkstyle"); err == nil {
			linterCmd = exec.Command("checkstyle", filePath)
		} else {
			fmt.Println(yellowStyle.Render("No Java linter found. Consider installing checkstyle."))
			return nil
		}
	case "cpp", "c":
		if _, err := exec.LookPath("cppcheck"); err == nil {
			linterCmd = exec.Command("cppcheck", "--enable=all", filePath)
		} else {
			fmt.Println(yellowStyle.Render("No C/C++ linter found. Consider installing cppcheck."))
			return nil
		}
	default:
		return fmt.Errorf("no linter available for file type: %s", ext)
	}

	output, err := linterCmd.CombinedOutput()
	if err != nil && len(output) > 0 {
		// Some linters return non-zero exit code when issues are found
		if format == "json" {
			fmt.Printf(`{"file": "%s", "issues": "%s"}`+"\n", filePath, strings.ReplaceAll(string(output), "\"", "\\\""))
		} else {
			fmt.Println(string(output))
		}
	} else if len(output) > 0 {
		fmt.Println(string(output))
	} else {
		fmt.Println(greenStyle.Render("No linting issues found!"))
	}

	return nil
}

// runCodingTest runs tests for the project
func runCodingTest(cmd *cobra.Command, args []string) error {
	verbose, _ := cmd.Flags().GetBool("verbose")
	pattern, _ := cmd.Flags().GetString("pattern")
	coverage, _ := cmd.Flags().GetBool("coverage")

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	pType := detectProjectType(cwd)

	fmt.Println(cyanStyle.Render(fmt.Sprintf("Running tests for %s project...", pType)))

	var testCmd *exec.Cmd
	switch pType {
	case ProjectTypeGo:
		args := []string{"test"}
		if verbose {
			args = append(args, "-v")
		}
		if coverage {
			args = append(args, "-cover")
		}
		if pattern != "" {
			args = append(args, "-run", pattern)
		}
		args = append(args, "./...")
		testCmd = exec.Command("go", args...)
	case ProjectTypePython:
		// Check for pytest first
		if _, err := exec.LookPath("pytest"); err == nil {
			args := []string{"-v"}
			if coverage {
				args = append(args, "--cov")
			}
			if pattern != "" {
				args = append(args, "-k", pattern)
			}
			testCmd = exec.Command("pytest", args...)
		} else {
			// Fall back to unittest
			testCmd = exec.Command("python3", "-m", "unittest", "discover")
		}
	case ProjectTypeNode:
		testCmd = exec.Command("npm", "test")
	case ProjectTypeRust:
		args := []string{"test"}
		if verbose {
			args = append(args, "--", "--nocapture")
		}
		if pattern != "" {
			args = append(args, pattern)
		}
		testCmd = exec.Command("cargo", args...)
	case ProjectTypeJava:
		if _, err := os.Stat("pom.xml"); err == nil {
			testCmd = exec.Command("mvn", "test")
		} else if _, err := os.Stat("build.gradle"); err == nil {
			testCmd = exec.Command("gradle", "test")
		} else {
			return fmt.Errorf("no Maven or Gradle build file found")
		}
	case ProjectTypeCpp:
		// Try to find and run tests
		if _, err := os.Stat("build"); err == nil {
			testCmd = exec.Command("ctest", "--output-on-failure")
		} else {
			return fmt.Errorf("no build directory found. Run 'magic coding build' first")
		}
	default:
		return fmt.Errorf("cannot determine how to run tests for this project type")
	}

	testCmd.Dir = cwd
	testCmd.Stdout = os.Stdout
	testCmd.Stderr = os.Stderr

	start := time.Now()
	err = testCmd.Run()
	duration := time.Since(start)

	if err != nil {
		fmt.Println(redStyle.Render(fmt.Sprintf("Tests failed after %v", duration)))
		return err
	}

	fmt.Println(greenStyle.Render(fmt.Sprintf("Tests passed in %v", duration)))
	return nil
}

// runCodingBuild builds the project
func runCodingBuild(cmd *cobra.Command, args []string) error {
	output, _ := cmd.Flags().GetString("output")
	target, _ := cmd.Flags().GetString("target")
	release, _ := cmd.Flags().GetBool("release")

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	pType := detectProjectType(cwd)

	fmt.Println(cyanStyle.Render(fmt.Sprintf("Building %s project...", pType)))

	var buildCmd *exec.Cmd
	switch pType {
	case ProjectTypeGo:
		args := []string{"build"}
		if output != "" {
			args = append(args, "-o", output)
		}
		if target != "" {
			parts := strings.Split(target, "/")
			if len(parts) == 2 {
				os.Setenv("GOOS", parts[0])
				os.Setenv("GOARCH", parts[1])
			}
		}
		buildCmd = exec.Command("go", args...)
	case ProjectTypePython:
		// Python doesn't compile, but we can check syntax
		fmt.Println(yellowStyle.Render("Python is interpreted. Checking syntax..."))
		buildCmd = exec.Command("python3", "-m", "py_compile", "main.py")
	case ProjectTypeNode:
		// Check for build script in package.json
		buildCmd = exec.Command("npm", "run", "build")
	case ProjectTypeRust:
		args := []string{"build"}
		if release {
			args = append(args, "--release")
		}
		if target != "" {
			args = append(args, "--target", target)
		}
		buildCmd = exec.Command("cargo", args...)
	case ProjectTypeJava:
		if _, err := os.Stat("pom.xml"); err == nil {
			args := []string{"compile"}
			if release {
				args = append(args, "-DskipTests")
			}
			buildCmd = exec.Command("mvn", args...)
		} else if _, err := os.Stat("build.gradle"); err == nil {
			args := []string{"build"}
			if release {
				args = append(args, "-x", "test")
			}
			buildCmd = exec.Command("gradle", args...)
		} else {
			return fmt.Errorf("no Maven or Gradle build file found")
		}
	case ProjectTypeCpp:
		// Create build directory and run cmake
		buildDir := filepath.Join(cwd, "build")
		if err := os.MkdirAll(buildDir, 0755); err != nil {
			return fmt.Errorf("failed to create build directory: %w", err)
		}

		// Run cmake
		cmakeCmd := exec.Command("cmake", "..")
		cmakeCmd.Dir = buildDir
		if output, err := cmakeCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("cmake failed: %w\n%s", err, output)
		}

		// Run make
		buildCmd = exec.Command("make")
		buildCmd.Dir = buildDir
	default:
		return fmt.Errorf("cannot determine how to build this project type")
	}

	buildCmd.Dir = cwd
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr

	start := time.Now()
	err = buildCmd.Run()
	duration := time.Since(start)

	if err != nil {
		fmt.Println(redStyle.Render(fmt.Sprintf("Build failed after %v", duration)))
		return err
	}

	fmt.Println(greenStyle.Render(fmt.Sprintf("Build successful in %v", duration)))
	return nil
}

// runCodingDebug launches debug mode
func runCodingDebug(cmd *cobra.Command, args []string) error {
	chatMode, _ := cmd.Flags().GetBool("chat")

	if chatMode {
		// Start chat with coding context
		return runChat(nil, nil)
	}

	fmt.Println(cyanStyle.Render("Launching debug mode..."))
	fmt.Println(whiteStyle.Render("Available commands:"))
	fmt.Println(whiteStyle.Render("  analyze <file>  - Analyze code file"))
	fmt.Println(whiteStyle.Render("  lint <file>     - Lint code file"))
	fmt.Println(whiteStyle.Render("  test            - Run tests"))
	fmt.Println(whiteStyle.Render("  build           - Build project"))
	fmt.Println(whiteStyle.Render("  exit            - Exit debug mode"))
	fmt.Println()

	// Interactive debug mode
	for {
		fmt.Print(cyanStyle.Render("debug> "))
		var input string
		fmt.Scanln(&input)

		switch {
		case input == "exit" || input == "quit":
			fmt.Println(greenStyle.Render("Exiting debug mode"))
			return nil
		case strings.HasPrefix(input, "analyze "):
			file := strings.TrimPrefix(input, "analyze ")
			codingAnalyzeCmd.Run(nil, []string{file})
		case strings.HasPrefix(input, "lint "):
			file := strings.TrimPrefix(input, "lint ")
			codingLintCmd.Run(nil, []string{file})
		case input == "test":
			codingTestCmd.Run(nil, nil)
		case input == "build":
			codingBuildCmd.Run(nil, nil)
		default:
			fmt.Println(yellowStyle.Render(fmt.Sprintf("Unknown command: %s", input)))
		}
	}
}
