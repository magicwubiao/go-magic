package tool

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ============================================================================
// List Files Tool
// ============================================================================

type ListFilesTool struct{}

func (t *ListFilesTool) Name() string {
	return "list_files"
}

func (t *ListFilesTool) Description() string {
	return "List files and directories in a specified path"
}

func (t *ListFilesTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "The directory path to list",
				"default":     ".",
			},
			"pattern": map[string]interface{}{
				"type":        "string",
				"description": "Glob pattern to filter files (optional)",
			},
			"include_hidden": map[string]interface{}{
				"type":        "boolean",
				"description": "Include hidden files",
				"default":     false,
			},
		},
		// Note: required is omitted when empty since all params are optional
	}
}

func (t *ListFilesTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	path := "."
	if p, ok := args["path"].(string); ok {
		path = p
	}

	includeHidden := false
	if h, ok := args["include_hidden"].(bool); ok {
		includeHidden = h
	}

	absPath, err := resolvePath(ctx, path)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var files []map[string]interface{}
	for _, entry := range entries {
		// Skip hidden files unless requested
		if !includeHidden && strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		info, _ := entry.Info()
		fileInfo := map[string]interface{}{
			"name":   entry.Name(),
			"is_dir": entry.IsDir(),
			"size":   0,
		}
		if info != nil {
			fileInfo["size"] = info.Size()
		}
		files = append(files, fileInfo)
	}

	return map[string]interface{}{
		"path":  absPath,
		"count": len(files),
		"files": files,
	}, nil
}

// ============================================================================
// Search In Files Tool (fix: expose use_regex and pass through to file_search)
// ============================================================================

type SearchInFilesTool struct{}

func (t *SearchInFilesTool) Name() string {
	return "search_in_files"
}

func (t *SearchInFilesTool) Description() string {
	return "Search for a pattern in file contents. Supports both plain text and regular expressions (set use_regex=true for regex). Returns matching lines with optional context."
}

func (t *SearchInFilesTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pattern": map[string]interface{}{
				"type":        "string",
				"description": "The search pattern (plain text or regex when use_regex=true)",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Directory or file path to search in",
				"default":     ".",
			},
			"file_pattern": map[string]interface{}{
				"type":        "string",
				"description": "File glob pattern to filter (e.g., '*.go', '*.txt')",
				"default":     "*",
			},
			"use_regex": map[string]interface{}{
				"type":        "boolean",
				"description": "Treat pattern as a regular expression instead of plain text. Enables alternation (a|b), anchors (^...$), character classes ([a-z]), etc.",
				"default":     false,
			},
			"case_sensitive": map[string]interface{}{
				"type":        "boolean",
				"description": "Case sensitive search",
				"default":     false,
			},
			"whole_word": map[string]interface{}{
				"type":        "boolean",
				"description": "Match whole word only (word boundaries)",
				"default":     false,
			},
			"context_lines": map[string]interface{}{
				"type":        "number",
				"description": "Number of lines of context before/after each match",
				"default":     0,
			},
			"max_results": map[string]interface{}{
				"type":        "number",
				"description": "Maximum number of matches to return",
				"default":     100,
			},
		},
		"required": []string{"pattern"},
	}
}

func (t *SearchInFilesTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	pattern, ok := args["pattern"].(string)
	if !ok || pattern == "" {
		return nil, fmt.Errorf("pattern argument is required")
	}

	searchPath := "."
	if p, ok := args["path"].(string); ok && p != "" {
		searchPath = p
	}

	filePattern := "*"
	if fp, ok := args["file_pattern"].(string); ok && fp != "" {
		filePattern = fp
	}

	useRegex := false
	if r, ok := args["use_regex"].(bool); ok {
		useRegex = r
	}

	caseSensitive := false
	if cs, ok := args["case_sensitive"].(bool); ok {
		caseSensitive = cs
	}

	wholeWord := false
	if ww, ok := args["whole_word"].(bool); ok {
		wholeWord = ww
	}

	contextLines := 0
	if cl, ok := args["context_lines"].(float64); ok {
		contextLines = int(cl)
	}

	maxResults := 100
	if mr, ok := args["max_results"].(float64); ok {
		maxResults = int(mr)
	}

	// Use the advanced file_search tool for actual implementation,
	// passing through all regex-related options.
	searcher := &FileSearchTool{}
	return searcher.Execute(ctx, map[string]interface{}{
		"pattern":        pattern,
		"path":           searchPath,
		"file_pattern":   filePattern,
		"use_regex":      useRegex,
		"case_sensitive": caseSensitive,
		"whole_word":     wholeWord,
		"context_lines":  contextLines,
		"max_results":    maxResults,
	})
}

