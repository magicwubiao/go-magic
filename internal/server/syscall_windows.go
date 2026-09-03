//go:build windows
// +build windows

package server

import (
	"bufio"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// syscallKill terminates the process identified by pid. On Windows signals
// are not supported, so SIGTERM and SIGKILL are treated identically:
// OpenProcess + TerminateProcess. A negative pid (process-group semantics
// used on Unix) is accepted for API compatibility — the group concept does
// not exist on Windows, so the absolute pid is terminated.
func syscallKill(pid int, sig syscall.Signal) error {
	if pid < 0 {
		pid = -pid
	}
	if pid == 0 {
		return nil
	}
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		// Process already gone — treat as success.
		if err == windows.ERROR_INVALID_PARAMETER {
			return nil
		}
		return err
	}
	defer windows.CloseHandle(h)
	return windows.TerminateProcess(h, 1)
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

// portOwnerPID returns the PID of a go-magic process ("magic" in its image
// name) that is LISTENING on the given TCP port. Returns 0 when no such
// process owns the port. Non-go-magic listeners are deliberately ignored so
// an unrelated service squatting on 8080/8081 is never killed by the gateway
// auto-restart logic.
func portOwnerPID(port int) int {
	portStr := fmt.Sprintf(":%d", port)
	out, err := exec.Command("netstat", "-ano", "-p", "tcp").Output()
	if err != nil {
		return 0
	}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 5 {
			continue
		}
		// Format: Proto  LocalAddress  ForeignAddress  State  PID
		if !strings.EqualFold(fields[0], "TCP") {
			continue
		}
		if !strings.EqualFold(fields[3], "LISTENING") {
			continue
		}
		if !strings.HasSuffix(fields[1], portStr) {
			continue
		}
		pid, err := strconv.Atoi(fields[4])
		if err != nil || pid <= 0 {
			continue
		}
		if isOurProcessPID(pid) {
			return pid
		}
	}
	return 0
}

// isOurProcessPID reports whether the process with pid is a go-magic
// executable (image name contains "magic").
func isOurProcessPID(pid int) bool {
	out, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/NH", "/FO", "CSV").Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), "magic")
}
