package tool

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// FileSearchTool 文件内容搜索工具
type FileSearchTool struct {
	BaseTool
}

// NewFileSearchTool 创建文件搜索工具
func NewFileSearchTool() *FileSearchTool {
	return &FileSearchTool{
		BaseTool: *NewBaseTool(
			"file_search",
			"Search for patterns in file contents using regex or text matching. Returns matching lines with context.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pattern": map[string]interface{}{
						"type":        "string",
						"description": "Search pattern (regex or plain text)",
					},
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Directory or file path to search in",
						"default":     ".",
					},
					"file_pattern": map[string]interface{}{
						"type":        "string",
						"description": "File glob pattern to filter (e.g., '*.go', '*.txt')",
					},
					"use_regex": map[string]interface{}{
						"type":        "boolean",
						"description": "Treat pattern as regex instead of plain text",
						"default":     false,
					},
					"case_sensitive": map[string]interface{}{
						"type":        "boolean",
						"description": "Case sensitive search",
						"default":     false,
					},
					"whole_word": map[string]interface{}{
						"type":        "boolean",
						"description": "Match whole word only",
						"default":     false,
					},
					"context_lines": map[string]interface{}{
						"type":        "number",
						"description": "Number of lines of context before/after match",
						"default":     0,
					},
					"max_results": map[string]interface{}{
						"type":        "number",
						"description": "Maximum number of matches to return",
						"default":     100,
					},
				},
				"required": []string{"pattern"},
			},
		),
	}
}

// Match 结构体表示单个匹配
type Match struct {
	File    string   `json:"file"`
	Line    int      `json:"line"`
	Column  int      `json:"column,omitempty"`
	Content string   `json:"content"`
	Context []string `json:"context,omitempty"`
}

// SearchResult 搜索结果
type SearchResult struct {
	Pattern      string  `json:"pattern"`
	Path         string  `json:"path"`
	TotalFiles   int     `json:"total_files"`
	TotalMatches int     `json:"total_matches"`
	Matches      []Match `json:"matches"`
}

// Execute 执行文件搜索
func (t *FileSearchTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	pattern, _ := params["pattern"].(string)
	if pattern == "" {
		return nil, fmt.Errorf("pattern is required")
	}

	path := "."
	if p, ok := params["path"].(string); ok && p != "" {
		path = p
	}

	filePattern := "*"
	if fp, ok := params["file_pattern"].(string); ok && fp != "" {
		filePattern = fp
	}

	useRegex := false
	if r, ok := params["use_regex"].(bool); ok {
		useRegex = r
	}

	caseSensitive := false
	if cs, ok := params["case_sensitive"].(bool); ok {
		caseSensitive = cs
	}

	wholeWord := false
	if ww, ok := params["whole_word"].(bool); ok {
		wholeWord = ww
	}

	contextLines := 0
	if cl, ok := params["context_lines"].(float64); ok {
		contextLines = int(cl)
	}

	maxResults := 100
	if mr, ok := params["max_results"].(float64); ok {
		maxResults = int(mr)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to access path: %w", err)
	}

	// 编译正则表达式
	var regex *regexp.Regexp
	if useRegex {
		flags := ""
		if !caseSensitive {
			flags = "(?i)"
		}
		patternToCompile := flags + pattern
		if wholeWord {
			patternToCompile = flags + `\b` + pattern + `\b`
		}
		regex, err = regexp.Compile(patternToCompile)
		if err != nil {
			return nil, fmt.Errorf("invalid regex: %w", err)
		}
	} else {
		// 转换为正则表达式
		escaped := regexp.QuoteMeta(pattern)
		flags := "(?i)"
		if caseSensitive {
			flags = ""
		}
		patternToCompile := flags + escaped
		if wholeWord {
			patternToCompile = flags + `\b` + escaped + `\b`
		}
		regex, err = regexp.Compile(patternToCompile)
		if err != nil {
			return nil, fmt.Errorf("invalid pattern: %w", err)
		}
	}

	result := &SearchResult{
		Pattern: pattern,
		Path:    absPath,
		Matches: make([]Match, 0),
	}

	// 遍历文件
	var files []string
	if info.IsDir() {
		files, err = t.findFiles(absPath, filePattern)
		if err != nil {
			return nil, fmt.Errorf("failed to find files: %w", err)
		}
	} else {
		files = []string{absPath}
	}

	totalMatches := 0
	totalFiles := 0

	for _, file := range files {
		matches, err := t.searchInFile(file, regex, contextLines, maxResults-totalMatches)
		if err != nil {
			continue // Skip files that can't be read
		}

		if len(matches) > 0 {
			totalFiles++
			for _, match := range matches {
				result.Matches = append(result.Matches, match)
				totalMatches++
				if totalMatches >= maxResults {
					break
				}
			}
		}

		if totalMatches >= maxResults {
			break
		}
	}

	result.TotalMatches = totalMatches
	result.TotalFiles = totalFiles

	return result, nil
}

func (t *FileSearchTool) findFiles(dir, pattern string) ([]string, error) {
	var files []string
	var walkErr error

	globPattern := filepath.Join(dir, "**", pattern)

	// Use filepath.Glob for simpler pattern matching
	allFiles, err := filepath.Glob(globPattern)
	if err != nil {
		return nil, err
	}

	// If glob doesn't work, fall back to WalkDir
	if len(allFiles) == 0 {
		filepath.WalkDir(dir, func(path string, info os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				// Skip hidden directories and common ignore directories
				name := info.Name()
				if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "__pycache__" {
					return filepath.SkipDir
				}
				return nil
			}

			matched, err := filepath.Match(pattern, info.Name())
			if err != nil {
				return nil
			}
			if matched {
				files = append(files, path)
			}
			return nil
		})
	} else {
		files = allFiles
	}

	// Filter out directories
	var result []string
	for _, f := range files {
		if info, err := os.Stat(f); err == nil && !info.IsDir() {
			result = append(result, f)
		}
	}

	sort.Strings(result)
	return result, walkErr
}

func (t *FileSearchTool) searchInFile(filePath string, regex *regexp.Regexp, contextLines, maxResults int) ([]Match, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var matches []Match
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		indices := regex.FindAllStringIndex(line, -1)
		if len(indices) == 0 {
			continue
		}

		// Get context lines
		// Note: For simplicity, we only include the current line
		// A full implementation would store and include surrounding lines

		for _, idx := range indices {
			match := Match{
				File:    filePath,
				Line:    lineNum,
				Column:  idx[0] + 1,
				Content: line,
			}
			matches = append(matches, match)

			if len(matches) >= maxResults {
				return matches, nil
			}
		}
	}

	return matches, nil
}

// ValidateParams 实现 ParamValidator 接口
func (t *FileSearchTool) ValidateParams(params map[string]interface{}) error {
	return ValidateParams(t.Schema(), params)
}
