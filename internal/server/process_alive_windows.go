//go:build windows
// +build windows

package server

import (
	"golang.org/x/sys/windows"
)

// isProcessAlive returns true if the process with the given pid is running.
// On Windows, signal(0) is not supported, so we use
// OpenProcess + GetExitCodeProcess instead.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var exitCode uint32
	if err := windows.GetExitCodeProcess(h, &exitCode); err != nil {
		return false
	}
	// A process is still alive if its exit code is 259 (STILL_ACTIVE).
	return exitCode == 259
}
