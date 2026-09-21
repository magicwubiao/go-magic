package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// userHomeDir 返回当前用户的真实主目录。
//
// Windows 上刻意不读 HOME：Git Bash / MSYS 把 HOME 设成 `/c/Users/xxx` 这类
// POSIX 风格路径，拼出的目录在原生进程里是错的（会变成 `\c\Users\...`）。
// 类 Unix 上优先 HOME（与 shell 的 `~` 语义一致，也让测试可注入）。
func userHomeDir() string {
	if runtime.GOOS != "windows" {
		if home := os.Getenv("HOME"); home != "" {
			return home
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home
	}
	return ""
}

// ExpandHome 把配置里以 `~` 开头的路径展开为真实的用户主目录。
//
// 支持的写法：`~`、`~/a/b`、`~\a\b`（两种分隔符都接受，Windows 上最终统一成
// 反斜杠）。`~user/...` 这种指向他人主目录的形式不展开，原样返回——Go 标准库
// 没有跨平台的实现，猜错比不猜更糟。
//
// 配置文件里写 `~/.magic/browser-profile` 这类路径时，一旦少了展开，进程就会在
// 当前工作目录（打包后常是安装目录）下建出一个名为 `~` 的字面量文件夹——这正是
// 它存在的理由：所有"配置里的路径"在落盘/传给子进程之前都必须过一遍这里。
func ExpandHome(p string) string {
	if p == "" || p[0] != '~' {
		return p
	}
	// 只有 `~` 本身或以分隔符结尾的形式才算家目录前缀。
	if len(p) > 1 && p[1] != '/' && p[1] != '\\' {
		return p
	}
	home := userHomeDir()
	if home == "" {
		// 拿不到主目录：保持原样，让调用方报出真实的路径错误，
		// 而不是悄悄把文件写到 CWD 下的 `~` 里。
		return p
	}
	rest := strings.TrimLeft(p[1:], `/\`)
	if rest == "" {
		return home
	}
	// 反斜杠按分隔符处理（`~\dir` 与 `~/dir` 等价）；filepath.Join 负责按当前
	// 平台归一化分隔符。
	rest = strings.ReplaceAll(rest, `\`, "/")
	return filepath.Join(home, rest)
}
