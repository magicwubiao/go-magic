//go:build windows
// +build windows

package server

import (
	"fmt"
	"net"
	"syscall"
)

func syscallKill(pid int, sig syscall.Signal) error {
	// On Windows, signal is a no-op; use TerminateProcess via os.FindProcess
	return nil
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
