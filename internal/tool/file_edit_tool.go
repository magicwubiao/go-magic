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
	return "PRECISELY edit existing files using EXACT text matching (old_content -> new_content). Multi-level tolerant matching: exact text → leading whitespace normalization → trailing whitespace tolerance. Always reports match location + surrounding context for verification. Prefer this over shell commands (sed/grep/python) for deterministic edits."
}

func (t *FileEditTool) Parameters() map[string]interface{} {
	return t.Schema()
}

func (t *FileEditTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"operation": map[string]interface{}{
				"type":        "string",
				"enum":        []interface{}{"insert", "replace", "delete"},
				"description": "'replace' (RECOMMENDED: old_content+new_content with tolerant multi-level matching), 'insert' (insert at line_start), 'delete' (delete lines line_start..line_end)",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Absolute or relative path to the target file",
			},
			"line_start": map[string]interface{}{
				"type":        "number",
				"description": "1-based line number. Only use when old_content truly cannot match. For replace/delete: inclusive start line. For insert: insert after this line.",
			},
			"line_end": map[string]interface{}{
				"type":        "number",
				"description": "1-based ending line (inclusive) for line-based replace/delete. Optional, defaults to line_start.",
			},
			"old_content": map[string]interface{}{
				"type":        "string",
				"description": "STRONGLY RECOMMENDED. The existing text in the file to replace. The tool performs tolerant matching: 1) exact match, 2) tab<->space leading whitespace tolerant, 3) trailing whitespace tolerant, 4) blank-edge tolerant. Include 3+ lines for uniqueness. If multiple matches found, the tool will report all locations and refuse to modify.",
			},
			"new_content": map[string]interface{}{
				"type":        "string",
				"description": "Replacement / insertion content. Multi-line accepted with real newlines. The actual original file indentation/line endings will be preserved when tolerant matching succeeds.",
			},
		},
		"required": []interface{}{"operation", "path"},
	}
}

// matchResult captures a found occurrence of old_content in the file
type matchResult struct {
	byteOffset  int    // byte offset in normalized content
	byteLength  int    // byte length of matched text in normalized content
	lineStart   int    // 1-based first line number
	lineEnd     int    // 1-based last line number (inclusive)
	matchedText string // the actual matched text from the file (preserves original indentation)
	tier        string // which matching tier found this
}

// Execute runs the edit operation and returns a detailed result including match diagnostics
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

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	content := string(data)
	totalLines := strings.Count(normalizeLineEndings(content), "\n") + 1
	if content == "" {
		totalLines = 0
	}

	var (
		newContent string
		matchInfo  interface{}
	)
	switch operation {
	case "insert":
		newContent, err = t.insertContent(content, params)
		if err == nil {
			ls := paramInt(params, "line_start")
			matchInfo = map[string]interface{}{
				"mode":           "line_based",
				"inserted_after": ls,
				"total_lines":    totalLines,
			}
		}
	case "replace":
		newContent, matchInfo, err = t.replaceContentDetailed(content, params, totalLines)
	case "delete":
		newContent, err = t.deleteContent(content, params)
		if err == nil {
			ls := paramInt(params, "line_start")
			le := paramInt(params, "line_end")
			if le == 0 {
				le = ls
			}
			matchInfo = map[string]interface{}{
				"mode":          "line_based",
				"deleted_lines": map[string]interface{}{"from": ls, "to": le},
				"total_lines":   totalLines,
			}
		}
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
		"path":           absPath,
		"operation":      operation,
		"bytes_written":  len(newContent),
		"original_bytes": len(content),
		"total_lines":    totalLines,
	}
	if matchInfo != nil {
		result["match"] = matchInfo
	}

	if issues, _ := LintFile(path); len(issues) > 0 {
		result["lint_warning"] = fmt.Sprintf("Lint issues found:\n%s", strings.Join(issues, "\n"))
	}

	return result, nil
}

// ---------------------------------------------------------------------------
// replaceContent with multi-level tolerant matching and diagnostics
// ---------------------------------------------------------------------------

func (t *FileEditTool) replaceContent(content string, params map[string]interface{}) (string, error) {
	out, _, err := t.replaceContentDetailed(content, params, -1)
	return out, err
}

