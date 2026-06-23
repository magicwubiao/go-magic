//go:build !windows
// +build !windows

package server

import (
	"fmt"
	"net"
	"syscall"
)

// syscallKill sends a signal to a process or process group
// On Unix: if pid is negative, the signal is sent to the process group
func syscallKill(pid int, sig syscall.Signal) error {
	return syscall.Kill(pid, sig)
}

var (
	syscallSIGTERM = syscall.SIGTERM
	syscallSIGKILL = syscall.SIGKILL
)

func isPort8080Free() bool {
	return isPortFree(8080)
}

func isPort8081Free() bool {
	return isPortFree(8081)
}

func isPortFree(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}
