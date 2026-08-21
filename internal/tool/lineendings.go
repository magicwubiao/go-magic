package tool

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Line ending style constants.
const (
	// LineEndingLF is the Unix-style line ending.
	LineEndingLF = "\n"
	// LineEndingCRLF is the Windows-style line ending.
	LineEndingCRLF = "\r\n"
	// binaryCheckSampleSize is how many bytes to sample for binary detection.
	binaryCheckSampleSize = 8192
	// binaryNullThreshold is the ratio of null bytes to trigger binary classification.
	binaryNullThreshold = 0.01
)

// codeFileExtensions maps lowercased extensions to true for known code/text types.
// Used to avoid binary detection false positives on legit encoded source files.
var codeFileExtensions = map[string]bool{
	".go": true, ".py": true, ".js": true, ".jsx": true, ".ts": true, ".tsx": true,
	".rs": true, ".java": true, ".c": true, ".h": true, ".cpp": true, ".hpp": true,
	".cc": true, ".cs": true, ".rb": true, ".php": true, ".swift": true, ".kt": true,
	".scala": true, ".r": true, ".m": true, ".mm": true, ".pl": true, ".sh": true,
	".bash": true, ".zsh": true, ".fish": true, ".bat": true, ".cmd": true, ".ps1": true,
	".lua": true, ".vim": true, ".vimrc": true, ".el": true, ".clj": true, ".ex": true,
	".exs": true, ".erl": true, ".hs": true, ".ml": true, ".fs": true, ".fsi": true,
	".fsx": true, ".sql": true, ".vue": true, ".svelte": true,
	".json": true, ".yaml": true, ".yml": true, ".toml": true, ".xml": true,
	".ini": true, ".cfg": true, ".conf": true, ".properties": true, ".env": true,
	".md": true, ".markdown": true, ".rst": true, ".adoc": true, ".txt": true,
	".log": true, ".csv": true, ".tsv": true,
	".html": true, ".htm": true, ".css": true, ".scss": true, ".sass": true, ".less": true,
	".svg": true, ".http": true, ".rest": true, ".graphql": true, ".gql": true,
	".dockerfile": true, ".makefile": true,
	".proto": true, ".thrift": true, ".avsc": true, ".tf": true, ".hcl": true,
	".cabal": true, ".nix": true, ".dhall": true, ".purs": true, ".idr": true,
	".agda": true, ".lean": true, ".coq": true, ".isabelle": true, ".thy": true,
}

// detectLineEnding detects the dominant line ending style of file content.
// Returns "\r\n" if CRLF is dominant, "\n" otherwise (including empty content).
func detectLineEnding(content string) string {
	crlf := strings.Count(content, "\r\n")
	lf := strings.Count(content, "\n") - crlf
	if crlf > lf {
		return LineEndingCRLF
	}
	return LineEndingLF
}

// normalizeLineEndings converts all line endings (CRLF and legacy CR) to LF.
// This is the canonical form used for text matching, so that old_text/old_content
// supplied with "\n" can match file content that uses "\r\n".
func normalizeLineEndings(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// convertLineEndings converts all line endings in s to the target ending.
// s is first normalized to LF, then converted. This ensures edits never
// produce mixed line endings inside a file.
func convertLineEndings(s, ending string) string {
	s = normalizeLineEndings(s)
	if ending == LineEndingCRLF {
		return strings.ReplaceAll(s, "\n", "\r\n")
	}
	return s
}

// ---------------------------------------------------------------------------
// Content classification helpers
// ---------------------------------------------------------------------------

// isBinaryContent reports whether the raw bytes are likely binary (not text).
// Uses two heuristics: presence of NUL bytes, and ratio of non-printable chars.
func isBinaryContent(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	sample := data
	if len(sample) > binaryCheckSampleSize {
		sample = sample[:binaryCheckSampleSize]
	}
	// Fast path: count NUL bytes (very reliable for most formats)
	nuls := bytes.Count(sample, []byte{0})
	if len(sample) > 0 && float64(nuls)/float64(len(sample)) >= binaryNullThreshold {
		return true
	}
	// Slower: count non-printable runes (excluding common whitespace/control)
	nonPrint := 0
	totalRunes := 0
	buf := sample
	for len(buf) > 0 {
		r, size := utf8.DecodeRune(buf)
		if r == utf8.RuneError && size == 1 {
			// Invalid UTF-8 byte; treat as suspicious but not automatically binary
			nonPrint++
		} else if !isTextRune(r) {
			nonPrint++
		}
		totalRunes++
		buf = buf[size:]
	}
	if totalRunes > 0 && float64(nonPrint)/float64(totalRunes) > 0.30 {
		return true
	}
	return false
}

// isTextRune reports whether r is a rune commonly found in text files
// (letters, digits, punctuation, whitespace, and common formatting controls).
func isTextRune(r rune) bool {
	if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) || unicode.IsMark(r) {
		return true
	}
	switch r {
	case ' ', '\t', '\n', '\r', '\f', '\v', '\b', '\u0000',
		'\u00a0', '\u1680', '\u2000', '\u2001', '\u2002', '\u2003',
		'\u2004', '\u2005', '\u2006', '\u2007', '\u2008', '\u2009',
		'\u200a', '\u2028', '\u2029', '\u202f', '\u205f', '\u3000':
		return true
	}
	// Allow C0/C1 control chars only when explicitly accepted above.
	return false
}

