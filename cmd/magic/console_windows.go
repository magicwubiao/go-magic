//go:build windows
// +build windows

package main

import (
	"syscall"
)

func initWindowsConsole() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setConsoleCP := kernel32.NewProc("SetConsoleCP")
	setConsoleOutputCP := kernel32.NewProc("SetConsoleOutputCP")
	setConsoleCP.Call(65001)       // UTF-8
	setConsoleOutputCP.Call(65001) // UTF-8
}
