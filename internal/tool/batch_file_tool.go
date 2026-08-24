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
								"binary_ok": map[string]interface{}{
									"type":        "boolean",
									"description": "If true, allow reading binary files (content omitted). Default false.",
									"default":     false,
								},
								"max_size_kb": map[string]interface{}{
									"type":        "number",
									"description": "Maximum file size to read (in KB). 0 or omitted uses security default.",
								},
							},
							"required": []interface{}{"path"},
						},
					},
				},
			},
			"operations": map[string]interface{}{
				"type":        "array",
				"description": "Array of operations (for batch_write and batch_search_replace).",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path":             map[string]interface{}{"type": "string"},
						"content":          map[string]interface{}{"type": "string", "description": "File content (for batch_write)"},
						"create_dirs":      map[string]interface{}{"type": "boolean", "description": "Create parent directories if they don't exist (for batch_write, default true)", "default": true},
						"backup":           map[string]interface{}{"type": "boolean", "description": "Create a .bak copy before overwriting (for batch_write, default false)", "default": false},
						"atomic":           map[string]interface{}{"type": "boolean", "description": "Write atomically via tempfile+rename (for batch_write, default true)", "default": true},
						"old_text":         map[string]interface{}{"type": "string", "description": "Text to search for (for batch_search_replace)"},
						"new_text":         map[string]interface{}{"type": "string", "description": "Replacement text (for batch_search_replace)"},
						"replace_all":      map[string]interface{}{"type": "boolean", "description": "Replace all occurrences. If false, replace only the first occurrence (default false).", "default": false},
						"case_sensitive":   map[string]interface{}{"type": "boolean", "description": "Match with case sensitivity (default true).", "default": true},
						"max_replacements": map[string]interface{}{"type": "number", "description": "Maximum number of replacements to perform (<0 means unlimited when replace_all is true)."},
						"dry_run":          map[string]interface{}{"type": "boolean", "description": "Report matches/positions without writing the file (default false).", "default": false},
						"require_unique":   map[string]interface{}{"type": "boolean", "description": "If true, fail when old_text matches more than once but replace_all is false (prevents ambiguous edits).", "default": false},
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

// ---------------------------------------------------------------------------
// batchRead — with binary detection, size limits, and classification metadata
// ---------------------------------------------------------------------------

func (t *BatchFileOpsTool) batchRead(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	filesRaw, ok := params["files"].([]interface{})
	if !ok || len(filesRaw) == 0 {
		return nil, fmt.Errorf("files array is required for batch_read")
	}

	security := FileSecurityFromContext(ctx)
	defaultMaxKB := security.MaxFileSizeKB
	if defaultMaxKB <= 0 {
		defaultMaxKB = 10240
	}

	results := make(map[string]interface{})
	for _, f := range filesRaw {
		var (
			filePath      string
			offset, limit int
			binaryOK      bool
			maxSizeKB     = defaultMaxKB
		)

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
			if b, ok := v["binary_ok"].(bool); ok {
				binaryOK = b
			}
			if kb, ok := v["max_size_kb"].(float64); ok && kb > 0 {
				maxSizeKB = int(kb)
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

		// Size check before reading to avoid loading huge files.
		info, err := os.Stat(absPath)
		if err != nil {
			results[filePath] = map[string]interface{}{"error": fmt.Sprintf("failed to stat file: %v", err)}
			continue
		}
		if info.Size() > int64(maxSizeKB)*1024 {
			results[filePath] = map[string]interface{}{
				"error":    fmt.Sprintf("file exceeds max size (%d KB > %d KB limit)", info.Size()/1024, maxSizeKB),
				"size":     info.Size(),
				"max_size": int64(maxSizeKB) * 1024,
			}
			continue
		}

		data, err := os.ReadFile(absPath)
		if err != nil {
			results[filePath] = map[string]interface{}{"error": fmt.Sprintf("failed to read file: %v", err)}
			continue
		}

		// Classify: detect binary vs code/text
		isCode, isBinary := classifyFile(absPath, data)
		if isBinary && !binaryOK {
			results[filePath] = map[string]interface{}{
				"error":     "binary file detected; set binary_ok=true to read (content will be omitted)",
				"is_code":   isCode,
				"is_binary": true,
				"size":      len(data),
			}
			continue
		}

		result := map[string]interface{}{
			"is_code":     isCode,
			"is_binary":   isBinary,
			"size":        len(data),
			"line_ending": detectLineEnding(string(data)),
		}

		if !isBinary {
			content := normalizeLineEndings(string(data))
			lines := strings.Split(content, "\n")
			totalLines := len(lines)

			startIdx := 0
			if offset > 0 {
				startIdx = offset - 1
				if startIdx < 0 {
					startIdx = 0
				}
			}

			readLines := lines
			if startIdx >= len(readLines) {
				readLines = nil
			} else if startIdx > 0 {
				readLines = readLines[startIdx:]
			}
			if limit > 0 && limit < len(readLines) {
				readLines = readLines[:limit]
			}

			result["content"] = strings.Join(readLines, "\n")
			result["total"] = totalLines
			result["read"] = len(readLines)
			result["offset"] = offset
		} else {
			result["content"] = ""
			result["note"] = "binary content omitted"
		}

		results[filePath] = result
	}

	return map[string]interface{}{
		"operation": "batch_read",
		"count":     len(results),
		"results":   results,
	}, nil
}

// ---------------------------------------------------------------------------
// batchWrite — atomic writes, optional backups, content validation
// ---------------------------------------------------------------------------

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

		createDirs := paramBool(opMap, "create_dirs", true)
		doBackup := paramBool(opMap, "backup", false)
		doAtomic := paramBool(opMap, "atomic", true)

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

		// Detect target line-ending style: preserve existing file's style, else LF.
		targetLE := LineEndingLF
		if existing, err := os.ReadFile(absPath); err == nil && len(existing) > 0 {
			targetLE = detectLineEnding(string(existing))
			// Optional backup
			if doBackup {
				bakPath := absPath + ".bak"
				if err := os.WriteFile(bakPath, existing, 0644); err != nil {
					results[filePath] = map[string]interface{}{"success": false, "error": fmt.Sprintf("failed to create backup: %v", err)}
					continue
				}
			}
		}

		// Sanity check on incoming content: refuse clearly binary data unless path is code
		codeExt := isCodeFile(absPath)
		if !codeExt && isBinaryContent([]byte(content)) {
			results[filePath] = map[string]interface{}{
				"success": false,
				"error":   "refusing to write binary-looking content to non-code extension; use a known code extension or double-check the payload",
			}
			continue
		}

		// Normalize user's input to LF first; then emit target line-ending.
		// This guarantees the written file has a consistent ending and the
		// line-count below matches the on-disk content.
		toWrite := convertLineEndings(content, targetLE)

		if doAtomic {
			if err := atomicWriteFile(absPath, []byte(toWrite), 0644); err != nil {
				results[filePath] = map[string]interface{}{"success": false, "error": fmt.Sprintf("failed to write file: %v", err)}
				continue
			}
		} else {
			if err := os.WriteFile(absPath, []byte(toWrite), 0644); err != nil {
				results[filePath] = map[string]interface{}{"success": false, "error": fmt.Sprintf("failed to write file: %v", err)}
				continue
			}
		}

		info, _ := os.Stat(absPath)
		result := map[string]interface{}{
			"success":     true,
			"bytes":       len(toWrite),
			"lines":       countContentLines(normalizeLineEndings(toWrite)),
			"line_ending": targetLE,
			"is_code":     codeExt,
		}
		if info != nil {
			result["size"] = info.Size()
		}
		if doBackup {
			result["backup"] = filepath.Base(absPath) + ".bak"
		}

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

// atomicWriteFile writes data to path atomically via temp file + rename.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

// ---------------------------------------------------------------------------
// batchDelete — unchanged here
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// batchSearchReplace — precise matching, full counting, match positions,
// case-sensitivity, replace_all, dry-run, unique-match enforcement,
// ambiguous-match warning, proper line-ending preservation.
// ---------------------------------------------------------------------------

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

		replaceAll := paramBool(opMap, "replace_all", false)
		caseSensitive := paramBool(opMap, "case_sensitive", true)
		maxRepl := paramInt(opMap, "max_replacements")
		dryRun := paramBool(opMap, "dry_run", false)
		requireUnique := paramBool(opMap, "require_unique", false)

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

		// Refuse to operate on binary content.
		if isCodeFile, isBin := classifyFile(absPath, data); isBin {
			results[filePath] = map[string]interface{}{
				"changes":   0,
				"error":     "binary file detected; search_replace is only supported for text/code files",
				"is_code":   isCodeFile,
				"is_binary": true,
			}
			continue
		}

		lineEnding := detectLineEnding(string(data))
		content := normalizeLineEndings(string(data))
		normOld := normalizeLineEndings(oldText)
		normNew := normalizeLineEndings(newText)

		// Find all matches (positions) for precise reporting and sanity checks.
		matches := findAllMatches(content, normOld, caseSensitive)

		if len(matches) == 0 {
			results[filePath] = map[string]interface{}{
				"changes":           0,
				"error":             "old_text not found in file",
				"case_sensitive":    caseSensitive,
				"occurrences_found": 0,
			}
			continue
		}

		// Enforce uniqueness when requested.
		if requireUnique && len(matches) != 1 {
			results[filePath] = map[string]interface{}{
				"changes":           0,
				"error":             fmt.Sprintf("require_unique=true but found %d matches; use replace_all or widen old_text to be unique", len(matches)),
				"occurrences_found": len(matches),
				"matches":           matches,
			}
			continue
		}

		// Determine how many replacements to actually perform.
		maxReplace := 0
		if replaceAll {
			if maxRepl <= 0 {
				maxReplace = len(matches)
			} else {
				maxReplace = maxRepl
				if maxReplace > len(matches) {
					maxReplace = len(matches)
				}
			}
		} else {
			// Single-replace mode: honor max_replacements=1 only, else warn.
			if maxRepl > 1 {
				maxRepl = 1
			}
			maxReplace = 1
		}

		newContent, actualChanges := replaceAllExact(content, normOld, normNew, caseSensitive, maxReplace)

		result := map[string]interface{}{
			"success":           actualChanges > 0,
			"changes":           actualChanges,
			"occurrences_found": len(matches),
			"matches":           matches,
			"case_sensitive":    caseSensitive,
			"replace_all":       replaceAll,
			"line_ending":       lineEnding,
			"is_code":           isCodeFile(absPath),
		}

		if len(matches) > 1 && !replaceAll {
			result["warning"] = fmt.Sprintf("found %d occurrences but replace_all=false; only the first occurrence (L%d) was replaced", len(matches), matches[0].LineStart)
		}

		if actualChanges == 0 {
			results[filePath] = result
			continue
		}

		if dryRun {
			result["dry_run"] = true
			result["note"] = "dry_run=true: file was not modified"
			results[filePath] = result
			continue
		}

		// Commit: preserve original line-ending style; write atomically.
		if err := atomicWriteFile(absPath, []byte(convertLineEndings(newContent, lineEnding)), 0644); err != nil {
			results[filePath] = map[string]interface{}{"changes": 0, "error": fmt.Sprintf("failed to write file: %v", err)}
			continue
		}

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
