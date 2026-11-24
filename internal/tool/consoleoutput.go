package tool

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// decodeConsoleOutput 把子进程输出转成 UTF-8 文本。
//
// 中文 Windows 的控制台代码页默认是 GBK（cp936）：PowerShell/cmd 下系统命令
// （ipconfig、systeminfo、ver 等）输出的中文是 GBK 字节，直接 string(b) 塞给
// 模型会得到一串替换字符（此前 execute_command 报错信息里的 "????" 即此因），
// 模型还会据乱码误判命令结果。策略与 internal/cliconn 的 decodeOutput 一致：
//   - 合法 UTF-8 → 原样返回（绝大多数场景：Go/Node/Python 子进程、chcp 65001）
//   - 否则尝试 GB18030 严格解码（GB18030 是 GBK 的超集，向下兼容）
//   - 仍失败 → 逐字节容错替换为 U+FFFD，至少保证不产生非法 UTF-8
func decodeConsoleOutput(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	if utf8.Valid(b) {
		return string(b)
	}
	if decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(b); err == nil && utf8.Valid(decoded) {
		return string(decoded)
	}
	return strings.ToValidUTF8(string(b), "\uFFFD")
}
