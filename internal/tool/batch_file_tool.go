package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BatchFileOpsTool performs batch file operations (read, write, delete, search_replace)
type BatchFileOpsTool struct{}

// NewBatchFileOpsTool creates a new BatchFileOpsTool
func NewBatchFileOpsTool() *BatchFileOpsTool {
	return &BatchFileOpsTool{}
}

func (t *BatchFileOpsTool) Name() string {
	return "batch_file_ops"
}

func (t *BatchFileOpsTool) Description() string {
	return "Perform batch file operations: batch_read, batch_write, batch_delete, batch_search_replace"
}

func (t *BatchFileOpsTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"operation": map[string]interface{}{
				"type":        "string",
				"enum":        []interface{}{"batch_read", "batch_write", "batch_delete", "batch_search_replace"},
				"description": "The batch operation to perform",
			},
			"files": map[string]interface{}{
				"type":        "array",
				"description": "Array of file paths (for batch_read and batch_delete). Each entry can be a string path or an object with path, offset, and limit fields.",
				"items": map[string]interface{}{
					"oneOf": []interface{}{
						map[string]interface{}{"type": "string"},
						map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"path":   map[string]interface{}{"type": "string"},
								"offset": map[string]interface{}{"type": "number", "description": "Line number to start reading from (1-based)"},
								"limit":  map[string]interface{}{"type": "number", "description": "Maximum number of lines to read"},
							},
							"required": []interface{}{"path"},
						},
					},
				},
			},
			"operations": map[string]interface{}{
				"type":        "array",
				"description": "Array of operations (for batch_write and batch_search_replace). For batch_write: {path, content, create_dirs}. For batch_search_replace: {path, old_text, new_text}.",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path":    map[string]interface{}{"type": "string"},
						"content": map[string]interface{}{"type": "string", "description": "File content (for batch_write)"},
						"create_dirs": map[string]interface{}{
							"type":        "boolean",
							"description": "Create parent directories if they don't exist (for batch_write)",
							"default":     true,
						},
						"old_text": map[string]interface{}{"type": "string", "description": "Text to search for (for batch_search_replace)"},
						"new_text": map[string]interface{}{"type": "string", "description": "Replacement text (for batch_search_replace)"},
					},
					"required": []interface{}{"path"},
				},
			},
		},
		"required": []interface{}{"operation"},
	}
}

func (t *BatchFileOpsTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	operation, _ := params["operation"].(string)
	if operation == "" {
		return nil, fmt.Errorf("operation is required")
	}

	switch operation {
	case "batch_read":
		return t.batchRead(ctx, params)
	case "batch_write":
		return t.batchWrite(ctx, params)
	case "batch_delete":
		return t.batchDelete(ctx, params)
	case "batch_search_replace":
		return t.batchSearchReplace(ctx, params)
	default:
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}
}

// batchRead reads multiple files at once.
// Input: files - array of file paths (string or object with path/offset/limit)
// Output: map of filepath -> {content, total, read, offset, error}
func (t *BatchFileOpsTool) batchRead(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	filesRaw, ok := params["files"].([]interface{})
	if !ok || len(filesRaw) == 0 {
		return nil, fmt.Errorf("files array is required for batch_read")
	}

	results := make(map[string]interface{})
	for _, f := range filesRaw {
		var filePath string
		var offset, limit int

		switch v := f.(type) {
		case string:
			filePath = v
		case map[string]interface{}:
			filePath, _ = v["path"].(string)
			if o, ok := v["offset"].(float64); ok {
				offset = int(o)
			}
			if l, ok := v["limit"].(float64); ok {
				limit = int(l)
			}
		default:
			continue
		}

		if filePath == "" {
			continue
		}

		absPath, err := resolvePath(ctx, filePath)
		if err != nil {
			results[filePath] = map[string]interface{}{"error": fmt.Sprintf("failed to resolve path: %v", err)}
			continue
		}

		data, err := os.ReadFile(absPath)
		if err != nil {
			results[filePath] = map[string]interface{}{"error": fmt.Sprintf("failed to read file: %v", err)}
			continue
		}

		content := string(data)
		lines := strings.Split(content, "\n")
		totalLines := len(lines)

		// Apply offset (1-based to 0-based)
		startIdx := 0
		if offset > 0 {
			startIdx = offset - 1
			if startIdx < 0 {
				startIdx = 0
			}
		}

		readLines := lines
		if startIdx > 0 && startIdx < len(readLines) {
			readLines = readLines[startIdx:]
		} else if startIdx >= len(readLines) {
			readLines = nil
		}

		if limit > 0 && limit < len(readLines) {
			readLines = readLines[:limit]
		}

		results[filePath] = map[string]interface{}{
			"content": strings.Join(readLines, "\n"),
			"total":   totalLines,
			"read":    len(readLines),
			"offset":  offset,
		}
	}

	return map[string]interface{}{
		"operation": "batch_read",
		"count":     len(results),
		"results":   results,
	}, nil
}

