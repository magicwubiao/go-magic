package tool

import (
	"encoding/json"
	"strings"
)

// Line ending style constants.
const (
	// LineEndingLF is the Unix-style line ending.
	LineEndingLF = "\n"
	// LineEndingCRLF is the Windows-style line ending.
	LineEndingCRLF = "\r\n"
)

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

// paramString reads a string parameter. Returns "" if missing or not a string.
func paramString(params map[string]interface{}, key string) string {
	s, _ := params[key].(string)
	return s
}
