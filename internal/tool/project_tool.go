package tool

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ProjectAnalyzeTool provides project-level analysis capabilities for coding mode.
// It supports multiple actions: analyze_structure, analyze_dependencies,
// analyze_complexity, find_entry_points, and generate_summary.
type ProjectAnalyzeTool struct {
	*BaseTool
}

// NewProjectAnalyzeTool creates a new project analysis tool.
func NewProjectAnalyzeTool() *ProjectAnalyzeTool {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the project directory to analyze",
			},
			"action": map[string]interface{}{
				"type": "string",
				"description": "Analysis action to perform",
				"enum": []string{
					"analyze_structure",
					"analyze_dependencies",
					"analyze_complexity",
					"find_entry_points",
					"generate_summary",
				},
			},
		},
		"required": []string{"path", "action"},
	}

	return &ProjectAnalyzeTool{
		BaseTool: NewBaseTool(
			"project_analyze",
			"Analyze project structure, dependencies, complexity, and entry points. Supports actions: analyze_structure, analyze_dependencies, analyze_complexity, find_entry_points, generate_summary.",
			schema,
		),
	}
}

// Execute runs the project analysis tool with the given parameters.
func (t *ProjectAnalyzeTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	path, ok := params["path"].(string)
	if !ok || path == "" {
		return nil, fmt.Errorf("path parameter is required")
	}

	action, ok := params["action"].(string)
	if !ok || action == "" {
		return nil, fmt.Errorf("action parameter is required")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve path: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to access path: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path must be a directory: %s", absPath)
	}

	switch action {
	case "analyze_structure":
		return t.analyzeStructure(absPath)
	case "analyze_dependencies":
		return t.analyzeDependencies(absPath)
	case "analyze_complexity":
		return t.analyzeComplexity(absPath)
	case "find_entry_points":
		return t.findEntryPoints(absPath)
	case "generate_summary":
		return t.generateSummary(absPath)
	default:
		return nil, fmt.Errorf("unknown action: %s (valid: analyze_structure, analyze_dependencies, analyze_complexity, find_entry_points, generate_summary)", action)
	}
}

// ---------------------------------------------------------------------------
// Action: analyze_structure
// ---------------------------------------------------------------------------

// StructureResult holds the output of project structure analysis.
type StructureResult struct {
	ProjectType    string              `json:"project_type"`
	RootDir        string              `json:"root_dir"`
	TopLevelDirs   []string            `json:"top_level_dirs"`
	TopLevelFiles  []string            `json:"top_level_files"`
	KeyFiles       []string            `json:"key_files"`
	TestDirs       []string            `json:"test_dirs"`
	ConfigFiles    []string            `json:"config_files"`
	DependencyInfo string              `json:"dependency_info"`
	VCS            string              `json:"vcs"`
	BuildFiles     []string            `json:"build_files"`
}

func (t *ProjectAnalyzeTool) analyzeStructure(dir string) (interface{}, error) {
	projectType := DetectProjectType(dir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var topDirs, topFiles []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") && e.Name() != ".git" {
			continue
		}
		if e.IsDir() {
			topDirs = append(topDirs, e.Name())
		} else {
			topFiles = append(topFiles, e.Name())
		}
	}
	sort.Strings(topDirs)
	sort.Strings(topFiles)

	keyFiles := t.findKeyFiles(dir)
	testDirs := t.findTestDirs(dir)
	configFiles := t.findConfigFiles(dir)
	buildFiles := t.findBuildFiles(dir, projectType)
	depInfo := t.detectDependencyInfo(dir, projectType)
	vcs := t.detectVCS(dir)

	result := StructureResult{
		ProjectType:    string(projectType),
		RootDir:        dir,
		TopLevelDirs:   topDirs,
		TopLevelFiles:  topFiles,
		KeyFiles:       keyFiles,
		TestDirs:       testDirs,
		ConfigFiles:    configFiles,
		DependencyInfo: depInfo,
		VCS:            vcs,
		BuildFiles:     buildFiles,
	}

	return result, nil
}

func (t *ProjectAnalyzeTool) findKeyFiles(dir string) []string {
	candidates := []string{
		"README.md", "README.txt", "README.rst", "README",
		"CHANGELOG.md", "CHANGELOG", "CHANGES.md",
		"CONTRIBUTING.md", "LICENSE", "LICENSE.md",
		"Makefile", "Dockerfile", "docker-compose.yml", "docker-compose.yaml",
		".gitignore", ".gitlab-ci.yml", ".github",
	}

	var found []string
	for _, c := range candidates {
		if fileExists(filepath.Join(dir, c)) {
			found = append(found, c)
		}
	}
	return found
}

func (t *ProjectAnalyzeTool) findTestDirs(dir string) []string {
	candidates := []string{
		"test", "tests", "testing", "__tests__", "spec", "specs",
		"integration_test", "integration_tests", "e2e", "benchmark",
	}

	var found []string
	for _, c := range candidates {
		if fileExists(filepath.Join(dir, c)) {
			found = append(found, c)
		}
	}
	return found
}

