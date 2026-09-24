package tool

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ErrFileTooLarge 表示文件超过搜索大小限制（跳过扫描）
var ErrFileTooLarge = errors.New("file too large to scan")

// maxScannedFileSize 单个文件参与扫描的大小上限（10MB）。
// 超过则整文件跳过，并计入 SearchResult.FilesTooLarge。
const maxScannedFileSize = 10 << 20

// maxScanResultsLimit 单次搜索返回匹配数的硬上限，防止 max_results 被传成
// 天文数字后把上下文撑爆。
const maxScanResultsLimit = 10000

// ignoredSearchDirs 搜索时直接跳过的目录名。以 "." 开头的隐藏目录由前缀
// 判断统一处理（.git 等自然被覆盖）。
var ignoredSearchDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"__pycache__":  true,
}

// FileSearchTool 文件内容搜索工具
type FileSearchTool struct {
	BaseTool
}

// NewFileSearchTool 创建文件搜索工具
func NewFileSearchTool() *FileSearchTool {
	return &FileSearchTool{
		BaseTool: *NewBaseTool(
			"file_search",
			"Search for patterns in file contents using regex or text matching. Returns matching lines with context. "+
				"Plain text by default (regex metacharacters such as | ( ) [ ] ^ $ are matched literally unless use_regex=true). "+
				"Works with LF, CRLF and lone-CR line endings; case_sensitive defaults to false.",
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
						"type": "string",
						"description": "File glob to filter by, matched against the file name and the path relative to `path`. " +
							"Supports '*.go' (any depth), 'sub/*.go', '**/*.go' and '{a,b}' brace groups (e.g. '*.{go,md}'). " +
							"Ignored when `path` is a single file.",
						"default": "*",
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
						"type": "boolean",
						"description": "Match whole word only. Word boundaries are added on each side of the pattern " +
							"only where that side starts/ends with a word character, so patterns like '@foo' still match.",
						"default": false,
					},
					"context_lines": map[string]interface{}{
						"type":        "number",
						"description": "Number of lines of context before/after match (max 100)",
						"default":     0,
					},
					"max_results": map[string]interface{}{
						"type":        "number",
						"description": "Maximum number of matches to return (max 10000)",
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
	TotalFiles   int     `json:"total_files"` // 含匹配的文件数（不是被扫描的文件数）
	TotalMatches int     `json:"total_matches"`
	Matches      []Match `json:"matches"`

	// 以下字段回显"实际生效的匹配条件"与扫描统计。零匹配是最难排查的
	// 情况，调用方需要能看出是不是 use_regex/case_sensitive/file_pattern
	// 被默认值吃掉了，而不是靠猜。
	UseRegex      bool   `json:"use_regex"`
	CaseSensitive bool   `json:"case_sensitive"`
	WholeWord     bool   `json:"whole_word"`
	FilePattern   string `json:"file_pattern"`
	FilesScanned  int    `json:"files_scanned"`
	FilesTooLarge int    `json:"files_skipped_too_large,omitempty"`
	Hint          string `json:"hint,omitempty"`
}

// Execute 执行文件搜索
func (t *FileSearchTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	pattern := paramString(params, "pattern")
	if strings.TrimSpace(pattern) == "" {
		return nil, fmt.Errorf("pattern is required")
	}

	path := paramString(params, "path")
	if path == "" {
		path = "."
	}

	filePattern := paramString(params, "file_pattern")
	if filePattern == "" {
		filePattern = "*"
	}

	useRegex := paramBool(params, "use_regex", false)
	caseSensitive := paramBool(params, "case_sensitive", false)
	wholeWord := paramBool(params, "whole_word", false)

	contextLines := 0
	if v := paramInt(params, "context_lines"); v > 0 {
		contextLines = v
	}
	if contextLines > 100 {
		contextLines = 100
	}

	maxResults := 100
	if v := paramInt(params, "max_results"); v > 0 {
		maxResults = v
	}
	if maxResults > maxScanResultsLimit {
		maxResults = maxScanResultsLimit
	}

	absPath, err := resolvePath(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to access path: %w", err)
	}

	regex, err := buildSearchRegex(pattern, useRegex, caseSensitive, wholeWord)
	if err != nil {
		return nil, err
	}

	result := &SearchResult{
		Pattern:       pattern,
		Path:          absPath,
		Matches:       make([]Match, 0),
		UseRegex:      useRegex,
		CaseSensitive: caseSensitive,
		WholeWord:     wholeWord,
		FilePattern:   filePattern,
	}

	// 遍历文件
	var files []string
	if info.IsDir() {
		files, err = t.findFiles(absPath, filePattern)
		if err != nil {
			return nil, fmt.Errorf("failed to find files: %w", err)
		}
	} else {
		// 单文件目标：file_pattern 不参与过滤（避免用户传了 file_pattern
		// 却意外搜了个空）。
		files = []string{absPath}
	}

	totalMatches := 0
	totalFiles := 0
	scanned := 0
	tooLarge := 0

	for _, file := range files {
		// 尊重上游取消/超时（例如工具执行超时、用户中断），
		// 避免大目录搜索时无视 deadline 一直跑到底。
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("search interrupted after %d matches: %w", totalMatches, err)
		}

		matches, err := t.searchInFile(ctx, file, regex, contextLines, maxResults-totalMatches)
		if err != nil {
			if errors.Is(err, ErrFileTooLarge) {
				tooLarge++
				continue
			}
			// 中断必须向上传播：以前这里一律 continue，会把 deadline
			// 之后每个文件都当成"读不了"静默跳过，返回一个看似成功的
			// 空结果。
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, fmt.Errorf("search interrupted after %d matches: %w", totalMatches, ctxErr)
			}
			continue // 无权限/已删除等：跳过该文件
		}
		scanned++

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
	result.FilesScanned = scanned
	result.FilesTooLarge = tooLarge
	result.Hint = emptyResultHint(result, len(files))

	return result, nil
}

// buildSearchRegex 把工具参数编译成最终正则。
//
// whole_word 只在模式端点为单词字符时才补 \b：旧实现无条件两侧加 \b，
// 于是 "@foo"、"#tag"、"foo(" 这类以非单词字符开头/结尾的模式永远匹配不上
// ——这是"某些模式不匹配"的一个真实来源。非单词字符一侧本来就无边界可言，
// 不加反而符合 grep -w 的直觉。
func buildSearchRegex(pattern string, useRegex, caseSensitive, wholeWord bool) (*regexp.Regexp, error) {
	body := pattern
	if !useRegex {
		body = regexp.QuoteMeta(pattern)
	}
	if wholeWord {
		body = applyWordBoundaries(body, pattern)
	}
	if !caseSensitive {
		// (?i) 必须放在最前：Go 的正则 flag 只对其后（同一分组内）生效。
		body = "(?i)" + body
	}
	regex, err := regexp.Compile(body)
	if err != nil {
		if useRegex {
			return nil, fmt.Errorf("invalid regex: %w", err)
		}
		return nil, fmt.Errorf("invalid pattern: %w", err)
	}
	return regex, nil
}

// applyWordBoundaries 按 raw（用户原始模式）的端点决定加哪一侧的 \b。
func applyWordBoundaries(body, raw string) string {
	if raw == "" {
		return body
	}
	if first, _ := utf8.DecodeRuneInString(raw); isWordRune(first) {
		body = `\b` + body
	}
	if last, _ := utf8.DecodeLastRuneInString(raw); isWordRune(last) {
		body = body + `\b`
	}
	return body
}

// isWordRune 判断是否属于 \w（字母/数字/下划线），端点判断用。
func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// findFiles 收集 dir 下所有匹配 pattern 的普通文件。
//
// filepath.Glob 的 "**" 只是普通的 "*"，不会跨目录分隔符递归匹配；这里统一
// 用 os.Root 做显式递归遍历：
//   - 正确递归所有子目录；
//   - 跳过隐藏目录与 node_modules/vendor/__pycache__；
//   - 通过 os.Root 防止符号链接逃逸出搜索根目录；
//   - file_pattern 同时按文件名与相对路径匹配（见 matchFilePattern）。
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
			if path != "." && (strings.HasPrefix(name, ".") || ignoredSearchDirs[name]) {
				return fs.SkipDir
			}
			return nil
		}
		rel := path
		if rel == "." {
			rel = d.Name()
		}
		if matchFilePattern(pattern, rel, d.Name()) {
			files = append(files, filepath.Join(dir, filepath.FromSlash(rel)))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(files)
	return files, nil
}

