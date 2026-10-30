//go:build !windows

package cliconn

import "os/exec"

// setProcAttrs 在非 Windows 平台无需处理：子进程继承已打开的 stdio，
// 也不会弹窗。
func setProcAttrs(cmd *exec.Cmd) {}

// isBatchScript 在非 Windows 平台恒为 false（.bat/.cmd 只是普通文件名）。
func isBatchScript(name string) bool { return false }