// isCodeFile reports whether path has a known code/text extension.
// When true, binary detection can be skipped/relaxed.
func isCodeFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if codeFileExtensions[ext] {
		return true
	}
	base := strings.ToLower(filepath.Base(path))
	return base == "dockerfile" || base == "makefile" ||
		base == "gemfile" || base == "rakefile" ||
		strings.HasSuffix(base, ".dockerfile")
}

// classifyFile returns two booleans: isCode (extension-based) and isBinary (content-based).
func classifyFile(path string, data []byte) (bool, bool) {
	code := isCodeFile(path)
	if code {
		sample := data
		if len(sample) > binaryCheckSampleSize {
			sample = sample[:binaryCheckSampleSize]
		}
		return code, isBinaryContent(data) && bytes.Count(sample, []byte{0}) > 0
	}
	return code, isBinaryContent(data)
}

// ---------------------------------------------------------------------------
// Search/replace helpers (exact, precise counting and positioning)
// ---------------------------------------------------------------------------

// MatchInfo records the position of a single match in normalized-LF content.
type MatchInfo struct {
	ByteStart   int `json:"byte_start"`
	ByteEnd     int `json:"byte_end"`
	LineStart   int `json:"line_start"` // 1-based
	LineEnd     int `json:"line_end"`   // 1-based, inclusive
	ColumnStart int `json:"column_start"`
	ColumnEnd   int `json:"column_end"`
}

// findAllMatches returns the positions of all non-overlapping occurrences of
// search in s, honoring case-sensitivity. search and s must already be in
// normalized LF form.
func findAllMatches(s, search string, caseSensitive bool) []MatchInfo {
	if search == "" {
		return nil
	}
	hay := s
	needle := search
	if !caseSensitive {
		hay = strings.ToLower(hay)
		needle = strings.ToLower(needle)
	}
	var matches []MatchInfo
	offset := 0
	for {
		idx := strings.Index(hay[offset:], needle)
		if idx < 0 {
			break
		}
		start := offset + idx
		end := start + len(needle)
		matches = append(matches, computeMatchInfo(s, start, end))
		offset = end
		if offset >= len(hay) {
			break
		}
	}
	return matches
}

// computeMatchInfo computes line/column info for a byte range in s (LF-normalized).
func computeMatchInfo(s string, start, end int) MatchInfo {
	mi := MatchInfo{ByteStart: start, ByteEnd: end, LineStart: 1, LineEnd: 1, ColumnStart: 1, ColumnEnd: 1}
	// Walk up to start
	line := 1
	lastNL := -1
	for i := 0; i < start; i++ {
		if s[i] == '\n' {
			line++
			lastNL = i
		}
	}
	mi.LineStart = line
	mi.ColumnStart = start - lastNL
	// Walk up to end
	for i := start; i < end; i++ {
		if i < len(s) && s[i] == '\n' {
			line++
			lastNL = i
		}
	}
	mi.LineEnd = line
	mi.ColumnEnd = end - lastNL
	if mi.ColumnEnd < 1 {
		mi.ColumnEnd = 1
	}
	return mi
}

// replaceAllExact replaces all (or first N) occurrences, preserving case
// when caseSensitive is true. Returns the new content and actual change count.
// search/replace/s must all be LF-normalized already.
func replaceAllExact(s, search, replace string, caseSensitive bool, maxReplace int) (string, int) {
	if search == "" || maxReplace == 0 {
		return s, 0
	}
	matches := findAllMatches(s, search, caseSensitive)
	if len(matches) == 0 {
		return s, 0
	}
	if maxReplace < 0 || maxReplace > len(matches) {
		maxReplace = len(matches)
	}
	matches = matches[:maxReplace]
	// Build result by copying non-match ranges + replacement for matches
	var sb strings.Builder
	sb.Grow(len(s) + len(matches)*(len(replace)-len(search)))
	cursor := 0
	for i, m := range matches {
		_ = i
		sb.WriteString(s[cursor:m.ByteStart])
		sb.WriteString(replace)
		cursor = m.ByteEnd
	}
	sb.WriteString(s[cursor:])
	return sb.String(), len(matches)
}

// countContentLines returns the number of lines in LF-normalized content.
func countContentLines(content string) int {
	if content == "" {
		return 0
	}
	return strings.Count(content, "\n") + 1
}

// paramInt reads a numeric parameter as an int, tolerating float64 (JSON
// decoding), int/int64 (direct map construction in Go), and json.Number.
// Returns 0 if the key is missing or not numeric.
func paramInt(params map[string]interface{}, key string) int {
	switch v := params[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case json.Number:
		if f, err := v.Int64(); err == nil {
			return int(f)
		}
	}
	return 0
}

// paramBool reads a boolean parameter. Returns false if missing or not bool.
func paramBool(params map[string]interface{}, key string, def bool) bool {
	switch v := params[key].(type) {
	case bool:
		return v
	case nil:
		return def
	}
	return def
}

// paramString reads a string parameter. Returns "" if missing or not a string.
func paramString(params map[string]interface{}, key string) string {
	s, _ := params[key].(string)
	return s
}