// matchFilePattern 判断相对路径 rel（base 为其文件名）是否命中 file_pattern。
//
// 旧实现只做 filepath.Match(pattern, d.Name())，也就是只拿文件名去比，于是
// "sub/*.go"、"internal/**/*.go" 这类带目录的模式必然一个都匹配不上。
// 现在的规则：
//   - 不含 "/" 的模式仍按文件名匹配，保持 "*.go 匹配任意层级" 的旧行为；
//   - 含 "/" 的模式按相对路径匹配，支持 "**" 跨层与 "{a,b}" 分组；
//   - 含 "/" 的模式额外尝试 "**/" 前缀，兼容用户写成仓库根相对路径而
//     实际搜索根在子目录的情况。
func matchFilePattern(pattern, rel, base string) bool {
	for _, alt := range expandBraces(pattern) {
		alt = filepath.ToSlash(strings.TrimSpace(alt))
		if alt == "" {
			continue
		}
		if !strings.Contains(alt, "/") {
			if ok, err := filepath.Match(alt, base); err == nil && ok {
				return true
			}
			continue
		}
		if matchGlobSegments(alt, rel) {
			return true
		}
		if matchGlobSegments("**/"+strings.TrimPrefix(alt, "/"), rel) {
			return true
		}
	}
	return false
}

