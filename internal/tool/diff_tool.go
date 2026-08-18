package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DiffPatchTool provides diff/patch capabilities for coding mode.
// It supports showing diffs, applying patches, comparing files, and creating backups.
type DiffPatchTool struct{}

// NewDiffPatchTool creates a new DiffPatchTool instance.
func NewDiffPatchTool() *DiffPatchTool {
	return &DiffPatchTool{}
}

// Name returns the tool name.
func (t *DiffPatchTool) Name() string {
	return "diff_patch"
}

// Description returns the tool description.
func (t *DiffPatchTool) Description() string {
	return "Diff and patch operations for coding mode: show diffs, apply search-and-replace patches, compare files, and create backups"
}

// Schema returns the OpenAI function calling JSON Schema.
func (t *DiffPatchTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []interface{}{"show_diff", "apply_patch", "show_changes", "create_backup"},
				"description": "The diff/patch operation to perform",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "File path (for show_diff, apply_patch, create_backup)",
			},
			"new_content": map[string]interface{}{
				"type":        "string",
				"description": "Proposed new content (for show_diff)",
			},
			"patches": map[string]interface{}{
				"type":        "array",
				"description": "Array of search-and-replace operations (for apply_patch)",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"old_text": map[string]interface{}{
							"type":        "string",
							"description": "Text to search for",
						},
						"new_text": map[string]interface{}{
							"type":        "string",
							"description": "Replacement text",
						},
					},
					"required": []interface{}{"old_text", "new_text"},
				},
			},
			"file_a": map[string]interface{}{
				"type":        "string",
				"description": "Path to the first file (for show_changes)",
			},
			"file_b": map[string]interface{}{
				"type":        "string",
				"description": "Path to the second file (for show_changes)",
			},
		},
		"required": []interface{}{"action"},
	}
}

