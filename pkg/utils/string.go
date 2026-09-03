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

// TailString 按 rune 安全返回字符串末尾最多 maxLen 个字符，超长时在开头
// 标注总字符数。与 TruncateDetailed 配合，用于日志中同时展示大字符串
// （如被截断的工具参数 JSON）的头尾。
func TailString(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	total := utf8.RuneCountInString(s)
	if total <= maxLen {
		return s
	}
	runes := []rune(s)
	return fmt.Sprintf("[truncated, total %d chars] ...", total) + string(runes[total-maxLen:])
}
