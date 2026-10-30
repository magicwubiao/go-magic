//go:build !windows

package cliconn

import "os/exec"

// setProcAttrs 在非 Windows 平台无需处理：子进程继承已打开的 stdio，
// 也不会弹窗。
func setProcAttrs(cmd *exec.Cmd) {}

// normalizeEnvKey 在类 Unix 平台保持原样：环境变量名大小写敏感。
func normalizeEnvKey(key string) string { return key }

// isBatchScript 在非 Windows 平台恒为 false（.bat/.cmd 只是普通文件名）。
func isBatchScript(name string) bool { return false }
