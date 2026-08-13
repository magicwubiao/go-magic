package utils

import (
	"fmt"
	"unicode/utf8"
)

// Truncate 按 rune 安全截断字符串到 maxLen 个字符（rune），超出则追加 "..."。
// 使用 rune 计数避免切断多字节 UTF-8 字符（如中文）产生无效 UTF-8。
func Truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxLen]) + "..."
}

// TruncateDetailed 按 rune 安全截断，并在末尾标注总字符数。
func TruncateDetailed(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	total := utf8.RuneCountInString(s)
	if total <= maxLen {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxLen]) + fmt.Sprintf("... [truncated, total %d chars]", total)
}