func (t *ProjectAnalyzeTool) findConfigFiles(dir string) []string {
	candidates := []string{
		".eslintrc.js", ".eslintrc.json", ".eslintrc.yml", ".eslintrc", "eslint.config.js",
		".prettierrc", ".prettierrc.json", "prettier.config.js",
		".tsconfig.json", "tsconfig.json",
		".golangci.yml", ".golangci.yaml",
		"pyproject.toml", "setup.cfg", "tox.ini",
		".editorconfig",
		".env", ".env.example", ".env.local",
	}

	var found []string
	for _, c := range candidates {
		if fileExists(filepath.Join(dir, c)) {
			found = append(found, c)
		}
	}
	return found
}

func (t *ProjectAnalyzeTool) findBuildFiles(dir string, pt ProjectType) []string {
	var candidates []string
	switch pt {
	case ProjectTypeGo:
		candidates = []string{"go.mod", "go.sum", "Makefile", "Dockerfile"}
	case ProjectTypeNode:
		candidates = []string{"package.json", "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "tsconfig.json", "webpack.config.js", "vite.config.ts", "rollup.config.js"}
	case ProjectTypeRust:
		candidates = []string{"Cargo.toml", "Cargo.lock", "Makefile", "Dockerfile", "build.rs"}
	case ProjectTypePython:
		candidates = []string{"setup.py", "setup.cfg", "pyproject.toml", "requirements.txt", "Pipfile", "Makefile", "Dockerfile", "tox.ini"}
	default:
		candidates = []string{"Makefile", "Dockerfile", "build.sh"}
	}

	var found []string
	for _, c := range candidates {
		if fileExists(filepath.Join(dir, c)) {
			found = append(found, c)
		}
	}
	return found
}

func (t *ProjectAnalyzeTool) detectDependencyInfo(dir string, pt ProjectType) string {
	switch pt {
	case ProjectTypeGo:
		if data, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil {
			firstLine := strings.SplitN(string(data), "\n", 3)
			if len(firstLine) >= 2 {
				return strings.TrimSpace(firstLine[1]) // module path
			}
		}
		return "go.mod found but could not parse module path"
	case ProjectTypeNode:
		if data, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
			// Quick extract of name
			s := string(data)
			if idx := strings.Index(s, `"name"`); idx != -1 {
				rest := s[idx:]
				if c1 := strings.Index(rest, `"`); c1 != -1 {
					rest = rest[c1+1:]
					if c2 := strings.Index(rest, `"`); c2 != -1 {
						return "package: " + rest[:c2]
					}
				}
			}
		}
		return "package.json found"
	case ProjectTypeRust:
		if data, err := os.ReadFile(filepath.Join(dir, "Cargo.toml")); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "name") && strings.Contains(line, "=") {
					parts := strings.SplitN(line, "=", 2)
					name := strings.TrimSpace(strings.Trim(strings.TrimSpace(parts[1]), "\""))
					return "crate: " + name
				}
			}
		}
		return "Cargo.toml found"
	case ProjectTypePython:
		if fileExists(filepath.Join(dir, "pyproject.toml")) {
			return "pyproject.toml found"
		}
		if fileExists(filepath.Join(dir, "setup.py")) {
			return "setup.py found"
		}
		return "requirements.txt found"
	default:
		return "no recognized dependency file"
	}
}

func (t *ProjectAnalyzeTool) detectVCS(dir string) string {
	if fileExists(filepath.Join(dir, ".git")) {
		return "git"
	}
	if fileExists(filepath.Join(dir, ".hg")) {
		return "mercurial"
	}
	if fileExists(filepath.Join(dir, ".svn")) {
		return "svn"
	}
	return "none"
}

// ---------------------------------------------------------------------------
// Action: analyze_dependencies
// ---------------------------------------------------------------------------

// Dependency represents a single dependency entry.
type Dependency struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Type    string `json:"type"` // "direct", "dev", "indirect"
}

// DependencyResult holds the output of dependency analysis.
type DependencyResult struct {
	ProjectType  ProjectType  `json:"project_type"`
	Dependencies []Dependency `json:"dependencies"`
	SourceFile   string       `json:"source_file"`
}

