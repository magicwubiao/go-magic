package tool

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// FileEditTool handles file edit operations
type FileEditTool struct{}

func NewFileEditTool() *FileEditTool {
	return &FileEditTool{}
}

func (t *FileEditTool) Name() string {
	return "file_edit"
}

func (t *FileEditTool) Description() string {
	return "Edit content in existing files (insert, replace, delete lines)"
}

func (t *FileEditTool) Parameters() map[string]interface{} {
	return t.Schema()
}

func (t *FileEditTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"operation": map[string]interface{}{
				"type": "string",
				"enum": []interface{}{"insert", "replace", "delete"},
			},
			"path": map[string]interface{}{
				"type": "string",
			},
			"line_start": map[string]interface{}{
				"type": "number",
			},
			"line_end": map[string]interface{}{
				"type": "number",
			},
			"old_content": map[string]interface{}{
				"type": "string",
			},
			"new_content": map[string]interface{}{
				"type": "string",
			},
		},
		"required": []interface{}{"operation", "path"},
	}
}

func (t *FileEditTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	path, _ := params["path"].(string)
	operation, _ := params["operation"].(string)

	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	absPath, err := resolvePath(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve path: %w", err)
	}

	// Read existing file
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	content := string(data)

	var newContent string
	switch operation {
	case "insert":
		newContent, err = t.insertContent(content, params)
	case "replace":
		newContent, err = t.replaceContent(content, params)
	case "delete":
		newContent, err = t.deleteContent(content, params)
	default:
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}

	if err != nil {
		return nil, err
	}

	security := FileSecurityFromContext(ctx)
	if err := os.WriteFile(absPath, []byte(newContent), security.DefaultFileMode); err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	result := map[string]interface{}{
		"path":          absPath,
		"operation":     operation,
		"bytes_written": len(newContent),
	}

	// Post-write lint (non-blocking)
	if issues, _ := LintFile(path); len(issues) > 0 {
		result["lint_warning"] = fmt.Sprintf("⚠ Lint issues found:\n%s", strings.Join(issues, "\n"))
	}

	return result, nil
}

func (t *FileEditTool) replaceContent(content string, params map[string]interface{}) (string, error) {
	lineStartF, _ := params["line_start"].(float64)
	lineEndF := lineStartF
	if le, ok := params["line_end"].(float64); ok {
		lineEndF = le
	}
	lineStart := int(lineStartF)
	lineEnd := int(lineEndF)
	newContent, _ := params["new_content"].(string)
	oldContent, _ := params["old_content"].(string)

	// If old_content provided, use text replacement
	if oldContent != "" {
		if !strings.Contains(content, oldContent) {
			return "", fmt.Errorf("old_content not found in file")
		}
		return strings.Replace(content, oldContent, newContent, 1), nil
	}

	// Otherwise use line-based replacement
	lines := strings.Split(content, "\n")
	startIdx := lineStart - 1
	endIdx := lineEnd

	if startIdx < 0 || startIdx >= len(lines) {
		return "", fmt.Errorf("line_start %d out of range (file has %d lines)", lineStart, len(lines))
	}
	if endIdx < startIdx || endIdx >= len(lines) {
		return "", fmt.Errorf("line_end %d out of range", lineEnd)
	}

	// Replace lines
	newLines := append(lines[:startIdx], strings.Split(newContent, "\n")...)
	newLines = append(newLines, lines[endIdx+1:]...)

	return strings.Join(newLines, "\n"), nil
}

func (t *FileEditTool) insertContent(content string, params map[string]interface{}) (string, error) {
	lineStartF, _ := params["line_start"].(float64)
	insertIdx := int(lineStartF)
	newContent, _ := params["new_content"].(string)

	lines := strings.Split(content, "\n")

	if insertIdx < 0 || insertIdx > len(lines) {
		return "", fmt.Errorf("line_start %d out of range (file has %d lines)", insertIdx, len(lines))
	}

	// Insert at specified position
	newLines := append(lines[:insertIdx], strings.Split(newContent, "\n")...)
	newLines = append(newLines, lines[insertIdx:]...)

	return strings.Join(newLines, "\n"), nil
}

func (t *FileEditTool) deleteContent(content string, params map[string]interface{}) (string, error) {
	lineStartF, _ := params["line_start"].(float64)
	lineStart := int(lineStartF)
	lineEnd := lineStart
	if le, ok := params["line_end"].(float64); ok {
		lineEnd = int(le)
	}

	lines := strings.Split(content, "\n")
	startIdx := lineStart - 1
	endIdx := lineEnd

	if startIdx < 0 || startIdx >= len(lines) {
		return "", fmt.Errorf("line_start %d out of range (file has %d lines)", lineStart, len(lines))
	}
	if endIdx < startIdx || endIdx >= len(lines) {
		return "", fmt.Errorf("line_end %d out of range (file has %d lines)", lineEnd, len(lines))
	}

	// Delete lines
	newLines := append(lines[:startIdx], lines[endIdx+1:]...)

	return strings.Join(newLines, "\n"), nil
}
