package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileEditTool 文件精确编辑工具
type FileEditTool struct {
	BaseTool
}

// NewFileEditTool 创建文件编辑工具
func NewFileEditTool() *FileEditTool {
	return &FileEditTool{
		BaseTool: *NewBaseTool(
			"file_edit",
			"Precisely edit specific lines in a file. Use for targeted modifications, insertions, or deletions.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file to edit",
					},
					"operation": map[string]interface{}{
						"type":        "string",
						"description": "Operation type: replace, insert, delete, append",
						"enum":        []string{"replace", "insert", "delete", "append"},
					},
					"line_start": map[string]interface{}{
						"type":        "number",
						"description": "Starting line number (1-based). For replace, this is the line to replace. For insert, inserts after this line.",
					},
					"line_end": map[string]interface{}{
						"type":        "number",
						"description": "Ending line number (inclusive). Only for replace and delete operations. Defaults to line_start.",
					},
					"new_content": map[string]interface{}{
						"type":        "string",
						"description": "New content to replace or insert",
					},
					"old_content": map[string]interface{}{
						"type":        "string",
						"description": "Content to find and replace (alternative to line numbers)",
					},
				},
				"required": []string{"path", "operation"},
			},
		),
	}
}

// ValidateParams 验证参数
func (t *FileEditTool) ValidateParams(params map[string]interface{}) error {
	path, _ := params["path"].(string)
	if path == "" {
		return fmt.Errorf("path is required")
	}
	return nil
}

// Execute 执行文件编辑
func (t *FileEditTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	path, _ := params["path"].(string)
	operation, _ := params["operation"].(string)

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	// 读取原文件
	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var newContent string
	switch operation {
	case "replace":
		newContent, err = t.replaceContent(string(content), params)
	case "insert":
		newContent, err = t.insertContent(string(content), params)
	case "delete":
		newContent, err = t.deleteContent(string(content), params)
	case "append":
		newContent, err = t.appendContent(string(content), params)
	default:
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}

	if err != nil {
		return nil, err
	}

	// 写入新内容
	if err := os.WriteFile(absPath, []byte(newContent), 0644); err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	return map[string]interface{}{
		"success":       true,
		"path":          absPath,
		"operation":     operation,
		"bytes_written": len(newContent),
	}, nil
}

func (t *FileEditTool) replaceContent(content string, params map[string]interface{}) (string, error) {
	lineStart, _ := params["line_start"].(float64)
	lineEnd := lineStart
	if le, ok := params["line_end"].(float64); ok {
		lineEnd = le
	}
	newContent, _ := params["new_content"].(string)
	oldContent, _ := params["old_content"].(string)

	// 如果提供了 old_content，使用文本替换
	if oldContent != "" {
		if !strings.Contains(content, oldContent) {
			return "", fmt.Errorf("old_content not found in file")
		}
		return strings.Replace(content, oldContent, newContent, 1), nil
	}

	// 否则使用行号替换
	lines := strings.Split(content, "\n")
	startIdx := int(lineStart) - 1
	endIdx := int(lineEnd)

	if startIdx < 0 || startIdx >= len(lines) {
		return "", fmt.Errorf("line_start %d out of range (file has %d lines)", lineStart, len(lines))
	}
	if endIdx < startIdx || endIdx >= len(lines) {
		return "", fmt.Errorf("line_end %d out of range", lineEnd)
	}

	// 替换行
	newLines := append(lines[:startIdx], strings.Split(newContent, "\n")...)
	newLines = append(newLines, lines[endIdx+1:]...)

	return strings.Join(newLines, "\n"), nil
}

func (t *FileEditTool) insertContent(content string, params map[string]interface{}) (string, error) {
	lineStart, _ := params["line_start"].(float64)
	newContent, _ := params["new_content"].(string)

	lines := strings.Split(content, "\n")
	insertIdx := int(lineStart)

	if insertIdx < 0 || insertIdx > len(lines) {
		return "", fmt.Errorf("line_start %d out of range", lineStart)
	}

	// 在指定位置插入
	newLines := append(lines[:insertIdx], strings.Split(newContent, "\n")...)
	newLines = append(newLines, lines[insertIdx:]...)

	return strings.Join(newLines, "\n"), nil
}

func (t *FileEditTool) deleteContent(content string, params map[string]interface{}) (string, error) {
	lineStart, _ := params["line_start"].(float64)
	lineEnd := lineStart
	if le, ok := params["line_end"].(float64); ok {
		lineEnd = le
	}

	lines := strings.Split(content, "\n")
	startIdx := int(lineStart) - 1
	endIdx := int(lineEnd)

	if startIdx < 0 || startIdx >= len(lines) {
		return "", fmt.Errorf("line_start %d out of range", lineStart)
	}
	if endIdx < startIdx || endIdx >= len(lines) {
		return "", fmt.Errorf("line_end %d out of range", lineEnd)
	}

	// 删除行
	newLines := append(lines[:startIdx], lines[endIdx+1:]...)

	return strings.Join(newLines, "\n"), nil
}

func (t *FileEditTool) appendContent(content string, params map[string]interface{}) (string, error) {
	newContent, _ := params["new_content"].(string)

	if !strings.HasSuffix(content, "\n") && content != "" {
		content += "\n"
	}

	return content + newContent, nil
}
