//go:build !windows

package server

// 非 Windows 平台无需注册表刷新：POSIX 安装器（apt/brew）改的是磁盘上的
// 目录与 shell 配置，PATH 本身不会被改写，进程内的 LookPath 天然能看到
// 新装的可执行文件。保持空实现让调用点无需按平台分支。
func refreshProcessPath() {}
