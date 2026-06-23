//go:build !windows
// +build !windows

package server

import (
	"os"
	"syscall"
)

// isProcessAlive returns true if the process with the given pid is running.
// On Unix, we use signal(0) which succeeds only if the process exists
// and we have permission to signal it.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return false
	}
	return true
}