func (t *ProjectAnalyzeTool) analyzeDependencies(dir string) (interface{}, error) {
	projectType := DetectProjectType(dir)

	var deps []Dependency
	var sourceFile string

	switch projectType {
	case ProjectTypeGo:
		deps, sourceFile = t.parseGoMod(dir)
	case ProjectTypeNode:
		deps, sourceFile = t.parsePackageJSON(dir)
	case ProjectTypeRust:
		deps, sourceFile = t.parseCargoToml(dir)
	case ProjectTypePython:
		deps, sourceFile = t.parsePythonDeps(dir)
	default:
		// Try all parsers
		if d, s := t.parseGoMod(dir); len(d) > 0 {
			return DependencyResult{ProjectType: ProjectTypeGo, Dependencies: d, SourceFile: s}, nil
		}
		if d, s := t.parsePackageJSON(dir); len(d) > 0 {
			return DependencyResult{ProjectType: ProjectTypeNode, Dependencies: d, SourceFile: s}, nil
		}
		if d, s := t.parseCargoToml(dir); len(d) > 0 {
			return DependencyResult{ProjectType: ProjectTypeRust, Dependencies: d, SourceFile: s}, nil
		}
		if d, s := t.parsePythonDeps(dir); len(d) > 0 {
			return DependencyResult{ProjectType: ProjectTypePython, Dependencies: d, SourceFile: s}, nil
		}
		if d, s := t.parsePomXML(dir); len(d) > 0 {
			return DependencyResult{ProjectType: "java", Dependencies: d, SourceFile: s}, nil
		}
		return DependencyResult{ProjectType: ProjectTypeUnknown, Dependencies: nil, SourceFile: ""}, nil
	}

	// Also try pom.xml for any project type
	if pomDeps, pomFile := t.parsePomXML(dir); len(pomDeps) > 0 && len(deps) == 0 {
		return DependencyResult{ProjectType: "java", Dependencies: pomDeps, SourceFile: pomFile}, nil
	}

	return DependencyResult{
		ProjectType:  projectType,
		Dependencies: deps,
		SourceFile:   sourceFile,
	}, nil
}

// parseGoMod parses go.mod and returns dependencies.
func (t *ProjectAnalyzeTool) parseGoMod(dir string) ([]Dependency, string) {
	path := filepath.Join(dir, "go.mod")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, ""
	}

	var deps []Dependency
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	inRequire := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "require (" {
			inRequire = true
			continue
		}
		if inRequire && line == ")" {
			inRequire = false
			continue
		}

		// Single-line require
		if strings.HasPrefix(line, "require ") {
			line = strings.TrimPrefix(line, "require ")
		}

		if inRequire || strings.HasPrefix(line, "require ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 && !strings.HasPrefix(parts[0], "//") {
				depType := "direct"
				if len(parts) >= 3 && (parts[2] == "//" || strings.HasPrefix(parts[2], "//")) {
					// Comment present, still direct
				}
				if strings.HasSuffix(parts[0], "//indirect") {
					depType = "indirect"
					parts[0] = strings.TrimSuffix(parts[0], "//indirect")
					parts[0] = strings.TrimSpace(parts[0])
				}
				deps = append(deps, Dependency{
					Name:    parts[0],
					Version: parts[1],
					Type:    depType,
				})
			}
		}
	}

	return deps, "go.mod"
}

// parsePackageJSON parses package.json and returns dependencies.
func (t *ProjectAnalyzeTool) parsePackageJSON(dir string) ([]Dependency, string) {
	path := filepath.Join(dir, "package.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, ""
	}

	var deps []Dependency
	content := string(data)

	// Parse dependencies section
	deps = append(deps, t.extractJSONDependencies(content, `"dependencies"`, "direct")...)
	deps = append(deps, t.extractJSONDependencies(content, `"devDependencies"`, "dev")...)
	deps = append(deps, t.extractJSONDependencies(content, `"peerDependencies"`, "peer")...)

	return deps, "package.json"
}

// extractJSONDependencies extracts dependencies from a JSON section.
func (t *ProjectAnalyzeTool) extractJSONDependencies(content, section, depType string) []Dependency {
	var deps []Dependency

	idx := strings.Index(content, section)
	if idx == -1 {
		return nil
	}

	// Find the opening brace after the section key
	braceStart := strings.Index(content[idx:], "{")
	if braceStart == -1 {
		return nil
	}
	braceStart += idx

	// Find matching closing brace
	depth := 0
	braceEnd := -1
	for i := braceStart; i < len(content); i++ {
		if content[i] == '{' {
			depth++
		} else if content[i] == '}' {
			depth--
			if depth == 0 {
				braceEnd = i
				break
			}
		}
	}
	if braceEnd == -1 {
		return nil
	}

	sectionContent := content[braceStart+1 : braceEnd]

	// Parse "name": "version" pairs
	lines := strings.Split(sectionContent, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "/*") {
			continue
		}

		// Extract key and value from "key": "value"
		if strings.Contains(line, ":") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				continue
			}
			name := strings.TrimSpace(parts[0])
			version := strings.TrimSpace(parts[1])

			// Remove surrounding quotes and trailing comma
			name = strings.Trim(name, "\"")
			version = strings.Trim(version, "\"")
			version = strings.TrimSuffix(version, ",")

			if name != "" && version != "" {
				deps = append(deps, Dependency{
					Name:    name,
					Version: version,
					Type:    depType,
				})
			}
		}
	}

	return deps
}