// Execute dispatches the action to the appropriate handler.
func (t *DiffPatchTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	action, _ := params["action"].(string)
	if action == "" {
		return nil, fmt.Errorf("action is required")
	}

	switch action {
	case "show_diff":
		return t.showDiff(params)
	case "apply_patch":
		return t.applyPatch(params)
	case "show_changes":
		return t.showChanges(params)
	case "create_backup":
		return t.createBackup(params)
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

// showDiff shows the diff between the current file content and a proposed new content.
func (t *DiffPatchTool) showDiff(params map[string]interface{}) (interface{}, error) {
	path, _ := params["path"].(string)
	newContent, _ := params["new_content"].(string)

	if path == "" {
		return nil, fmt.Errorf("path is required for show_diff")
	}
	if newContent == "" {
		return nil, fmt.Errorf("new_content is required for show_diff")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// 规范化行尾后再做行级 diff，避免 CRLF 文件的每行混入 \r 干扰比较。
	oldLines := strings.Split(normalizeLineEndings(string(data)), "\n")
	newLines := strings.Split(normalizeLineEndings(newContent), "\n")

	diff := generateUnifiedDiff(path, oldLines, newLines)

	return map[string]interface{}{
		"path":      path,
		"action":    "show_diff",
		"diff":      diff,
		"additions": countLinesByPrefix(diff, "+"),
		"removals":  countLinesByPrefix(diff, "-"),
	}, nil
}

// applyPatch applies a set of search-and-replace operations to a file atomically.
func (t *DiffPatchTool) applyPatch(params map[string]interface{}) (interface{}, error) {
	path, _ := params["path"].(string)

	if path == "" {
		return nil, fmt.Errorf("path is required for apply_patch")
	}

	patchesRaw, ok := params["patches"].([]interface{})
	if !ok || len(patchesRaw) == 0 {
		return nil, fmt.Errorf("patches is required and must be a non-empty array for apply_patch")
	}

	// Parse patches
	type patch struct {
		OldText string
		NewText string
	}
	patches := make([]patch, 0, len(patchesRaw))
	for i, p := range patchesRaw {
		pm, ok := p.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("patch[%d] must be an object", i)
		}
		oldText, _ := pm["old_text"].(string)
		newText, _ := pm["new_text"].(string)
		if oldText == "" {
			return nil, fmt.Errorf("patch[%d].old_text is required", i)
		}
		patches = append(patches, patch{OldText: oldText, NewText: newText})
	}

	// Read the original file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	content := string(data)
	lineEnding := detectLineEnding(content)
	content = normalizeLineEndings(content)

	// Validate all patches can be applied before making any changes (atomic check).
	// old_text 与文件内容均规范化为 LF 后匹配，兼容 CRLF 文件。
	for i, p := range patches {
		normOld := normalizeLineEndings(p.OldText)
		if !strings.Contains(content, normOld) {
			return nil, fmt.Errorf("patch[%d]: old_text not found in file", i)
		}
		// Check for ambiguous matches (old_text appears more than once)
		count := strings.Count(content, normOld)
		if count > 1 {
			return nil, fmt.Errorf("patch[%d]: old_text matches %d occurrences in file (ambiguous), please provide more context to uniquely identify the target", i, count)
		}
	}

	// Apply all patches
	originalLines := strings.Split(content, "\n")
	totalLinesChanged := 0
	appliedCount := 0

	for _, p := range patches {
		oldLines := strings.Split(normalizeLineEndings(p.OldText), "\n")
		newLines := strings.Split(normalizeLineEndings(p.NewText), "\n")

		idx := findExactMatch(originalLines, oldLines)
		if idx < 0 {
			// This should not happen since we validated above, but guard against it
			return nil, fmt.Errorf("failed to find exact match for patch in file")
		}

		// Replace lines
		replaced := append(originalLines[:idx], newLines...)
		replaced = append(replaced, originalLines[idx+len(oldLines):]...)
		originalLines = replaced

		// Track lines changed: count removed + added
		totalLinesChanged += len(oldLines) + len(newLines)
		appliedCount++
	}

	// Write the result, preserving the file's original line ending style.
	newContent := convertLineEndings(strings.Join(originalLines, "\n"), lineEnding)
	if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	return map[string]interface{}{
		"path":             path,
		"action":           "apply_patch",
		"patches_applied":  appliedCount,
		"total_patches":    len(patchesRaw),
		"lines_changed":    totalLinesChanged,
		"final_line_count": len(originalLines),
	}, nil
}

// showChanges compares two files and shows differences.
func (t *DiffPatchTool) showChanges(params map[string]interface{}) (interface{}, error) {
	fileA, _ := params["file_a"].(string)
	fileB, _ := params["file_b"].(string)

	if fileA == "" || fileB == "" {
		return nil, fmt.Errorf("file_a and file_b are required for show_changes")
	}

	dataA, err := os.ReadFile(fileA)
	if err != nil {
		return nil, fmt.Errorf("failed to read file_a: %w", err)
	}
	dataB, err := os.ReadFile(fileB)
	if err != nil {
		return nil, fmt.Errorf("failed to read file_b: %w", err)
	}

	// 规范化行尾后再比较，CRLF 与 LF 内容视为等价。
	linesA := strings.Split(normalizeLineEndings(string(dataA)), "\n")
	linesB := strings.Split(normalizeLineEndings(string(dataB)), "\n")

	diff := generateUnifiedDiff(fmt.Sprintf("%s vs %s", filepath.Base(fileA), filepath.Base(fileB)), linesA, linesB)

	return map[string]interface{}{
		"file_a":    fileA,
		"file_b":    fileB,
		"action":    "show_changes",
		"diff":      diff,
		"additions": countLinesByPrefix(diff, "+"),
		"removals":  countLinesByPrefix(diff, "-"),
	}, nil
}

// createBackup creates a backup of a file before modification.
func (t *DiffPatchTool) createBackup(params map[string]interface{}) (interface{}, error) {
	path, _ := params["path"].(string)

	if path == "" {
		return nil, fmt.Errorf("path is required for create_backup")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Generate backup path: <original>.bak.<timestamp>
	ext := filepath.Ext(path)
	base := path[:len(path)-len(ext)]
	timestamp := time.Now().Format("20060102-150405")
	backupPath := fmt.Sprintf("%s.bak%s.%s", base, ext, timestamp)

	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		return nil, fmt.Errorf("failed to create backup: %w", err)
	}

	info, _ := os.Stat(path)

	return map[string]interface{}{
		"action":        "create_backup",
		"original_path": path,
		"backup_path":   backupPath,
		"size_bytes":    len(data),
		"original_mod":  info.ModTime().Format(time.RFC3339),
	}, nil
}

// ---------------------------------------------------------------------------
// Unified diff generation (LCS-based)
// ---------------------------------------------------------------------------

// diffOp represents a single edit operation.
type diffOp struct {
	kind int // 0 = equal, 1 = insert, -1 = delete
	line string
}

// lcs computes the longest common subsequence table for two string slices.
func lcs(a, b []string) [][]int {
	m, n := len(a), len(b)
	// dp[i][j] = length of LCS of a[:i] and b[:j]
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	return dp
}

// computeDiffOps produces a sequence of diff operations from the LCS table.
func computeDiffOps(a, b []string) []diffOp {
	dp := lcs(a, b)
	var ops []diffOp
	i, j := len(a), len(b)
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && a[i-1] == b[j-1] {
			ops = append(ops, diffOp{0, a[i-1]})
			i--
			j--
		} else if j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]) {
			ops = append(ops, diffOp{1, b[j-1]})
			j--
		} else {
			ops = append(ops, diffOp{-1, a[i-1]})
			i--
		}
	}
	// Reverse to get forward order
	for l, r := 0, len(ops)-1; l < r; l, r = l+1, r-1 {
		ops[l], ops[r] = ops[r], ops[l]
	}
	return ops
}

