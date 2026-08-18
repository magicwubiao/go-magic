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
	path := paramString(params, "path")
	operation := paramString(params, "operation")

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
	lineStart := paramInt(params, "line_start")
	lineEnd := paramInt(params, "line_end")
	if _, ok := params["line_end"]; !ok {
		lineEnd = lineStart
	}
	newContent := paramString(params, "new_content")
	oldContent := paramString(params, "old_content")

	// 检测文件主导行尾。匹配前统一规范化为 LF，写回时还原，避免 CRLF 文件
	// 用 LF 文本匹配失败，以及插入 LF 内容导致混合行尾。
	lineEnding := detectLineEnding(content)
	content = normalizeLineEndings(content)

	// If old_content provided, use text replacement (line-ending insensitive)
	if oldContent != "" {
		normOld := normalizeLineEndings(oldContent)
		if !strings.Contains(content, normOld) {
			return "", fmt.Errorf("old_content not found in file")
		}
		normNew := normalizeLineEndings(newContent)
		result := strings.Replace(content, normOld, normNew, 1)
		return convertLineEndings(result, lineEnding), nil
	}

	// Otherwise use line-based replacement. 行号为 1-based，替换 [line_start, line_end]（含）。
	lines := strings.Split(content, "\n")
	startIdx := lineStart - 1
	endIdx := lineEnd

	if startIdx < 0 || startIdx >= len(lines) {
		return "", fmt.Errorf("line_start %d out of range (file has %d lines)", lineStart, len(lines))
	}
	if endIdx < startIdx || endIdx >= len(lines) {
		return "", fmt.Errorf("line_end %d out of range", lineEnd)
	}

	// 使用新分配的切片，避免 append 写入 lines 底层数组造成别名污染。
	inserted := strings.Split(normalizeLineEndings(newContent), "\n")
	newLines := make([]string, 0, len(lines)+len(inserted))
	newLines = append(newLines, lines[:startIdx]...)
	newLines = append(newLines, inserted...)
	newLines = append(newLines, lines[endIdx:]...)

	return convertLineEndings(strings.Join(newLines, "\n"), lineEnding), nil
}

func (t *FileEditTool) insertContent(content string, params map[string]interface{}) (string, error) {
	insertIdx := paramInt(params, "line_start")
	newContent := paramString(params, "new_content")

	lineEnding := detectLineEnding(content)
	content = normalizeLineEndings(content)
	lines := strings.Split(content, "\n")

	if insertIdx < 0 || insertIdx > len(lines) {
		return "", fmt.Errorf("line_start %d out of range (file has %d lines)", insertIdx, len(lines))
	}

	// 在第 insertIdx 行之后插入，新内容行尾转换为文件行尾。
	// 使用新分配的切片，避免 append 写入 lines 底层数组造成别名污染。
	inserted := strings.Split(normalizeLineEndings(newContent), "\n")
	newLines := make([]string, 0, len(lines)+len(inserted))
	newLines = append(newLines, lines[:insertIdx]...)
	newLines = append(newLines, inserted...)
	newLines = append(newLines, lines[insertIdx:]...)

	return convertLineEndings(strings.Join(newLines, "\n"), lineEnding), nil
}

func (t *FileEditTool) deleteContent(content string, params map[string]interface{}) (string, error) {
	lineStart := paramInt(params, "line_start")
	lineEnd := lineStart
	if le := paramInt(params, "line_end"); le != 0 || params["line_end"] != nil {
		lineEnd = le
	}

	lineEnding := detectLineEnding(content)
	content = normalizeLineEndings(content)
	lines := strings.Split(content, "\n")
	startIdx := lineStart - 1
	endIdx := lineEnd

	if startIdx < 0 || startIdx >= len(lines) {
		return "", fmt.Errorf("line_start %d out of range (file has %d lines)", lineStart, len(lines))
	}
	if endIdx < startIdx || endIdx >= len(lines) {
		return "", fmt.Errorf("line_end %d out of range (file has %d lines)", lineEnd, len(lines))
	}

	// 删除 [line_start, line_end]（含）。使用新分配的切片避免别名污染。
	newLines := make([]string, 0, len(lines)-(endIdx-startIdx))
	newLines = append(newLines, lines[:startIdx]...)
	newLines = append(newLines, lines[endIdx:]...)

	return convertLineEndings(strings.Join(newLines, "\n"), lineEnding), nil
}