// parseCargoToml parses Cargo.toml and returns dependencies.
func (t *ProjectAnalyzeTool) parseCargoToml(dir string) ([]Dependency, string) {
	path := filepath.Join(dir, "Cargo.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, ""
	}

	var deps []Dependency
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	inSection := ""
	sectionType := "direct"

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Detect section headers
		if strings.HasPrefix(line, "[dependencies]") || strings.HasPrefix(line, "[dependencies.") {
			inSection = "dependencies"
			sectionType = "direct"
			continue
		}
		if strings.HasPrefix(line, "[dev-dependencies]") || strings.HasPrefix(line, "[dev-dependencies.") {
			inSection = "dev-dependencies"
			sectionType = "dev"
			continue
		}
		if strings.HasPrefix(line, "[build-dependencies]") || strings.HasPrefix(line, "[build-dependencies.") {
			inSection = "build-dependencies"
			sectionType = "build"
			continue
		}

		// End of section
		if strings.HasPrefix(line, "[") && inSection != "" {
			inSection = ""
			continue
		}

		if inSection == "" {
			continue
		}

		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse "name = "version"" or "name = { version = "x" }"
		if strings.Contains(line, "=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			name := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])

			if name == "" {
				continue
			}

			version := ""
			if strings.HasPrefix(value, "\"") {
				// Simple version: name = "1.0"
				version = strings.Trim(value, "\"")
			} else if strings.HasPrefix(value, "{") {
				// Inline table: name = { version = "1.0" }
				if vIdx := strings.Index(value, "version"); vIdx != -1 {
					rest := value[vIdx:]
					if eqIdx := strings.Index(rest, "="); eqIdx != -1 {
						rest = rest[eqIdx+1:]
						rest = strings.TrimSpace(rest)
						if qIdx := strings.Index(rest, "\""); qIdx != -1 {
							rest = rest[qIdx+1:]
							if endIdx := strings.Index(rest, "\""); endIdx != -1 {
								version = rest[:endIdx]
							}
						}
					}
				}
			}

			if version != "" {
				deps = append(deps, Dependency{
					Name:    name,
					Version: version,
					Type:    sectionType,
				})
			}
		}
	}

	return deps, "Cargo.toml"
}

// parsePythonDeps parses Python dependency files.
func (t *ProjectAnalyzeTool) parsePythonDeps(dir string) ([]Dependency, string) {
	// Try requirements.txt first
	path := filepath.Join(dir, "requirements.txt")
	if fileExists(path) {
		return t.parseRequirementsTxt(path), "requirements.txt"
	}

	// Try pyproject.toml
	path = filepath.Join(dir, "pyproject.toml")
	if fileExists(path) {
		return t.parsePyprojectToml(path), "pyproject.toml"
	}

	return nil, ""
}

// parseRequirementsTxt parses a requirements.txt file.
func (t *ProjectAnalyzeTool) parseRequirementsTxt(path string) []Dependency {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var deps []Dependency
	scanner := bufio.NewScanner(strings.NewReader(string(data)))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
			continue
		}

		name, version := t.parsePipRequirement(line)
		if name != "" {
			deps = append(deps, Dependency{
				Name:    name,
				Version: version,
				Type:    "direct",
			})
		}
	}

	return deps
}

// parsePipRequirement parses a single pip requirement line.
func (t *ProjectAnalyzeTool) parsePipRequirement(line string) (name, version string) {
	// Handle various formats:
	// package==1.0
	// package>=1.0
	// package<=1.0
	// package~=1.0
	// package!=1.0
	// package>1.0
	// package<1.0
	// package
	// package @ https://...

	// Strip environment markers
	if idx := strings.Index(line, ";"); idx != -1 {
		line = strings.TrimSpace(line[:idx])
	}

	// Strip extras: package[extra]
	if idx := strings.Index(line, "["); idx != -1 {
		name = strings.TrimSpace(line[:idx])
	} else {
		name = strings.TrimSpace(line)
	}

	// Find version specifier
	for _, op := range []string{"==", ">=", "<=", "~=", "!=", ">=", "<="} {
		if idx := strings.Index(name, op); idx != -1 {
			version = strings.TrimSpace(name[idx+len(op):])
			name = strings.TrimSpace(name[:idx])
			return name, version
		}
	}

	// Check for single > or <
	if idx := strings.IndexAny(name, "><"); idx != -1 {
		version = strings.TrimSpace(name[idx+1:])
		name = strings.TrimSpace(name[:idx])
		return name, version
	}

	return name, ""
}

// parsePyprojectToml extracts dependencies from pyproject.toml.
func (t *ProjectAnalyzeTool) parsePyprojectToml(path string) []Dependency {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var deps []Dependency
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	inDeps := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.Contains(line, "dependencies") && strings.Contains(line, "=") {
			if strings.Contains(line, "[") {
				inDeps = true
				continue
			}
		}

		if inDeps {
			if line == "]" || (strings.HasPrefix(line, "[") && !strings.Contains(line, "dependencies")) {
				inDeps = false
				continue
			}
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}

			dep := strings.Trim(line, "\"', ")
			dep = strings.TrimSuffix(dep, ",")
			if dep != "" {
				name, version := t.parsePipRequirement(dep)
				if name != "" {
					deps = append(deps, Dependency{
						Name:    name,
						Version: version,
						Type:    "direct",
					})
				}
			}
		}
	}

	return deps
}