// expandBraces 展开单层的 {...} 分组，例如 "*.{go,md}" -> ["*.go","*.md"]。
// 最多展开 64 项，防止病态输入把搜索放大。
func expandBraces(pattern string) []string {
	open := strings.IndexByte(pattern, '{')
	if open < 0 {
		return []string{pattern}
	}
	rel := strings.IndexByte(pattern[open+1:], '}')
	if rel < 0 {
		return []string{pattern} // 没有闭合的 }：当作普通字符
	}
	closeIdx := open + 1 + rel
	prefix := pattern[:open]
	body := pattern[open+1 : closeIdx]
	suffix := pattern[closeIdx+1:]

	var out []string
	for _, part := range strings.Split(body, ",") {
		for _, rest := range expandBraces(suffix) {
			out = append(out, prefix+strings.TrimSpace(part)+rest)
			if len(out) >= 64 {
				return out
			}
		}
	}
	if len(out) == 0 {
		return []string{pattern}
	}
	return out
}

// matchGlobSegments 按路径段匹配 glob，支持 "**" 跨任意层。
func matchGlobSegments(pattern, path string) bool {
	p := strings.Split(strings.Trim(filepath.ToSlash(pattern), "/"), "/")
	s := strings.Split(strings.Trim(filepath.ToSlash(path), "/"), "/")
	return matchSegments(p, s)
}

func matchSegments(p, s []string) bool {
	if len(p) == 0 {
		return len(s) == 0
	}
	if p[0] == "**" {
		for i := 0; i <= len(s); i++ {
			if matchSegments(p[1:], s[i:]) {
				return true
			}
		}
		return false
	}
	if len(s) == 0 {
		return false
	}
	ok, err := filepath.Match(p[0], s[0])
	if err != nil || !ok {
		return false
	}
	return matchSegments(p[1:], s[1:])
}

func (t *FileSearchTool) searchInFile(ctx context.Context, filePath string, regex *regexp.Regexp, contextLines, maxResults int) ([]Match, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// 大文件保护：跳过超大文件，避免扫描日志/二进制导致超时
	if info, statErr := file.Stat(); statErr == nil && info.Size() > maxScannedFileSize {
		return nil, ErrFileTooLarge
	}

	if maxResults <= 0 {
		return nil, nil
	}

	if contextLines <= 0 {
		return t.scanLines(ctx, filePath, file, regex, maxResults)
	}
	return t.scanLinesWithContext(ctx, filePath, file, regex, contextLines, maxResults)
}

