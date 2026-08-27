package utils

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// spillDirName 超长工具输出落盘的目录名（相对 ~/.magic/）。
const spillDirName = "tool_outputs"

// SanitizeFilenameLabel 清理用于文件名的标签：仅保留字母数字、下划线与短横线，
// 其余替换为 '_'，并限制长度，防止路径注入或超长文件名。
func SanitizeFilenameLabel(label string, maxLen int) string {
	var b strings.Builder
	for _, r := range label {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	s := b.String()
	if s == "" {
		s = "output"
	}
	return Truncate(s, maxLen)
}

// SpillToFile 将完整内容落盘到 ~/.magic/tool_outputs/<ts>_<label>_<rand>.txt，
// 返回文件绝对路径；失败返回空字符串（调用方应优雅降级为纯截断）。
// 文件权限 0600：工具输出可能包含敏感信息（日志、代码、密钥片段）。
func SpillToFile(content, label string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, ".magic", spillDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	// 随机后缀避免同秒并发覆盖
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		suffix = []byte{byte(time.Now().UnixNano() >> 8), byte(time.Now().UnixNano())}
	}
	name := fmt.Sprintf("%s_%s_%s.txt",
		time.Now().Format("20060102_150405"),
		SanitizeFilenameLabel(label, 64),
		hex.EncodeToString(suffix),
	)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return ""
	}
	return path
}

// TruncateWithSpill rune 安全截断到 maxLen 个字符；若发生截断，则将完整内容
// 落盘并在截断文本末尾追加指针，供后续人工或工具按需读取全文。
// 该函数是防止 92K+ 巨型消息进入上下文的统一入口。
func TruncateWithSpill(content, label string, maxLen int) string {
	total := utf8.RuneCountInString(content)
	if total <= maxLen {
		return content
	}
	head := TruncateDetailed(content, maxLen)
	if path := SpillToFile(content, label); path != "" {
		head += fmt.Sprintf("\n[full output (%d chars) saved to %s]", total, path)
	}
	return head
}

// ErrTruncateWithSpill 错误分支专用：错误信息同样可能巨大（堆栈、响应体回显）。
func ErrTruncateWithSpill(err error, label string, maxLen int) string {
	if err == nil {
		return ""
	}
	return TruncateWithSpill(fmt.Sprintf("Error: %v", err), label+"_error", maxLen)
}
