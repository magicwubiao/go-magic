package context

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// FileType represents the type of context file
type FileType int

const (
	TypeMarkdown FileType = iota
	TypeYAML
	TypeJSON
	TypeText
	TypeCode
)

// String returns the string representation of FileType
func (ft FileType) String() string {
	switch ft {
	case TypeMarkdown:
		return "markdown"
	case TypeYAML:
		return "yaml"
	case TypeJSON:
		return "json"
	case TypeText:
		return "text"
	case TypeCode:
		return "code"
	default:
		return "unknown"
	}
}

// ContextFile represents a context file entry
type ContextFile struct {
	Path       string   `json:"path"`
	Type       FileType `json:"type"`
	Weight     float64  `json:"weight"`      // Priority weight (0.0-1.0)
	Variables  []string `json:"variables"`  // Template variables
	MaxTokens  int      `json:"max_tokens"`  // Max tokens to include
	AutoLoad   bool     `json:"auto_load"`   // Auto-load on startup
	Tags       []string `json:"tags"`        // Category tags
	Content    string   `json:"-"`           // Actual content (not serialized)
	LoadedAt   int64    `json:"loaded_at"`   // When it was last loaded
}

// Manager handles project context files
type Manager struct {
	projectRoot   string
	configDir     string
	contextFiles  map[string]*ContextFile
	autoPatterns   []*regexp.Regexp
	loadedContent map[string]string
}

// Config holds context manager configuration
type Config struct {
	Enabled        bool     `yaml:"enabled"`
	ProjectRoot    string   `yaml:"project_root"`
	ContextFiles   []string `yaml:"context_files"`
	AutoLoad       []string `yaml:"auto_load_patterns"`
	ExcludePatterns []string `yaml:"exclude_patterns"`
	MaxContextSize int      `yaml:"max_context_size"` // in tokens
	IncludeHidden  bool     `yaml:"include_hidden"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		Enabled:        true,
		MaxContextSize: 8000,
		IncludeHidden:  false,
		AutoLoad: []string{
			"AGENTS.md",
			"README.md",
			"CONTEXT.md",
			".context/**",
			"docs/*.md",
		},
		ExcludePatterns: []string{
			"**/.git/**",
			"**/node_modules/**",
			"**/*.log",
			"**/vendor/**",
			"**/__pycache__/**",
		},
	}
}

// NewManager creates a new context manager
func NewManager(projectRoot, configDir string) *Manager {
	return &Manager{
		projectRoot:   projectRoot,
		configDir:     configDir,
		contextFiles:  make(map[string]*ContextFile),
		autoPatterns:  make([]*regexp.Regexp, 0),
		loadedContent: make(map[string]string),
	}
}

// LoadContext loads all context files for a project
func (m *Manager) LoadContext(cfg *Config) (string, error) {
	if !cfg.Enabled {
		return "", nil
	}

	var builder strings.Builder

	// Load auto-load patterns
	for _, pattern := range cfg.AutoLoad {
		files, err := m.findMatchingFiles(pattern, cfg.ExcludePatterns)
		if err != nil {
			continue
		}
		for _, file := range files {
			content, err := m.loadFile(file)
			if err != nil {
				continue
			}
			m.loadedContent[file] = content
			
			// Add header for context
			relPath, _ := filepath.Rel(m.projectRoot, file)
			builder.WriteString(fmt.Sprintf("\n\n## %s\n\n", relPath))
			builder.WriteString(content)
		}
	}

	// Load specific context files
	for _, file := range cfg.ContextFiles {
		fullPath := file
		if !filepath.IsAbs(file) {
			fullPath = filepath.Join(m.projectRoot, file)
		}
		content, err := m.loadFile(fullPath)
		if err != nil {
			continue
		}
		m.loadedContent[fullPath] = content
		
		relPath, _ := filepath.Rel(m.projectRoot, fullPath)
		builder.WriteString(fmt.Sprintf("\n\n## %s\n\n", relPath))
		builder.WriteString(content)
	}

	return builder.String(), nil
}

// loadFile reads and parses a context file
func (m *Manager) loadFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	content := string(data)
	
	// Handle different file types
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md":
		content = m.processMarkdown(content)
	case ".yaml", ".yml":
		content = m.processYAML(content)
	case ".json":
		content = m.processJSON(content)
	}

	return content, nil
}

// processMarkdown removes markdown formatting for cleaner context
func (m *Manager) processMarkdown(content string) string {
	// Remove code block markers for cleaner extraction
	lines := strings.Split(content, "\n")
	var result []string
	
	inCodeBlock := false
	for _, line := range lines {
		if strings.HasPrefix(line, "```") {
			inCodeBlock = !inCodeBlock
			continue
		}
		if !inCodeBlock {
			result = append(result, line)
		}
	}
	
	return strings.Join(result, "\n")
}