// replaceContentDetailed performs the replace and returns both new content and detailed match info
func (t *FileEditTool) replaceContentDetailed(content string, params map[string]interface{}, totalLines int) (string, interface{}, error) {
	lineStart := paramInt(params, "line_start")
	lineEnd := paramInt(params, "line_end")
	if _, ok := params["line_end"]; !ok && lineStart > 0 {
		lineEnd = lineStart
	}
	newContent := paramString(params, "new_content")
	oldContent := paramString(params, "old_content")

	lineEnding := detectLineEnding(content)
	normContent := normalizeLineEndings(content)

	if oldContent != "" {
		normOld := normalizeLineEndings(oldContent)
		normNew := normalizeLineEndings(newContent)

		matches, err := findMatchesMultiTier(normContent, normOld)
		if err != nil {
			return "", nil, fmt.Errorf("%s", buildMatchDiagnostics(normContent, normOld, totalLines))
		}
		if len(matches) > 1 {
			return "", nil, fmt.Errorf("ambiguous old_content: %d occurrences matched at lines %s. %s",
				len(matches), summarizeMatchLines(matches), buildAmbiguityHint(normContent, matches))
		}

		m := matches[0]
		replaced := normContent[:m.byteOffset] + normNew + normContent[m.byteOffset+m.byteLength:]

		info := map[string]interface{}{
			"mode":        "content_match",
			"tier":        m.tier,
			"line_start":  m.lineStart,
			"line_end":    m.lineEnd,
			"byte_offset": m.byteOffset,
			"byte_length": m.byteLength,
			"context": map[string]string{
				"before": getContextLines(normContent, m.byteOffset, -2, 0),
				"match":  m.matchedText,
				"after":  getContextLines(normContent, m.byteOffset+m.byteLength, 0, 2),
			},
		}
		return convertLineEndings(replaced, lineEnding), info, nil
	}

	// Line-based fallback
	if lineStart == 0 {
		return "", nil, fmt.Errorf("old_content (preferred) or line_start+line_end must be provided for replace")
	}
	lines := strings.Split(normContent, "\n")
	startIdx := lineStart - 1
	endIdx := lineEnd
	if startIdx < 0 || startIdx >= len(lines) {
		return "", nil, fmt.Errorf("line_start %d out of range (file has %d lines)", lineStart, len(lines))
	}
	if endIdx < startIdx || endIdx > len(lines) {
		return "", nil, fmt.Errorf("line_end %d out of range [start=%d, lines=%d]", lineEnd, lineStart, len(lines))
	}
	inserted := strings.Split(normalizeLineEndings(newContent), "\n")
	newLines := make([]string, 0, len(lines)+len(inserted))
	newLines = append(newLines, lines[:startIdx]...)
	newLines = append(newLines, inserted...)
	newLines = append(newLines, lines[endIdx:]...)

	info := map[string]interface{}{
		"mode":       "line_based",
		"line_start": lineStart,
		"line_end":   lineEnd,
		"note":       "line-based replace used (less reliable, consider old_content+new_content)",
	}
	return convertLineEndings(strings.Join(newLines, "\n"), lineEnding), info, nil
}

// ---------------------------------------------------------------------------
// Multi-tier matching engine
// ---------------------------------------------------------------------------

// findMatchesMultiTier tries progressively more tolerant matching tiers.
// Returns all matches at the first successful tier (to avoid mixing strategies).
// Returns error if NO tier finds matches.
func findMatchesMultiTier(content, old string) ([]matchResult, error) {
	return findMatchesMultiTierInner(content, old, true)
}

func findMatchesMultiTierInner(content, old string, tryEdgeBlank bool) ([]matchResult, error) {
	if old == "" {
		return nil, fmt.Errorf("empty search")
	}

	type tierFn struct {
		name string
		fn   func(c, o string) []matchResult
	}
	tiers := []tierFn{
		{"exact", matchExact},
		{"leading_ws_normalized", matchLeadingWSNormalized},
		{"trailing_ws_tolerant", matchTrailingWSTolerant},
	}

	for _, tier := range tiers {
		matches := tier.fn(content, old)
		if len(matches) > 0 {
			for i := range matches {
				matches[i].tier = tier.name
			}
			return matches, nil
		}
	}

	// Edge-blank tolerant: trim edge blank lines and retry inner tiers 0-2 recursively
	if tryEdgeBlank {
		trimmed := strings.TrimLeft(old, "\r\n")
		trimmed = strings.TrimRight(trimmed, "\r\n")
		if trimmed != old && trimmed != "" {
			matches, err := findMatchesMultiTierInner(content, trimmed, false)
			if err == nil && len(matches) > 0 {
				for i := range matches {
					matches[i].tier = "edge_blank_tolerant (" + matches[i].tier + ")"
				}
				return matches, nil
			}
		}
	}

	return nil, fmt.Errorf("no match in any tier")
}