// ============================================================================
// Read File Tool (Enhanced: explicit line offset/limit + large-file pagination)
// ============================================================================

type ReadFileTool struct{}

func (t *ReadFileTool) Name() string {
	return "read_file"
}

func (t *ReadFileTool) Description() string {
	return "Read a file. Use offset (1-based line number) and limit (max lines) to read a specific range -- required for large files, since results are truncated when too long. If the content is truncated, continue reading with offset = last shown line + 1."
}

func (t *ReadFileTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "The path to the file to read",
			},
			"limit": map[string]interface{}{
				"type":        "number",
				"description": "Maximum number of lines to read (default: all lines)",
			},
			"offset": map[string]interface{}{
				"type":        "number",
				"description": "Line number to start reading from (1-based, default: 1 = beginning of file)",
			},
		},
		"required": []string{"path"},
	}
}

func (t *ReadFileTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	path, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path argument is required")
	}

	absPath, err := resolvePath(ctx, path)
	if err != nil {
		return nil, err
	}

	// Handle offset and limit (1-based offset)
	offset := 0 // 0-based index into lines
	limit := 0

	if o, ok := args["offset"].(float64); ok {
		offset = int(o) - 1 // Convert to 0-based
		if offset < 0 {
			offset = 0
		}
	}
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}

	// Stream the file line by line instead of os.ReadFile: memory use is
	// bounded by the returned window, not by the file size, so multi-GB
	// logs no longer load entirely into RAM just to read 100 lines.
	f, err := os.Open(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	defer f.Close()

	const maxLineBytes = 4 * 1024 * 1024 // 4MB per line guard
	scanner := bufio.NewScanner(bufio.NewReaderSize(f, 256*1024))
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	// Keep trailing \r on CRLF files: previous os.ReadFile+strings.Split
	// semantics preserved them, and edit_file's verbatim old_content
	// matching depends on read_file returning the exact bytes.
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			return i + 1, data[:i], nil
		}
		if atEOF {
			return len(data), data, nil
		}
		return 0, nil, nil
	})

	var (
		lines      []string
		totalLines int
	)
	for scanner.Scan() {
		totalLines++
		if totalLines > offset && (limit <= 0 || len(lines) < limit) {
			lines = append(lines, scanner.Text())
		}
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return nil, fmt.Errorf("file contains a line longer than %d MB; split the file or use a different tool", maxLineBytes/(1024*1024))
		}
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// If offset is beyond the file, return a clear message instead of
	// silently falling back to the beginning of the file.
	if offset > 0 && offset >= totalLines {
		return map[string]interface{}{
			"path":      absPath,
			"total":     totalLines,
			"read":      0,
			"offset":    offset,
			"content":   "",
			"type":      detectFileType(absPath, strings.Join(lines, "\n")),
			"note":      fmt.Sprintf("offset %d is beyond the end of file (total %d lines)", offset+1, totalLines),
			"truncated": false,
		}, nil
	}

	readContent := strings.Join(lines, "\n")

	// Guard against silent agent-level truncation: when the caller did not
	// pass limit, the agent truncates oversized tool results (~50K chars)
	// WITHOUT any truncation signal, so the model believes it saw the whole
	// file. Cap the window here, flag it, and tell the model how to continue.
	const maxNoLimitChars = 48000
	if limit <= 0 && len([]rune(readContent)) > maxNoLimitChars {
		// rune 边界安全截断，避免切断多字节字符产生乱码
		runes := []rune(readContent)
		cut := string(runes[:maxNoLimitChars])
		if idx := strings.LastIndexByte(cut, '\n'); idx > 0 {
			cut = cut[:idx]
		}
		shownLines := strings.Count(cut, "\n") + 1
		readContent = cut
		lines = lines[:shownLines]
	}

	// Track whether the caller's requested range covers the whole file.
	// If the caller did not pass offset (started at line 1) and the
	// returned range is shorter than the file, the result was truncated
	// by the request -- signal that reading should continue via offset.
	truncated := limit > 0 && offset+limit < totalLines
	if limit <= 0 && len(readContent) > 0 {
		// no-limit path: truncated when we did not return all lines
		truncated = len(lines) < totalLines
	}

	result := map[string]interface{}{
		"path":      absPath,
		"total":     totalLines,
		"read":      len(lines),
		"offset":    offset,
		"firstLine": offset + 1,
		"content":   readContent,
		"type":      detectFileType(absPath, readContent),
		"truncated": truncated,
	}

	// If truncated and the caller did not specify offset, add a hint so
	// the model knows to continue from the next chunk.
	if truncated && offset == 0 {
		result["note"] = fmt.Sprintf("file has %d lines; only the first %d lines were returned. Continue reading with offset=%d to get the rest.", totalLines, len(lines), len(lines)+1)
	}

	return result, nil
}

