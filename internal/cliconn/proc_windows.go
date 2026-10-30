//go:build windows

package cliconn

import (
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// createNoWindow 让子进程不弹控制台窗口。
//
// 服务进程（gateway/TUI 之外的托盘版）拉起 cmd 时不加这个标志，用户桌面会
// 每调一次工具闪一个黑框——CLI 连接器的调用频率远比手动执行高。
const createNoWindow = 0x08000000

func setProcAttrs(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}

// normalizeEnvKey 统一环境变量名的大小写：Windows 的变量名大小写不敏感，
// `Path` 与 `PATH` 是同一个变量，去重时必须归一，否则会同时塞两条进去。
func normalizeEnvKey(key string) string { return strings.ToUpper(key) }

// isBatchScript 报告 command 是否是 .bat/.cmd（不能直接 CreateProcess）。
func isBatchScript(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".bat", ".cmd":
		return true
	}
	return false
}