// batchWrite writes/creates multiple files at once.
// Input: operations - array of {path, content, create_dirs}
// Output: map of filepath -> {success, bytes, lines, error}
func (t *BatchFileOpsTool) batchWrite(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	opsRaw, ok := params["operations"].([]interface{})
	if !ok || len(opsRaw) == 0 {
		return nil, fmt.Errorf("operations array is required for batch_write")
	}

	results := make(map[string]interface{})
	for _, op := range opsRaw {
		opMap, ok := op.(map[string]interface{})
		if !ok {
			continue
		}

		filePath, _ := opMap["path"].(string)
		if filePath == "" {
			continue
		}

		content, _ := opMap["content"].(string)

		createDirs := true
		if cd, ok := opMap["create_dirs"].(bool); ok {
			createDirs = cd
		}

		absPath, err := resolvePath(ctx, filePath)
		if err != nil {
			results[filePath] = map[string]interface{}{"success": false, "error": fmt.Sprintf("failed to resolve path: %v", err)}
			continue
		}

		if createDirs {
			dir := filepath.Dir(absPath)
			if err := os.MkdirAll(dir, 0755); err != nil {
				results[filePath] = map[string]interface{}{"success": false, "error": fmt.Sprintf("failed to create directories: %v", err)}
				continue
			}
		}

		if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
			results[filePath] = map[string]interface{}{"success": false, "error": fmt.Sprintf("failed to write file: %v", err)}
			continue
		}

		info, _ := os.Stat(absPath)
		result := map[string]interface{}{
			"success": true,
			"bytes":   len(content),
			"lines":   strings.Count(content, "\n") + 1,
		}
		if info != nil {
			result["size"] = info.Size()
		}

		// Post-write lint (non-blocking)
		if issues, _ := LintFile(absPath); len(issues) > 0 {
			result["lint_warning"] = fmt.Sprintf("Lint issues found:\n%s", strings.Join(issues, "\n"))
		}

		results[filePath] = result
	}

	return map[string]interface{}{
		"operation": "batch_write",
		"count":     len(results),
		"results":   results,
	}, nil
}

// batchDelete deletes multiple files at once.
// Input: files - array of file paths (strings)
// Output: map of filepath -> {success, error}
func (t *BatchFileOpsTool) batchDelete(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	filesRaw, ok := params["files"].([]interface{})
	if !ok || len(filesRaw) == 0 {
		return nil, fmt.Errorf("files array is required for batch_delete")
	}

	results := make(map[string]interface{})
	for _, f := range filesRaw {
		filePath, ok := f.(string)
		if !ok || filePath == "" {
			continue
		}

		absPath, err := resolvePath(ctx, filePath)
		if err != nil {
			results[filePath] = map[string]interface{}{"success": false, "error": fmt.Sprintf("failed to resolve path: %v", err)}
			continue
		}

		if err := os.Remove(absPath); err != nil {
			results[filePath] = map[string]interface{}{"success": false, "error": fmt.Sprintf("failed to delete file: %v", err)}
			continue
		}

		results[filePath] = map[string]interface{}{
			"success": true,
		}
	}

	return map[string]interface{}{
		"operation": "batch_delete",
		"count":     len(results),
		"results":   results,
	}, nil
}

// batchSearchReplace performs search and replace across multiple files.
// Input: operations - array of {path, old_text, new_text}
// Output: map of filepath -> {changes, error}
func (t *BatchFileOpsTool) batchSearchReplace(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	opsRaw, ok := params["operations"].([]interface{})
	if !ok || len(opsRaw) == 0 {
		return nil, fmt.Errorf("operations array is required for batch_search_replace")
	}

	results := make(map[string]interface{})
	for _, op := range opsRaw {
		opMap, ok := op.(map[string]interface{})
		if !ok {
			continue
		}

		filePath, _ := opMap["path"].(string)
		if filePath == "" {
			continue
		}

		oldText, _ := opMap["old_text"].(string)
		newText, _ := opMap["new_text"].(string)

		if oldText == "" {
			results[filePath] = map[string]interface{}{"changes": 0, "error": "old_text is required"}
			continue
		}

		absPath, err := resolvePath(ctx, filePath)
		if err != nil {
			results[filePath] = map[string]interface{}{"changes": 0, "error": fmt.Sprintf("failed to resolve path: %v", err)}
			continue
		}

		data, err := os.ReadFile(absPath)
		if err != nil {
			results[filePath] = map[string]interface{}{"changes": 0, "error": fmt.Sprintf("failed to read file: %v", err)}
			continue
		}

		content := string(data)

		if !strings.Contains(content, oldText) {
			results[filePath] = map[string]interface{}{"changes": 0, "error": "old_text not found in file"}
			continue
		}

		newContent := strings.Replace(content, oldText, newText, 1)
		changes := strings.Count(newContent, newText) - strings.Count(content, newText)
		// Since we only replace the first occurrence, changes is at most 1
		// A more accurate count: check if replacement actually changed content
		if newContent != content {
			changes = 1
		} else {
			changes = 0
		}

		if err := os.WriteFile(absPath, []byte(newContent), 0644); err != nil {
			results[filePath] = map[string]interface{}{"changes": 0, "error": fmt.Sprintf("failed to write file: %v", err)}
			continue
		}

		result := map[string]interface{}{
			"success": true,
			"changes": changes,
		}

		// Post-write lint (non-blocking)
		if issues, _ := LintFile(absPath); len(issues) > 0 {
			result["lint_warning"] = fmt.Sprintf("Lint issues found:\n%s", strings.Join(issues, "\n"))
		}

		results[filePath] = result
	}

	return map[string]interface{}{
		"operation": "batch_search_replace",
		"count":     len(results),
		"results":   results,
	}, nil
}