// parsePomXML parses pom.xml (Maven) and returns dependencies.
func (t *ProjectAnalyzeTool) parsePomXML(dir string) ([]Dependency, string) {
	path := filepath.Join(dir, "pom.xml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, ""
	}

	var deps []Dependency
	content := string(data)
	lines := strings.Split(content, "\n")

	inDependency := false
	var currentDep Dependency
	depDepth := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.Contains(trimmed, "<dependency>") {
			inDependency = true
			depDepth++
			currentDep = Dependency{}
			continue
		}

		if inDependency {
			if strings.Contains(trimmed, "<dependency>") {
				depDepth++
			}
			if strings.Contains(trimmed, "</dependency>") {
				depDepth--
				if depDepth == 0 {
					if currentDep.Name != "" {
						if currentDep.Type == "" {
							currentDep.Type = "direct"
						}
						deps = append(deps, currentDep)
					}
					inDependency = false
					continue
				}
			}

			if strings.Contains(trimmed, "<groupId>") {
				start := strings.Index(trimmed, "<groupId>") + len("<groupId>")
				end := strings.Index(trimmed[start:], "</groupId>")
				if end != -1 {
					currentDep.Name = trimmed[start : start+end]
				}
			}
			if strings.Contains(trimmed, "<artifactId>") {
				start := strings.Index(trimmed, "<artifactId>") + len("<artifactId>")
				end := strings.Index(trimmed[start:], "</artifactId>")
				if end != -1 {
					artifact := trimmed[start : start+end]
					if currentDep.Name != "" {
						currentDep.Name = currentDep.Name + ":" + artifact
					} else {
						currentDep.Name = artifact
					}
				}
			}
			if strings.Contains(trimmed, "<version>") {
				start := strings.Index(trimmed, "<version>") + len("<version>")
				end := strings.Index(trimmed[start:], "</version>")
				if end != -1 {
					currentDep.Version = trimmed[start : start+end]
				}
			}
			if strings.Contains(trimmed, "<scope>") {
				start := strings.Index(trimmed, "<scope>") + len("<scope>")
				end := strings.Index(trimmed[start:], "</scope>")
				if end != -1 {
					scope := trimmed[start : start+end]
					switch scope {
					case "test":
						currentDep.Type = "dev"
					case "provided":
						currentDep.Type = "provided"
					case "runtime":
						currentDep.Type = "direct"
					default:
						currentDep.Type = "direct"
					}
				}
			}
		}
	}

	return deps, "pom.xml"
}

// ---------------------------------------------------------------------------
// Action: analyze_complexity
// ---------------------------------------------------------------------------

// ComplexityResult holds the output of code complexity analysis.
type ComplexityResult struct {
	TotalFiles     int            `json:"total_files"`
	TotalDirs      int            `json:"total_dirs"`
	TotalLines     int            `json:"total_lines"`
	LanguageBreakdown map[string]LanguageStats `json:"language_breakdown"`
	LargestFiles   []FileInfo     `json:"largest_files"`
}

// LanguageStats holds statistics for a single language.
type LanguageStats struct {
	Files int `json:"files"`
	Lines int `json:"lines"`
}

// FileInfo holds information about a single file.
type FileInfo struct {
	Path  string `json:"path"`
	Lines int    `json:"lines"`
	Size  int64  `json:"size"`
}

func (t *ProjectAnalyzeTool) analyzeComplexity(dir string) (interface{}, error) {
	langMap := make(map[string]*LanguageStats)
	var allFiles []FileInfo
	totalLines := 0
	totalDirs := 0

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors
		}

		// Skip hidden directories and common non-source dirs
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") && name != "." {
				return filepath.SkipDir
			}
			if name == "vendor" || name == "node_modules" || name == "third_party" ||
				name == "__pycache__" || name == ".git" || name == "target" ||
				name == "dist" || name == "build" || name == "out" {
				return filepath.SkipDir
			}
			totalDirs++
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		lang := extensionToLanguage(ext)
		if lang == "" {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		lines, err := t.countLines(path)
		if err != nil {
			return nil
		}

		totalLines += lines
		allFiles = append(allFiles, FileInfo{
			Path:  path,
			Lines: lines,
			Size:  info.Size(),
		})

		stats, ok := langMap[lang]
		if !ok {
			stats = &LanguageStats{}
			langMap[lang] = stats
		}
		stats.Files++
		stats.Lines += lines

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk directory: %w", err)
	}

	// Sort largest files by line count
	sort.Slice(allFiles, func(i, j int) bool {
		return allFiles[i].Lines > allFiles[j].Lines
	})

	largest := allFiles
	if len(largest) > 20 {
		largest = largest[:20]
	}

	// Build language breakdown
	breakdown := make(map[string]LanguageStats)
	for lang, stats := range langMap {
		breakdown[lang] = *stats
	}

	return ComplexityResult{
		TotalFiles:        len(allFiles),
		TotalDirs:         totalDirs,
		TotalLines:        totalLines,
		LanguageBreakdown: breakdown,
		LargestFiles:      largest,
	}, nil
}

