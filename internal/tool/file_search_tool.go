package tool

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ErrFileTooLarge 表示文件超过搜索大小限制（跳过扫描）
var ErrFileTooLarge = errors.New("file too large to scan")

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

	absPath, err := resolvePath(ctx, path)
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
		// 尊重上游取消/超时（例如工具执行超时、用户中断），
		// 避免大目录搜索时无视 deadline 一直跑到底。
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("search interrupted after %d matches: %w", totalMatches, err)
		}

		matches, err := t.searchInFile(ctx, file, regex, contextLines, maxResults-totalMatches)
		if err != nil {
			continue // Skip files that can't be read or are too large
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

// findFiles 收集 dir 下所有匹配 pattern 的普通文件。
// 注意：filepath.Glob 的 "**" 只是普通的 "*"，不会跨目录分隔符递归匹配，
// 导致 filepath.Join(dir, "**", pattern) 在多级子目录下几乎必然匹配失败；
// 而旧的 WalkDir 回退分支又只在 glob 结果为空时才触发，语义混乱。
// 这里统一改为基于 os.Root 的显式递归遍历：
//   - 正确递归所有子目录；
//   - 跳过隐藏目录与 node_modules/vendor/__pycache__ 等常见忽略目录；
//   - 通过 os.Root 防止符号链接逃逸出搜索根目录。
func (t *FileSearchTool) findFiles(dir, pattern string) ([]string, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	var files []string
	err = fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 无权限/已删除的条目直接跳过
		}
		if d.IsDir() {
			name := d.Name()
			if path != "." && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "__pycache__") {
				return fs.SkipDir
			}
			return nil
		}
		matched, mErr := filepath.Match(pattern, d.Name())
		if mErr != nil {
			return nil
		}
		if matched {
			files = append(files, filepath.Join(dir, path))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(files)
	return files, nil
}

func (t *FileSearchTool) searchInFile(ctx context.Context, filePath string, regex *regexp.Regexp, contextLines, maxResults int) ([]Match, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// 大文件保护：跳过超过 10MB 的文件，避免扫描日志/二进制导致超时
	if info, statErr := file.Stat(); statErr == nil && info.Size() > 10<<20 {
		return nil, ErrFileTooLarge
	}

	if contextLines <= 0 {
		return t.scanLines(ctx, filePath, file, regex, maxResults)
	}
	return t.scanLinesWithContext(ctx, filePath, file, regex, contextLines, maxResults)
}

// scanLines 逐行扫描并收集匹配（无上下文）
func (t *FileSearchTool) scanLines(ctx context.Context, filePath string, file *os.File, regex *regexp.Regexp, maxResults int) ([]Match, error) {
	var matches []Match
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return matches, err
		}
		lineNum++
		line := scanner.Text()

		indices := regex.FindAllStringIndex(line, -1)
		if len(indices) == 0 {
			continue
		}

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
	return matches, scanner.Err()
}

// scanLinesWithContext 扫描文件并为每个匹配附带前后 N 行上下文
func (t *FileSearchTool) scanLinesWithContext(ctx context.Context, filePath string, file *os.File, regex *regexp.Regexp, contextLines, maxResults int) ([]Match, error) {
	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	var matches []Match
	lastCtxEnd := -1
	for i, line := range lines {
		if err := ctx.Err(); err != nil {
			return matches, err
		}
		indices := regex.FindAllStringIndex(line, -1)
		if len(indices) == 0 {
			continue
		}

		start := i - contextLines
		if start < 0 {
			start = 0
		}
		end := i + contextLines + 1
		if end > len(lines) {
			end = len(lines)
		}
		if start < lastCtxEnd {
			start = lastCtxEnd // 与上一个匹配的上下文重叠时去重
		}
		if start >= end {
			continue
		}
		contextBlock := strings.Join(lines[start:end], "\n")
		lastCtxEnd = end

		for _, idx := range indices {
			match := Match{
				File:    filePath,
				Line:    i + 1,
				Column:  idx[0] + 1,
				Content: line,
				Context: []string{contextBlock},
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