// --------------- Tier 0: exact text match ---------------
func matchExact(content, old string) []matchResult {
	var out []matchResult
	idx := 0
	for {
		pos := strings.Index(content[idx:], old)
		if pos < 0 {
			break
		}
		abs := idx + pos
		m := buildMatchResult(content, abs, abs+len(old))
		out = append(out, m)
		idx = abs + len(old)
		if idx >= len(content) {
			break
		}
	}
	return out
}

// --------------- Tier 1: leading whitespace normalized (tab<->spaces) ---------------
func matchLeadingWSNormalized(content, old string) []matchResult {
	return structuralMatch(content, old, normalizeLeadingWS)
}

// --------------- Tier 2: trailing whitespace tolerant ---------------
func matchTrailingWSTolerant(content, old string) []matchResult {
	return structuralMatch(content, old, normalizeTrailingWS)
}

// structuralMatch performs line-by-line structural matching with a line normalizer
func structuralMatch(content, old string, lineNorm func(string) string) []matchResult {
	cLines := strings.Split(content, "\n")
	oLines := strings.Split(old, "\n")
	if len(oLines) > len(cLines) {
		return nil
	}

	normCLines := make([]string, len(cLines))
	for i, l := range cLines {
		normCLines[i] = lineNorm(l)
	}
	normOLines := make([]string, len(oLines))
	for i, l := range oLines {
		normOLines[i] = lineNorm(l)
	}

	var out []matchResult
	for i := 0; i <= len(cLines)-len(oLines); i++ {
		match := true
		for j := 0; j < len(oLines); j++ {
			if normCLines[i+j] != normOLines[j] {
				match = false
				break
			}
		}
		if match {
			// Compute byte offsets in original content
			startOffset := lineStartByteOffset(cLines, i)
			endOffset := startOffset + len(cLines[i])
			for j := 1; j < len(oLines); j++ {
				endOffset += 1 + len(cLines[i+j]) // +1 for \n
			}
			mr := matchResult{
				byteOffset:  startOffset,
				byteLength:  endOffset - startOffset,
				lineStart:   i + 1,
				lineEnd:     i + len(oLines),
				matchedText: content[startOffset:endOffset],
			}
			out = append(out, mr)
		}
	}
	return out
}

// --------------- Helpers ---------------

func buildMatchResult(content string, start, end int) matchResult {
	byteOff := 0
	lineStart := 1
	for i, c := range content {
		if i == start {
			lineStart = byteOff + 1
		}
		if c == '\n' {
			byteOff++
		}
	}
	// simpler + safer: compute by scanning up to start
	lineStart = 1
	for i := 0; i < start && i < len(content); i++ {
		if content[i] == '\n' {
			lineStart++
		}
	}
	lineEnd := lineStart
	for i := start; i < end && i < len(content); i++ {
		if content[i] == '\n' {
			lineEnd++
		}
	}
	if end > 0 && end <= len(content) && content[end-1] != '\n' && lineEnd == lineStart && strings.ContainsRune(content[start:end], '\n') {
		// already counted above; no-op guard
	}
	return matchResult{
		byteOffset:  start,
		byteLength:  end - start,
		lineStart:   lineStart,
		lineEnd:     lineEnd,
		matchedText: content[start:end],
	}
}

func lineStartByteOffset(lines []string, lineIndex int) int {
	off := 0
	for i := 0; i < lineIndex; i++ {
		off += len(lines[i]) + 1 // +\n
	}
	return off
}

func normalizeLeadingWS(line string) string {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return " " + line[i:] // single space prefix marker to keep distinct from empty
}

func normalizeTrailingWS(line string) string {
	i := len(line)
	for i > 0 && (line[i-1] == ' ' || line[i-1] == '\t') {
		i--
	}
	return line[:i]
}

func getContextLines(content string, bytePos int, linesBefore, linesAfter int) string {
	// walk backwards to find start of context window
	start := bytePos
	if linesBefore < 0 {
		for i := 0; i > linesBefore && start > 0; i-- {
			start--
			for start > 0 && content[start-1] != '\n' {
				start--
			}
		}
	} else if linesBefore > 0 {
		for i := 0; i < linesBefore; i++ {
			for start < len(content) && content[start] != '\n' {
				start++
			}
			if start < len(content) {
				start++
			}
		}
	}
	end := bytePos
	if linesAfter > 0 {
		for i := 0; i < linesAfter; i++ {
			for end < len(content) && content[end] != '\n' {
				end++
			}
			if end < len(content) {
				end++
			}
		}
		if end < len(content) {
			for end < len(content) && content[end] != '\n' {
				end++
			}
		}
	}
	if end > len(content) {
		end = len(content)
	}
	return content[start:end]
}

