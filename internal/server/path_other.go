//go:build !windows

package server

// 非 Windows 平台无需注册表刷新：POSIX 安装器（apt/brew）改的是磁盘上的
// 目录与 shell 配置，PATH 本身不会被改写，进程内的 LookPath 天然能看到
// 新装的可执行文件。保持空实现让调用点无需按平台分支。
func refreshProcessPath() {}

// addUserPathEntry 仅 Windows 便携包直装会调用（portableSpec 只配在
// Windows 上）；其它平台永远走不到这里，空实现即可。
func addUserPathEntry(dir string) error { return nil }

// notifyEnvChanged Windows 上广播 WM_SETTINGCHANGE；POSIX 无此概念。
func notifyEnvChanged() {}
