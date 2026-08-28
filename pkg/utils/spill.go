package utils

// ErrTruncateDetailed 错误分支专用截断：错误信息同样可能巨大（堆栈、响应体回显）。
// 已移除 tooloutput 落盘机制，超长内容统一按 rune 安全截断。
func ErrTruncateDetailed(errText, label string, maxLen int) string {
	if errText == "" {
		return ""
	}
	_ = label // label 保留参数位以兼容旧调用点，不再参与落盘文件名
	return TruncateDetailed(errText, maxLen)
}
