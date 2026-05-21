//go:build windows
// +build windows

package gateway

import (
	"syscall"
)

func init() {
	// Set console to UTF-8 mode on Windows
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setConsoleCP := kernel32.NewProc("SetConsoleCP")
	setConsoleOutputCP := kernel32.NewProc("SetConsoleOutputCP")

	// 65001 is UTF-8 code page
	setConsoleCP.Call(65001)
	setConsoleOutputCP.Call(65001)
}
