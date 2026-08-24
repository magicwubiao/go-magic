package tool

import (
	"context"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
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
	return "PRECISELY edit existing files using EXACT text matching (old_content -> new_content). Multi-level tolerant matching: exact text → leading whitespace(tab<->spaces) → trailing whitespace → leading+trailing combined → blank-edge tolerant. Always reports match location + surrounding context for verification. Prefer this over shell commands (sed/grep/python) for deterministic edits."
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
				"description": "1-based line number. Only use when old_content truly cannot match. For insert: line_start=0 means at top of file (before line 1). For replace/delete: inclusive start line, minimum 1.",
			},
			"line_end": map[string]interface{}{
				"type":        "number",
				"description": "1-based ending line (inclusive) for line-based replace/delete. Optional, defaults to line_start. Must be >= line_start and >= 1.",
			},
			"old_content": map[string]interface{}{
				"type":        "string",
				"description": "STRONGLY RECOMMENDED. The existing text in the file to replace. The tool performs tolerant matching: 1) exact match, 2) tab<->space leading whitespace tolerant, 3) trailing whitespace tolerant, 4) leading+trailing combined tolerant, 5) blank-edge tolerant (trim extra leading/trailing blank lines). Include 3+ lines for uniqueness. If multiple matches found, the tool will report all locations and refuse to modify.",
			},
			"new_content": map[string]interface{}{
				"type":        "string",
				"description": "Replacement / insertion content. Multi-line accepted with real newlines. The actual original file line endings will be preserved when tolerant matching succeeds.",
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
			return "", nil, fmt.Errorf("%s", buildMatchDiagnostics(normContent, normOld, totalLines, err))
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
				"before": getContextLines(normContent, m.byteOffset, 2, 0),
				"match":  m.matchedText,
				"after":  getContextLines(normContent, m.byteOffset+m.byteLength, 0, 3),
			},
		}
		return convertLineEndings(replaced, lineEnding), info, nil
	}

	// Line-based fallback (1-based)
	if lineStart == 0 {
		return "", nil, fmt.Errorf("old_content (preferred) or line_start+line_end must be provided for replace; line_start must be >= 1 (1-based)")
	}
	if lineStart < 0 || lineEnd < 0 {
		return "", nil, fmt.Errorf("line_start/line_end must be >= 1 (1-based)")
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
// Multi-tier matching engine (5 tiers, CRITICAL #1 FIX: combined tier added)
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
		{"leading_ws", matchLeadingWSNormalized},
		{"trailing_ws", matchTrailingWSTolerant},
		{"leading+trailing_ws", matchLeadingAndTrailingWSNormalized}, // CRITICAL #1: NEW combined tier
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

	// Edge-blank tolerant: trim edge blank lines and retry inner tiers 0-3 recursively
	if tryEdgeBlank {
		trimmed := strings.TrimLeft(old, "\r\n")
		trimmed = strings.TrimRight(trimmed, "\r\n")
		if trimmed != old && trimmed != "" {
			matches, err := findMatchesMultiTierInner(content, trimmed, false)
			if err == nil && len(matches) > 0 {
				for i := range matches {
					matches[i].tier = "edge_blank+" + matches[i].tier
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
	return structuralMatch(content, old, []lineNormalizer{normalizeLeadingWS})
}

// --------------- Tier 2: trailing whitespace tolerant ---------------
func matchTrailingWSTolerant(content, old string) []matchResult {
	return structuralMatch(content, old, []lineNormalizer{normalizeTrailingWS})
}

// --------------- Tier 3: leading + trailing whitespace combined (CRITICAL #1) ---------------
func matchLeadingAndTrailingWSNormalized(content, old string) []matchResult {
	return structuralMatch(content, old, []lineNormalizer{normalizeLeadingWS, normalizeTrailingWS})
}

type lineNormalizer func(string) string

// applyNormalizers applies a chain of line normalizers
func applyNormalizers(line string, norms []lineNormalizer) string {
	for _, n := range norms {
		line = n(line)
	}
	return line
}

// structuralMatch performs line-by-line structural matching with a chain of normalizers
// LOW #11 FIX: uses prefix-sum array for O(1) byte-offset lookup on large files
func structuralMatch(content, old string, normalizers []lineNormalizer) []matchResult {
	cLines := strings.Split(content, "\n")
	oLines := strings.Split(old, "\n")
	if len(oLines) > len(cLines) {
		return nil
	}

	// LOW #11: precompute prefix byte-offsets O(N) instead of per-hit O(i)
	prefixOffsets := make([]int, len(cLines)+1)
	for i := 0; i < len(cLines); i++ {
		prefixOffsets[i+1] = prefixOffsets[i] + len(cLines[i]) + 1 // +\n
	}

	normCLines := make([]string, len(cLines))
	for i, l := range cLines {
		normCLines[i] = applyNormalizers(l, normalizers)
	}
	normOLines := make([]string, len(oLines))
	for i, l := range oLines {
		normOLines[i] = applyNormalizers(l, normalizers)
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
			startOffset := prefixOffsets[i]
			endOffset := prefixOffsets[i+len(oLines)]
			if endOffset > 0 {
				endOffset-- // remove trailing \n counted by prefixOffsets[last+1]
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

// HIGH #4: buildMatchResult - removed dead first-scan, removed impossible guard,
// renamed misleading variable.
func buildMatchResult(content string, start, end int) matchResult {
	// compute lineStart by scanning only up to `start` (matches "simpler + safer" comment intent)
	lineStart := 1
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
	return matchResult{
		byteOffset:  start,
		byteLength:  end - start,
		lineStart:   lineStart,
		lineEnd:     lineEnd,
		matchedText: content[start:end],
	}
}

// unused helper preserved for external callers (same impl as prefixOffsets, kept for API compat)
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

// CRITICAL #2 FIX: getContextLines unified to always use positive linesBefore/linesAfter.
// linesBefore = N: include N lines BEFORE bytePos.
// linesAfter  = M: include M lines AFTER bytePos.
// If bytePos is at the start of a line, that line is NOT counted in "before".
// If bytePos is at the end   of a line, that line is NOT counted in "after".
func getContextLines(content string, bytePos int, linesBefore, linesAfter int) string {
	if bytePos < 0 {
		bytePos = 0
	}
	if bytePos > len(content) {
		bytePos = len(content)
	}

	start := bytePos
	if linesBefore > 0 {
		for i := 0; i < linesBefore && start > 0; i++ {
			// move back 1 char, then continue back to preceding newline
			start--
			for start > 0 && content[start-1] != '\n' {
				start--
			}
		}
	}

	end := bytePos
	if linesAfter > 0 {
		for i := 0; i < linesAfter; i++ {
			// move forward to next '\n'
			for end < len(content) && content[end] != '\n' {
				end++
			}
			if end < len(content) {
				end++ // include the '\n' so after-context terminates cleanly
			} else {
				break
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
	b.WriteString("Make old_content more unique by including more surrounding context (include 3+ lines of context). Candidates:\n")
	for i, m := range matches {
		before := getContextLines(content, m.byteOffset, 1, 0)
		after := getContextLines(content, m.byteOffset+m.byteLength, 0, 1)
		fmt.Fprintf(&b, "  [%d] lines %d-%d\n    ...%q<<<MATCH>>>%q...\n",
			i+1, m.lineStart, m.lineEnd, truncateRune(before, 80), truncateRune(after, 80))
	}
	b.WriteString("  (Non-overlapping occurrences reported; overlap in repeated patterns is skipped.)")
	return b.String()
}

// HIGH #6 FIX: now accepts and wraps the original cause (empty search vs no match)
// MEDIUM #9 FIX: anchor search uses entire first non-empty line, not trimmed generic word
func buildMatchDiagnostics(content, old string, totalLines int, cause error) string {
	cLines := strings.Split(content, "\n")
	oLines := strings.Split(old, "\n")
	var b strings.Builder
	fmt.Fprintf(&b, "old_content not found in file. Diagnostics:\n")
	fmt.Fprintf(&b, "  - old_content: %d lines, %d chars\n", len(oLines), len(old))
	fmt.Fprintf(&b, "  - file: %d lines, %d chars\n", len(cLines), len(content))
	if cause != nil {
		fmt.Fprintf(&b, "  - cause: %v\n", cause)
	}

	if len(oLines) > 0 {
		// MEDIUM #9: use the FULL first non-empty line as anchor, not TrimSpace.
		// This avoids false matches on generic words like "func" / "if" / "return".
		anchor := ""
		for _, l := range oLines {
			if strings.TrimSpace(l) != "" {
				anchor = l
				break
			}
		}
		if anchor != "" {
			anchorTrim := strings.TrimSpace(anchor)
			var similar []string
			for i, cl := range cLines {
				ct := strings.TrimSpace(cl)
				if ct == "" {
					continue
				}
				// Prefer exact whole-line match, then contains, then reversed contains.
				score := 0
				switch {
				case cl == anchor:
					score = 3
				case ct == anchorTrim:
					score = 2
				case strings.Contains(cl, anchorTrim):
					score = 1
				case strings.Contains(anchorTrim, ct) && len(ct) > len(anchorTrim)/2:
					score = 1
				}
				if score > 0 {
					similar = append(similar, fmt.Sprintf("L%d: %q", i+1, truncateRune(cl, 120)))
				}
				if len(similar) >= 5 {
					break
				}
			}
			if len(similar) > 0 {
				fmt.Fprintf(&b, "  - Closest matching lines based on old_content's first non-empty line:\n    %s\n", strings.Join(similar, "\n    "))
			}
		}

		fmt.Fprintf(&b, "  - First 3 lines of old_content (VERBATIM reference):\n")
		for i := 0; i < len(oLines) && i < 3; i++ {
			fmt.Fprintf(&b, "    old[%d]: %q\n", i, oLines[i])
		}
	}
	b.WriteString("  HINT: Use read_file to obtain VERBATIM content from the actual file. Even single-space/indent differences used to cause exact-match failure. This tool now supports 5-tier tolerant matching: exact → leading_ws → trailing_ws → leading+trailing_ws → edge_blank.")
	return b.String()
}

// MEDIUM #7 FIX: truncate by rune instead of byte, avoids UTF-8 mid-character corruption
func truncateRune(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n]) + "…"
}

// truncate preserved as backward-compat alias for other callers
func truncate(s string, n int) string {
	return truncateRune(s, n)
}

// ---------------------------------------------------------------------------
// insert / delete (line-based, unified 1-based semantics + robust validation)
// ---------------------------------------------------------------------------

// CRITICAL #3 FIX: insert line_start unified to "0-based-like 0 allowed for top,
// 1..N means after line N (consistent with 1-based readability used elsewhere)."
func (t *FileEditTool) insertContent(content string, params map[string]interface{}) (string, error) {
	insertLine := paramInt(params, "line_start")
	newContent := paramString(params, "new_content")

	lineEnding := detectLineEnding(content)
	normContent := normalizeLineEndings(content)
	lines := strings.Split(normContent, "\n")

	// LOW #10 FIX: range printed once, with semantics
	if insertLine < 0 || insertLine > len(lines) {
		return "", fmt.Errorf("line_start %d out of range. Valid: 0 (insert at top / before line 1) .. %d (insert at end / after line %d, file has %d lines)",
			insertLine, len(lines), len(lines), len(lines))
	}

	inserted := strings.Split(normalizeLineEndings(newContent), "\n")
	newLines := make([]string, 0, len(lines)+len(inserted))
	newLines = append(newLines, lines[:insertLine]...)
	newLines = append(newLines, inserted...)
	newLines = append(newLines, lines[insertLine:]...)

	return convertLineEndings(strings.Join(newLines, "\n"), lineEnding), nil
}

// HIGH #5 + CRITICAL #3 FIX: delete line_start/line_end strictly 1-based, validate min values
func (t *FileEditTool) deleteContent(content string, params map[string]interface{}) (string, error) {
	lineStart := paramInt(params, "line_start")
	lineEnd := lineStart
	if le := paramInt(params, "line_end"); le != 0 || params["line_end"] != nil {
		lineEnd = le
	}

	// Strict validation for 1-based semantics shared with replace
	if lineStart < 1 {
		return "", fmt.Errorf("line_start must be >= 1 (1-based) for delete operation, got %d", lineStart)
	}
	if lineEnd < 1 {
		return "", fmt.Errorf("line_end must be >= 1 (1-based) for delete operation, got %d", lineEnd)
	}

	lineEnding := detectLineEnding(content)
	normContent := normalizeLineEndings(content)
	lines := strings.Split(normContent, "\n")
	startIdx := lineStart - 1
	endIdx := lineEnd

	if startIdx >= len(lines) {
		return "", fmt.Errorf("line_start %d out of range (file has %d lines)", lineStart, len(lines))
	}
	if endIdx < startIdx || endIdx > len(lines) {
		return "", fmt.Errorf("line_end %d out of range. Must satisfy %d <= line_end <= %d",
			lineEnd, lineStart, len(lines))
	}

	newLines := make([]string, 0, len(lines)-(endIdx-startIdx))
	newLines = append(newLines, lines[:startIdx]...)
	newLines = append(newLines, lines[endIdx:]...)

	return convertLineEndings(strings.Join(newLines, "\n"), lineEnding), nil
}