// generateUnifiedDiff creates a unified diff string from two line slices.
func generateUnifiedDiff(label string, oldLines, newLines []string) string {
	ops := computeDiffOps(oldLines, newLines)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("--- %s (original)\n", label))
	sb.WriteString(fmt.Sprintf("+++ %s (modified)\n", label))

	// Group operations into hunks with context lines.
	const contextSize = 3

	type hunk struct {
		oldStart, oldCount int
		newStart, newCount int
		lines              []string // prefixed lines (+, -, or " ")
	}

	// Identify change positions (non-equal ops)
	changePositions := make([]int, 0)
	for i, op := range ops {
		if op.kind != 0 {
			changePositions = append(changePositions, i)
		}
	}

	if len(changePositions) == 0 {
		sb.WriteString("(no differences)\n")
		return sb.String()
	}

	// Merge nearby changes into hunks
	var hunks []hunk
	hunkStart := -1
	hunkEnd := -1

	addHunk := func() {
		// Determine the range of ops to include (with context)
		start := hunkStart - contextSize
		if start < 0 {
			start = 0
		}
		end := hunkEnd + contextSize + 1
		if end > len(ops) {
			end = len(ops)
		}

		var lines []string
		oldStart := -1
		newStart := -1
		oldCount := 0
		newCount := 0

		for i := start; i < end; i++ {
			op := ops[i]
			switch op.kind {
			case 0:
				lines = append(lines, " "+op.line)
				oldCount++
				newCount++
				if oldStart < 0 {
					oldStart = i + 1 // 1-based line number in old
				}
				if newStart < 0 {
					newStart = i + 1
				}
			case -1:
				lines = append(lines, "-"+op.line)
				oldCount++
				if oldStart < 0 {
					// Find the old line number by counting equal/delete ops before this
					oldStart = countOldLineNumber(ops, i)
				}
			case 1:
				lines = append(lines, "+"+op.line)
				newCount++
				if newStart < 0 {
					newStart = countNewLineNumber(ops, i)
				}
			}
		}

		if oldStart < 0 {
			oldStart = 1
		}
		if newStart < 0 {
			newStart = 1
		}

		hunks = append(hunks, hunk{
			oldStart: oldStart,
			oldCount: oldCount,
			newStart: newStart,
			newCount: newCount,
			lines:    lines,
		})
	}

	for _, pos := range changePositions {
		if hunkStart < 0 {
			hunkStart = pos
			hunkEnd = pos
		} else if pos-hunkEnd <= contextSize*2 {
			hunkEnd = pos
		} else {
			addHunk()
			hunkStart = pos
			hunkEnd = pos
		}
	}
	if hunkStart >= 0 {
		addHunk()
	}

	for _, h := range hunks {
		sb.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", h.oldStart, h.oldCount, h.newStart, h.newCount))
		for _, line := range h.lines {
			sb.WriteString(line)
			sb.WriteByte('\n')
		}
	}

	return sb.String()
}

// countOldLineNumber computes the 1-based line number in the old file for a given op index.
func countOldLineNumber(ops []diffOp, idx int) int {
	lineNum := 0
	for i := 0; i <= idx; i++ {
		if ops[i].kind == 0 || ops[i].kind == -1 {
			lineNum++
		}
	}
	return lineNum
}

// countNewLineNumber computes the 1-based line number in the new file for a given op index.
func countNewLineNumber(ops []diffOp, idx int) int {
	lineNum := 0
	for i := 0; i <= idx; i++ {
		if ops[i].kind == 0 || ops[i].kind == 1 {
			lineNum++
		}
	}
	return lineNum
}

// countLinesByPrefix counts lines in the diff output that start with the given prefix.
func countLinesByPrefix(diff, prefix string) int {
	count := 0
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, prefix) {
			count++
		}
	}
	return count
}

// findExactMatch searches for an exact contiguous match of target lines within source lines.
// Returns the starting index, or -1 if not found.
func findExactMatch(source, target []string) int {
	if len(target) == 0 {
		return 0
	}
	if len(target) > len(source) {
		return -1
	}
	for i := 0; i <= len(source)-len(target); i++ {
		match := true
		for j := 0; j < len(target); j++ {
			if source[i+j] != target[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// Ensure DiffPatchTool satisfies the Tool interface at compile time.
var _ Tool = (*DiffPatchTool)(nil)