func (t *ProjectAnalyzeTool) countLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		count++
	}
	return count, scanner.Err()
}

func extensionToLanguage(ext string) string {
	switch ext {
	case ".go":
		return "Go"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "JavaScript"
	case ".ts", ".tsx":
		return "TypeScript"
	case ".py":
		return "Python"
	case ".rs":
		return "Rust"
	case ".java":
		return "Java"
	case ".kt", ".kts":
		return "Kotlin"
	case ".c", ".h":
		return "C"
	case ".cpp", ".cc", ".cxx", ".hpp", ".hxx":
		return "C++"
	case ".cs":
		return "C#"
	case ".rb":
		return "Ruby"
	case ".php":
		return "PHP"
	case ".swift":
		return "Swift"
	case ".scala":
		return "Scala"
	case ".sh", ".bash", ".zsh":
		return "Shell"
	case ".sql":
		return "SQL"
	case ".html", ".htm":
		return "HTML"
	case ".css", ".scss", ".sass", ".less":
		return "CSS"
	case ".vue":
		return "Vue"
	case ".svelte":
		return "Svelte"
	case ".lua":
		return "Lua"
	case ".r", ".R":
		return "R"
	case ".dart":
		return "Dart"
	case ".zig":
		return "Zig"
	case ".ex", ".exs":
		return "Elixir"
	case ".erl":
		return "Erlang"
	case ".hs":
		return "Haskell"
	case ".ml", ".mli":
		return "OCaml"
	case ".proto":
		return "Protocol Buffers"
	case ".graphql", ".gql":
		return "GraphQL"
	case ".toml", ".yaml", ".yml", ".json", ".xml":
		return "" // skip config/data files from code counts
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// Action: find_entry_points
// ---------------------------------------------------------------------------

// EntryPointResult holds the output of entry point detection.
type EntryPointResult struct {
	EntryPoints []EntryPoint `json:"entry_points"`
	ConfigFiles []string     `json:"config_files"`
}

// EntryPoint represents a detected entry point.
type EntryPoint struct {
	Path        string `json:"path"`
	Type        string `json:"type"`        // "main", "cli", "library", "test", "config"
	Description string `json:"description"`
}

func (t *ProjectAnalyzeTool) findEntryPoints(dir string) (interface{}, error) {
	projectType := DetectProjectType(dir)
	var entries []EntryPoint
	configFiles := t.findConfigFiles(dir)

	// Common entry point patterns by project type
	switch projectType {
	case ProjectTypeGo:
		entries = t.findGoEntryPoints(dir)
	case ProjectTypeNode:
		entries = t.findNodeEntryPoints(dir)
	case ProjectTypeRust:
		entries = t.findRustEntryPoints(dir)
	case ProjectTypePython:
		entries = t.findPythonEntryPoints(dir)
	default:
		entries = t.findGenericEntryPoints(dir)
	}

	// Also find common config files that serve as entry points
	configEntries := t.findConfigEntryPoints(dir, projectType)
	entries = append(entries, configEntries...)

	return EntryPointResult{
		EntryPoints: entries,
		ConfigFiles: configFiles,
	}, nil
}

func (t *ProjectAnalyzeTool) findGoEntryPoints(dir string) []EntryPoint {
	var entries []EntryPoint

	// Look for main.go files
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}

		base := filepath.Base(path)
		if base == "main.go" {
			entries = append(entries, EntryPoint{
				Path:        path,
				Type:        "main",
				Description: "Go main package entry point",
			})
		}
		return nil
	})

	return entries
}

func (t *ProjectAnalyzeTool) findNodeEntryPoints(dir string) []EntryPoint {
	var entries []EntryPoint

	candidates := []struct {
		file        string
		entryType   string
		description string
	}{
		{"index.js", "main", "JavaScript entry point"},
		{"index.ts", "main", "TypeScript entry point"},
		{"main.js", "main", "JavaScript main entry point"},
		{"main.ts", "main", "TypeScript main entry point"},
		{"app.js", "main", "JavaScript application entry point"},
		{"app.ts", "main", "TypeScript application entry point"},
		{"server.js", "main", "Server entry point"},
		{"server.ts", "main", "Server entry point"},
		{"cli.js", "cli", "CLI entry point"},
		{"cli.ts", "cli", "CLI entry point"},
		{"bin/index.js", "cli", "Binary entry point"},
		{"bin/cli.js", "cli", "Binary CLI entry point"},
		{"src/index.ts", "main", "Source entry point"},
		{"src/main.ts", "main", "Source main entry point"},
		{"src/app.ts", "main", "Source application entry point"},
	}

	for _, c := range candidates {
		path := filepath.Join(dir, c.file)
		if fileExists(path) {
			entries = append(entries, EntryPoint{
				Path:        path,
				Type:        c.entryType,
				Description: c.description,
			})
		}
	}

	// Also check package.json "main" and "bin" fields
	pkgPath := filepath.Join(dir, "package.json")
	if data, err := os.ReadFile(pkgPath); err == nil {
		content := string(data)
		if idx := strings.Index(content, `"main"`); idx != -1 {
			mainFile := t.extractJSONStringValue(content[idx:], "main")
			if mainFile != "" {
				absMain := filepath.Join(dir, mainFile)
				if fileExists(absMain) {
					entries = append(entries, EntryPoint{
						Path:        absMain,
						Type:        "main",
						Description: "package.json main field",
					})
				}
			}
		}
	}

	return entries
}