// processYAML extracts key-value pairs from YAML
func (m *Manager) processYAML(content string) string {
	// Simple YAML parsing - extract structure
	var result []string
	scanner := bufio.NewScanner(strings.NewReader(content))
	var indent int
	
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		
		// Track indentation for hierarchy
		spaces := len(line) - len(strings.TrimLeft(line, " "))
		indent = spaces / 2
		
		// Only show top-level and one level deep
		if indent <= 1 {
			result = append(result, line)
		}
	}
	
	return strings.Join(result, "\n")
}

// processJSON extracts and formats JSON
func (m *Manager) processJSON(content string) string {
	var data interface{}
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		return content
	}
	
	// Compact but readable output
	output, _ := json.MarshalIndent(data, "", "  ")
	return string(output)
}

// findMatchingFiles finds files matching glob patterns
func (m *Manager) findMatchingFiles(pattern string, excludePatterns []string) ([]string, error) {
	var results []string
	
	// Handle glob patterns
	if strings.Contains(pattern, "*") {
		matches, err := filepath.Glob(filepath.Join(m.projectRoot, pattern))
		if err != nil {
			return nil, err
		}
		results = append(results, matches...)
	} else {
		// Direct file path
		path := filepath.Join(m.projectRoot, pattern)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			results = append(results, path)
		}
	}
	
	// Filter excludes
	var filtered []string
	for _, file := range results {
		excluded := false
		for _, exclude := range excludePatterns {
			excludePath := filepath.Join(m.projectRoot, exclude)
			matched, _ := filepath.Match(excludePath, file)
			if matched || strings.Contains(file, strings.TrimPrefix(exclude, "**/")) {
				excluded = true
				break
			}
		}
		if !excluded {
			filtered = append(filtered, file)
		}
	}
	
	return filtered, nil
}

// GetLoadedContent returns the loaded content for a file
func (m *Manager) GetLoadedContent(path string) string {
	return m.loadedContent[path]
}

// RegisterContextFile registers a context file manually
func (m *Manager) RegisterContextFile(file *ContextFile) {
	m.contextFiles[file.Path] = file
}

// ListContextFiles returns all registered context files
func (m *Manager) ListContextFiles() []*ContextFile {
	files := make([]*ContextFile, 0, len(m.contextFiles))
	for _, f := range m.contextFiles {
		files = append(files, f)
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Weight > files[j].Weight
	})
	return files
}

// ExtractVariables extracts template variables from content
func (m *Manager) ExtractVariables(content string) []string {
	re := regexp.MustCompile(`\{\{(\w+)\}\}`)
	matches := re.FindAllStringSubmatch(content, -1)
	
	vars := make(map[string]bool)
	for _, match := range matches {
		if len(match) > 1 {
			vars[match[1]] = true
		}
	}
	
	result := make([]string, 0, len(vars))
	for v := range vars {
		result = append(result, v)
	}
	sort.Strings(result)
	return result
}

// RenderTemplate renders a template with variables
func (m *Manager) RenderTemplate(content string, variables map[string]string) string {
	result := content
	for key, value := range variables {
		result = strings.ReplaceAll(result, "{{"+key+"}}", value)
	}
	return result
}

// ContextSummary provides a summary of loaded context
type ContextSummary struct {
	TotalFiles    int            `json:"total_files"`
	TotalSize     int            `json:"total_size"`
	ByType        map[string]int `json:"by_type"`
	ByTag         map[string]int `json:"by_tag"`
	Variables     []string       `json:"variables"`
	ProjectRoot   string         `json:"project_root"`
}

// GetSummary returns a summary of the loaded context
func (m *Manager) GetSummary() *ContextSummary {
	summary := &ContextSummary{
		TotalFiles:  len(m.loadedContent),
		ProjectRoot: m.projectRoot,
		ByType:      make(map[string]int),
		ByTag:      make(map[string]int),
	}
	
	for path, content := range m.loadedContent {
		summary.TotalSize += len(content)
		
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".md":
			summary.ByType["markdown"]++
		case ".yaml", ".yml":
			summary.ByType["yaml"]++
		case ".json":
			summary.ByType["json"]++
		case ".txt":
			summary.ByType["text"]++
		default:
			summary.ByType["other"]++
		}
	}
	
	// Extract all variables
	allVars := make(map[string]bool)
	for _, content := range m.loadedContent {
		for _, v := range m.ExtractVariables(content) {
			allVars[v] = true
		}
	}
	for v := range allVars {
		summary.Variables = append(summary.Variables, v)
	}
	
	return summary
}

// SaveContext saves current context state
func (m *Manager) SaveContext() error {
	cachePath := filepath.Join(m.configDir, "context_cache.json")
	
	data, err := json.MarshalIndent(m.loadedContent, "", "  ")
	if err != nil {
		return err
	}
	
	return os.WriteFile(cachePath, data, 0644)
}

// LoadContextCache loads cached context
func (m *Manager) LoadContextCache() error {
	cachePath := filepath.Join(m.configDir, "context_cache.json")
	
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return err
	}
	
	return json.Unmarshal(data, &m.loadedContent)
}
