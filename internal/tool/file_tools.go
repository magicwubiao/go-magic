package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
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
		"required": []string{},
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

	absPath, err := filepath.Abs(path)
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
// Search In Files Tool
// ============================================================================

type SearchInFilesTool struct{}

func (t *SearchInFilesTool) Name() string {
	return "search_in_files"
}

func (t *SearchInFilesTool) Description() string {
	return "Search for a pattern in file contents"
}

func (t *SearchInFilesTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pattern": map[string]interface{}{
				"type":        "string",
				"description": "The search pattern (text or regex)",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Directory to search in",
				"default":     ".",
			},
			"file_pattern": map[string]interface{}{
				"type":        "string",
				"description": "File pattern to match (e.g., *.go)",
				"default":     "*",
			},
			"case_sensitive": map[string]interface{}{
				"type":        "boolean",
				"description": "Case sensitive search",
				"default":     false,
			},
		},
		"required": []string{"pattern"},
	}
}

func (t *SearchInFilesTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	pattern, ok := args["pattern"].(string)
	if !ok {
		return nil, fmt.Errorf("pattern argument is required")
	}

	searchPath := "."
	if p, ok := args["path"].(string); ok {
		searchPath = p
	}

	filePattern := "*"
	if fp, ok := args["file_pattern"].(string); ok {
		filePattern = fp
	}

	caseSensitive := false
	if cs, ok := args["case_sensitive"].(bool); ok {
		caseSensitive = cs
	}

	// Use the advanced file_search tool for actual implementation
	searcher := &FileSearchTool{}
	return searcher.Execute(ctx, map[string]interface{}{
		"pattern":        pattern,
		"path":           searchPath,
		"file_pattern":   filePattern,
		"case_sensitive": caseSensitive,
		"use_regex":      false,
	})
}

// ============================================================================
// Web Extract Tool
// ============================================================================

type WebExtractTool struct{}

func (t *WebExtractTool) Name() string {
	return "web_extract"
}

func (t *WebExtractTool) Description() string {
	return "Extract and parse content from web pages"
}

func (t *WebExtractTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "URL to extract content from",
			},
			"query": map[string]interface{}{
				"type":        "string",
				"description": "CSS selector or query to extract specific content",
			},
		},
		"required": []string{"url"},
	}
}

func (t *WebExtractTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	url, ok := args["url"].(string)
	if !ok {
		return nil, fmt.Errorf("url is required")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; GoMagic/1.0)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	content := string(body)

	// Try to extract title
	title := extractTitle(content)

	// Try to extract main content
	mainContent := extractMainContent(content)

	return map[string]interface{}{
		"url":        url,
		"title":      title,
		"content":    mainContent,
		"raw_length": len(content),
	}, nil
}

func extractTitle(htmlContent string) string {
	start := strings.Index(htmlContent, "<title>")
	if start == -1 {
		return ""
	}
	end := strings.Index(htmlContent[start+7:], "</title>")
	if end == -1 {
		return ""
	}
	return strings.TrimSpace(htmlContent[start+7 : start+7+end])
}

func extractMainContent(htmlContent string) string {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return htmlContent
	}

	var content strings.Builder
	var extractText func(*html.Node)
	extractText = func(n *html.Node) {
		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				content.WriteString(text)
				content.WriteString(" ")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			// Skip script and style elements
			if c.Type == html.ElementNode {
				switch c.DataAtom {
				case atom.Script, atom.Style:
					continue
				}
			}
			extractText(c)
		}
	}

	extractText(doc)
	result := content.String()
	// Clean up whitespace
	result = strings.Join(strings.Fields(result), " ")

	if len(result) > 5000 {
		result = result[:5000] + "..."
	}
	return result
}

// ============================================================================
// Read File Tool (Enhanced)
// ============================================================================

type ReadFileTool struct{}

func (t *ReadFileTool) Name() string {
	return "read_file"
}

func (t *ReadFileTool) Description() string {
	return "Read the complete contents of a file"
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
				"description": "Maximum number of lines to read",
			},
			"offset": map[string]interface{}{
				"type":        "number",
				"description": "Line number to start reading from (1-based)",
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

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	content := string(data)

	// Handle offset and limit
	offset := 0
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

	lines := strings.Split(content, "\n")
	if offset > 0 && offset < len(lines) {
		lines = lines[offset:]
	}
	if limit > 0 && limit < len(lines) {
		lines = lines[:limit]
	}

	result := map[string]interface{}{
		"path":    absPath,
		"total":   len(strings.Split(content, "\n")),
		"read":    len(lines),
		"offset":  offset,
		"content": strings.Join(lines, "\n"),
	}

	// Try to detect file type
	result["type"] = detectFileType(absPath, content)

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
	case ".xml":
		return "xml"
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

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	dir := filepath.Dir(absPath)
	os.MkdirAll(dir, 0755)

	appendMode := false
	if a, ok := args["append"].(bool); ok {
		appendMode = a
	}

	var err2 error
	if appendMode {
		var f *os.File
		f, err2 = os.OpenFile(absPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err2 == nil {
			f.Write([]byte(content))
			f.Close()
		}
	} else {
		err2 = os.WriteFile(absPath, []byte(content), 0644)
	}

	if err2 != nil {
		return nil, fmt.Errorf("failed to write file: %w", err2)
	}

	// Get file info
	info, _ := os.Stat(absPath)

	return map[string]interface{}{
		"success": true,
		"path":    absPath,
		"bytes":   len(content),
		"lines":   strings.Count(content, "\n") + 1,
		"size":    info.Size(),
	}, nil
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