func detectFileType(path, content string) string {
	ext := strings.ToLower(filepath.Ext(path))

	switch ext {
	case ".go":
		return "go source"
	case ".py":
		return "python source"
	case ".js", ".jsx":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".json":
		return "json"
	case ".md", ".markdown":
		return "markdown"
	case ".html", ".htm":
		return "html"
	case ".css":
		return "css"
	case ".sql":
		return "sql"
	case ".sh", ".bash":
		return "shell script"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	default:
		if strings.HasPrefix(content, "#!/bin") {
			return "shell script"
		}
		if strings.HasPrefix(content, "{") || strings.HasPrefix(content, "[") {
			return "possibly json"
		}
		return "text"
	}
}

// ============================================================================
// Write File Tool (Enhanced)
// ============================================================================

type WriteFileTool struct{}

func (t *WriteFileTool) Name() string {
	return "write_file"
}

func (t *WriteFileTool) Description() string {
	return "Write or create a file with the given content"
}

func (t *WriteFileTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "The path to the file to write",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "The content to write",
			},
			"append": map[string]interface{}{
				"type":        "boolean",
				"description": "Append to existing file instead of overwriting",
				"default":     false,
			},
		},
		"required": []string{"path", "content"},
	}
}

func (t *WriteFileTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	path, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path argument is required")
	}

	content, ok := args["content"].(string)
	if !ok {
		return nil, fmt.Errorf("content argument is required")
	}

	absPath, err := resolvePath(ctx, path)
	if err != nil {
		return nil, err
	}

	security := FileSecurityFromContext(ctx)

	if security.MaxFileSizeKB > 0 && len(content) > security.MaxFileSizeKB*1024 {
		return nil, fmt.Errorf("file size %d bytes exceeds maximum allowed size of %d KB", len(content), security.MaxFileSizeKB)
	}

	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, security.DefaultDirMode); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	appendMode := false
	if a, ok := args["append"].(bool); ok {
		appendMode = a
	}

	var err2 error
	if appendMode {
		f, ferr := os.OpenFile(absPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, security.DefaultFileMode)
		if ferr != nil {
			err2 = ferr
		} else {
			if _, werr := f.Write([]byte(content)); werr != nil {
				err2 = werr
			}
			if cerr := f.Close(); cerr != nil && err2 == nil {
				err2 = cerr
			}
		}
	} else {
		err2 = os.WriteFile(absPath, []byte(content), security.DefaultFileMode)
	}

	if err2 != nil {
		return nil, fmt.Errorf("failed to write file: %w", err2)
	}

	// Get file info
	info, _ := os.Stat(absPath)

	result := map[string]interface{}{
		"success": true,
		"path":    absPath,
		"bytes":   len(content),
		"lines":   strings.Count(content, "\n") + 1,
		"size":    info.Size(),
	}

	// Post-write lint (non-blocking)
	if issues, _ := LintFile(absPath); len(issues) > 0 {
		result["lint_warning"] = fmt.Sprintf("⚠ Lint issues found:\n%s", strings.Join(issues, "\n"))
	}

	return result, nil
}

// ============================================================================
// Batch Helper: Convert JSON results to readable format
// ============================================================================

func jsonPrettyPrint(v interface{}) string {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(data)
}