func (t *ProjectAnalyzeTool) findRustEntryPoints(dir string) []EntryPoint {
	var entries []EntryPoint

	// Look for main.rs and lib.rs
	candidates := []struct {
		file        string
		entryType   string
		description string
	}{
		{"src/main.rs", "main", "Rust binary entry point"},
		{"src/lib.rs", "library", "Rust library entry point"},
		{"main.rs", "main", "Rust binary entry point (root)"},
		{"lib.rs", "library", "Rust library entry point (root)"},
	}

	for _, c := range candidates {
		path := filepath.Join(dir, c.file)
		if fileExists(path) {
			entries = append(entries, EntryPoint{
				Path:        path,
				Type:        c.entryType,
				Description: c.description,
			})
		}
	}

	// Check for binary targets in Cargo.toml
	cargoPath := filepath.Join(dir, "Cargo.toml")
	if data, err := os.ReadFile(cargoPath); err == nil {
		content := string(data)
		if strings.Contains(content, "[[bin]]") {
			// Parse [[bin]] sections for custom binary targets
			lines := strings.Split(content, "\n")
			for i, line := range lines {
				if strings.TrimSpace(line) == "[[bin]]" {
					// Look for path in next few lines
					for j := i + 1; j < i+10 && j < len(lines); j++ {
						l := strings.TrimSpace(lines[j])
						if strings.HasPrefix(l, "path") && strings.Contains(l, "=") {
							parts := strings.SplitN(l, "=", 2)
							binPath := strings.Trim(strings.TrimSpace(parts[1]), "\"")
							absPath := filepath.Join(dir, binPath)
							if fileExists(absPath) {
								entries = append(entries, EntryPoint{
									Path:        absPath,
									Type:        "main",
									Description: "Rust binary target",
								})
							}
							break
						}
						if strings.HasPrefix(l, "[") {
							break
						}
					}
				}
			}
		}
	}

	return entries
}

func (t *ProjectAnalyzeTool) findPythonEntryPoints(dir string) []EntryPoint {
	var entries []EntryPoint

	candidates := []struct {
		file        string
		entryType   string
		description string
	}{
		{"main.py", "main", "Python main entry point"},
		{"app.py", "main", "Python application entry point"},
		{"manage.py", "cli", "Django management entry point"},
		{"cli.py", "cli", "CLI entry point"},
		{"__main__.py", "main", "Python package main entry point"},
		{"setup.py", "build", "Python setup/install entry point"},
		{"wsgi.py", "main", "WSGI application entry point"},
		{"asgi.py", "main", "ASGI application entry point"},
		{"src/__main__.py", "main", "Source package main entry point"},
		{"src/main.py", "main", "Source main entry point"},
	}

	for _, c := range candidates {
		path := filepath.Join(dir, c.file)
		if fileExists(path) {
			entries = append(entries, EntryPoint{
				Path:        path,
				Type:        c.entryType,
				Description: c.description,
			})
		}
	}

	return entries
}

func (t *ProjectAnalyzeTool) findGenericEntryPoints(dir string) []EntryPoint {
	var entries []EntryPoint

	// Generic entry point detection for unknown project types
	candidates := []struct {
		file        string
		entryType   string
		description string
	}{
		{"main.go", "main", "Go main entry point"},
		{"main.rs", "main", "Rust main entry point"},
		{"main.py", "main", "Python main entry point"},
		{"main.js", "main", "JavaScript main entry point"},
		{"main.ts", "main", "TypeScript main entry point"},
		{"index.js", "main", "JavaScript entry point"},
		{"index.ts", "main", "TypeScript entry point"},
		{"App.java", "main", "Java application entry point"},
		{"Main.java", "main", "Java main class"},
		{"Makefile", "build", "Make build entry point"},
		{"Dockerfile", "build", "Docker build entry point"},
	}

	for _, c := range candidates {
		path := filepath.Join(dir, c.file)
		if fileExists(path) {
			entries = append(entries, EntryPoint{
				Path:        path,
				Type:        c.entryType,
				Description: c.description,
			})
		}
	}

	return entries
}