func summarizeMatchLines(matches []matchResult) string {
	var parts []string
	for _, m := range matches {
		if m.lineStart == m.lineEnd {
			parts = append(parts, fmt.Sprintf("L%d", m.lineStart))
		} else {
			parts = append(parts, fmt.Sprintf("L%d-%d", m.lineStart, m.lineEnd))
		}
	}
	return strings.Join(parts, ", ")
}

func buildAmbiguityHint(content string, matches []matchResult) string {
	var b strings.Builder
	b.WriteString("Make old_content more unique by including surrounding context. Candidates:\n")
	for i, m := range matches {
		before := getContextLines(content, m.byteOffset, -1, 0)
		after := getContextLines(content, m.byteOffset+m.byteLength, 0, 1)
		fmt.Fprintf(&b, "  [%d] lines %d-%d\n    ...%q<<<MATCH>>>%q...\n",
			i+1, m.lineStart, m.lineEnd, truncate(before, 80), truncate(after, 80))
	}
	return b.String()
}

func buildMatchDiagnostics(content, old string, totalLines int) string {
	cLines := strings.Split(content, "\n")
	oLines := strings.Split(old, "\n")
	var b strings.Builder
	fmt.Fprintf(&b, "old_content not found in file. Diagnostics:\n")
	fmt.Fprintf(&b, "  - old_content: %d lines, %d chars\n", len(oLines), len(old))
	fmt.Fprintf(&b, "  - file: %d lines, %d chars\n", len(cLines), len(content))
	if len(oLines) > 0 {
		// Find closest matching line (first line of old_content as anchor)
		anchor := oLines[0]
		var similar []string
		for i, cl := range cLines {
			if strings.Contains(cl, strings.TrimSpace(anchor)) || strings.Contains(strings.TrimSpace(cl), strings.TrimSpace(anchor)) {
				similar = append(similar, fmt.Sprintf("L%d: %q", i+1, truncate(cl, 100)))
			}
			if len(similar) >= 5 {
				break
			}
		}
		if len(similar) > 0 {
			fmt.Fprintf(&b, "  - Similar lines containing old_content's first line:\n    %s\n", strings.Join(similar, "\n    "))
		}
		fmt.Fprintf(&b, "  - First 3 lines of old_content for reference:\n")
		for i := 0; i < len(oLines) && i < 3; i++ {
			fmt.Fprintf(&b, "    old[%d]: %q\n", i, oLines[i])
		}
	}
	b.WriteString("  HINT: Use read_file to obtain VERBATIM content from the actual file. Even single-space/indent differences cause exact-match failure. This tool now supports leading/trailing whitespace tolerant matching.")
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ---------------------------------------------------------------------------
// insert / delete (line-based, unchanged behavior but more robust errors)
// ---------------------------------------------------------------------------

func (t *FileEditTool) insertContent(content string, params map[string]interface{}) (string, error) {
	insertIdx := paramInt(params, "line_start")
	newContent := paramString(params, "new_content")

	lineEnding := detectLineEnding(content)
	normContent := normalizeLineEndings(content)
	lines := strings.Split(normContent, "\n")

	if insertIdx < 0 || insertIdx > len(lines) {
		return "", fmt.Errorf("line_start %d out of range (file has %d lines, valid range 0..%d for insert)", insertIdx, len(lines), len(lines))
	}

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
	normContent := normalizeLineEndings(content)
	lines := strings.Split(normContent, "\n")
	startIdx := lineStart - 1
	endIdx := lineEnd

	if startIdx < 0 || startIdx >= len(lines) {
		return "", fmt.Errorf("line_start %d out of range (file has %d lines)", lineStart, len(lines))
	}
	if endIdx < startIdx || endIdx > len(lines) {
		return "", fmt.Errorf("line_end %d out of range (file has %d lines, valid up to line_start=%d)", lineEnd, len(lines), lineStart)
	}

	newLines := make([]string, 0, len(lines)-(endIdx-startIdx))
	newLines = append(newLines, lines[:startIdx]...)
	newLines = append(newLines, lines[endIdx:]...)

	return convertLineEndings(strings.Join(newLines, "\n"), lineEnding), nil
}