// forEachLine 逐行读取，且不设单行长度上限。
//
// 旧实现用 bufio.Scanner（默认 64KB 单行上限），遇到压缩后的 JS/JSON、
// 单行大日志这类超长行会直接返回 bufio.ErrTooLong，整个文件被当成"读不了"
// 静默跳过 —— 表现为"这个文件里任何模式都搜不到"，是最难排查的漏匹配。
//
// 同时这里把孤立 \r（经典 Mac 换行）也当换行符：否则整个文件会被当成一行，
// ^ $ 与逐行语义全部失效。若存在 \r\n，先按 \n 切分、再削掉尾部 \r，行为与
// 之前一致。
func forEachLine(ctx context.Context, file *os.File, fn func(lineNum int, text string) bool) error {
	reader := bufio.NewReaderSize(file, 64*1024)
	lineNum := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		chunk, readErr := reader.ReadString('\n')
		if len(chunk) > 0 {
			chunk = strings.TrimSuffix(chunk, "\n")
			chunk = strings.TrimSuffix(chunk, "\r")
			for _, part := range strings.Split(chunk, "\r") {
				lineNum++
				if !fn(lineNum, part) {
					return nil
				}
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
	}
}

// scanLines 逐行扫描并收集匹配（无上下文）
func (t *FileSearchTool) scanLines(ctx context.Context, filePath string, file *os.File, regex *regexp.Regexp, maxResults int) ([]Match, error) {
	var matches []Match
	err := forEachLine(ctx, file, func(lineNum int, line string) bool {
		for _, idx := range regex.FindAllStringIndex(line, -1) {
			matches = append(matches, Match{
				File:    filePath,
				Line:    lineNum,
				Column:  idx[0] + 1,
				Content: line,
			})
			if len(matches) >= maxResults {
				return false
			}
		}
		return true
	})
	return matches, err
}

// scanLinesWithContext 扫描文件并为每个匹配附带前后 N 行上下文
func (t *FileSearchTool) scanLinesWithContext(ctx context.Context, filePath string, file *os.File, regex *regexp.Regexp, contextLines, maxResults int) ([]Match, error) {
	var lines []string
	if err := forEachLine(ctx, file, func(_ int, text string) bool {
		lines = append(lines, text)
		return true
	}); err != nil {
		return nil, err
	}

	var matches []Match
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

		// 每个匹配都独立带一份完整上下文窗口。旧实现用 lastCtxEnd 去重，
		// 当匹配行落在上一个窗口内部时会走到 start>=end 的 continue 分支，
		// 该匹配的上下文（含 "<<< MATCH" 标记）被整段丢掉 —— 相邻行的
		// 多个匹配都会中招。这里保留重复行，换取"每个匹配上下文必然完整"。
		contextSlice := make([]string, 0, end-start)
		for ln := start; ln < end; ln++ {
			if ln == i {
				contextSlice = append(contextSlice, lines[ln]+"  // <<< MATCH")
			} else {
				contextSlice = append(contextSlice, lines[ln])
			}
		}

		for _, idx := range indices {
			matches = append(matches, Match{
				File:    filePath,
				Line:    i + 1,
				Column:  idx[0] + 1,
				Content: line,
				Context: contextSlice,
			})
			if len(matches) >= maxResults {
				return matches, nil
			}
		}
	}
	return matches, nil
}

// emptyResultHint 在零匹配时生成一条可读提示，说明实际生效的匹配条件，
// 让调用方（尤其是 LLM）不用靠猜就能修正参数。
func emptyResultHint(res *SearchResult, filesFound int) string {
	if res.TotalMatches > 0 {
		return ""
	}
	if filesFound == 0 {
		return fmt.Sprintf("no file matched file_pattern %q under %s; widen it (e.g. %q or \"**/*.go\")",
			res.FilePattern, res.Path, "*")
	}

	parts := []string{fmt.Sprintf("scanned %d file(s), 0 matches", res.FilesScanned)}
	if res.FilesTooLarge > 0 {
		parts = append(parts, fmt.Sprintf("%d file(s) skipped for exceeding 10MB", res.FilesTooLarge))
	}
	if res.CaseSensitive {
		parts = append(parts, "match was CASE-SENSITIVE")
	} else {
		parts = append(parts, "match was case-insensitive")
	}
	if res.UseRegex {
		parts = append(parts, "pattern was treated as a REGEX")
	} else {
		parts = append(parts, "pattern was treated as PLAIN TEXT, so regex metacharacters were literal - "+
			"pass use_regex=true for alternation (a|b), anchors (^...$) or character classes ([a-z])")
	}
	if res.WholeWord {
		parts = append(parts, "whole_word=true only matches when the pattern is bounded by word characters")
	}
	return strings.Join(parts, "; ")
}

// ValidateParams 实现 ParamValidator 接口
func (t *FileSearchTool) ValidateParams(params map[string]interface{}) error {
	return ValidateParams(t.Schema(), params)
}