func (t *ProjectAnalyzeTool) findConfigEntryPoints(dir string, pt ProjectType) []EntryPoint {
	var entries []EntryPoint

	configCandidates := []struct {
		file        string
		entryType   string
		description string
	}{
		{"go.mod", "config", "Go module definition"},
		{"package.json", "config", "Node.js package manifest"},
		{"Cargo.toml", "config", "Rust package manifest"},
		{"pyproject.toml", "config", "Python project configuration"},
		{"setup.py", "config", "Python setup configuration"},
		{"requirements.txt", "config", "Python dependencies"},
		{"tsconfig.json", "config", "TypeScript configuration"},
		{"webpack.config.js", "config", "Webpack build configuration"},
		{"vite.config.ts", "config", "Vite build configuration"},
		{"vite.config.js", "config", "Vite build configuration"},
		{"Dockerfile", "config", "Docker build configuration"},
		{"docker-compose.yml", "config", "Docker Compose configuration"},
		{"docker-compose.yaml", "config", "Docker Compose configuration"},
		{"Makefile", "config", "Make build configuration"},
		{".golangci.yml", "config", "Go linter configuration"},
		{".eslintrc.js", "config", "ESLint configuration"},
		{".eslintrc.json", "config", "ESLint configuration"},
		{".prettierrc", "config", "Prettier configuration"},
		{"pom.xml", "config", "Maven project configuration"},
		{"build.gradle", "config", "Gradle build configuration"},
		{"build.gradle.kts", "config", "Gradle Kotlin build configuration"},
	}

	for _, c := range configCandidates {
		path := filepath.Join(dir, c.file)
		if fileExists(path) {
			entries = append(entries, EntryPoint{
				Path:        path,
				Type:        c.entryType,
				Description: c.description,
			})
		}
	}

	return entries
}

// extractJSONStringValue extracts a string value from JSON content starting at a key.
func (t *ProjectAnalyzeTool) extractJSONStringValue(content, key string) string {
	// Find "key": "value" pattern
	pattern := `"` + key + `"`
	idx := strings.Index(content, pattern)
	if idx == -1 {
		return ""
	}

	rest := content[idx+len(pattern):]

	// Skip whitespace and colon
	for len(rest) > 0 && (rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\n' || rest[0] == ':' || rest[0] == '\r') {
		rest = rest[1:]
	}

	if len(rest) == 0 || rest[0] != '"' {
		return ""
	}

	rest = rest[1:]
	end := strings.Index(rest, `"`)
	if end == -1 {
		return ""
	}

	return rest[:end]
}

// ---------------------------------------------------------------------------
// Action: generate_summary
// ---------------------------------------------------------------------------

// ProjectSummary is a comprehensive project analysis report.
type ProjectSummary struct {
	ProjectType    string              `json:"project_type"`
	RootDir        string              `json:"root_dir"`
	VCS            string              `json:"vcs"`
	Structure      *StructureResult    `json:"structure,omitempty"`
	Dependencies   *DependencyResult   `json:"dependencies,omitempty"`
	Complexity     *ComplexityResult   `json:"complexity,omitempty"`
	EntryPoints    *EntryPointResult   `json:"entry_points,omitempty"`
}

func (t *ProjectAnalyzeTool) generateSummary(dir string) (interface{}, error) {
	// Run all analyses
	structureRaw, err := t.analyzeStructure(dir)
	if err != nil {
		return nil, fmt.Errorf("structure analysis failed: %w", err)
	}
	structure, ok := structureRaw.(StructureResult)
	if !ok {
		return nil, fmt.Errorf("structure analysis returned unexpected type")
	}

	depsRaw, err := t.analyzeDependencies(dir)
	if err != nil {
		return nil, fmt.Errorf("dependency analysis failed: %w", err)
	}
	deps, ok := depsRaw.(DependencyResult)
	if !ok {
		return nil, fmt.Errorf("dependency analysis returned unexpected type")
	}

	complexityRaw, err := t.analyzeComplexity(dir)
	if err != nil {
		return nil, fmt.Errorf("complexity analysis failed: %w", err)
	}
	complexity, ok := complexityRaw.(ComplexityResult)
	if !ok {
		return nil, fmt.Errorf("complexity analysis returned unexpected type")
	}

	entryPointsRaw, err := t.findEntryPoints(dir)
	if err != nil {
		return nil, fmt.Errorf("entry point analysis failed: %w", err)
	}
	entryPoints, ok := entryPointsRaw.(EntryPointResult)
	if !ok {
		return nil, fmt.Errorf("entry point analysis returned unexpected type")
	}

	return ProjectSummary{
		ProjectType:  structure.ProjectType,
		RootDir:      structure.RootDir,
		VCS:          structure.VCS,
		Structure:    &structure,
		Dependencies: &deps,
		Complexity:   &complexity,
		EntryPoints:  &entryPoints,
	}, nil
}
