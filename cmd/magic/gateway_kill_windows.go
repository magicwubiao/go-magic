//go:build windows
// +build windows

package main

import (
	"bufio"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// killProcessGroup sends sig to the process identified by pid.
// On Windows, process groups are managed via Job Objects. We don't
// have a Job Object handle here, so we fall back to terminating the
// process tree via WMI/TaskKill semantics: OpenProcess and call
// TerminateProcess. The gateway runs health/API in goroutines (not
// separate processes), so this is sufficient.
func killProcessGroup(pid int, sig syscall.Signal) error {
	if pid <= 0 {
		return nil
	}
	// We only support termination on Windows; the signal value is
	// ignored. OpenProcess + TerminateProcess is the standard way to
	// kill a process.
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		// If the process is already gone, treat that as success.
		if err == windows.ERROR_INVALID_PARAMETER {
			return nil
		}
		return err
	}
	defer windows.CloseHandle(h)
	if err := windows.TerminateProcess(h, 1); err != nil {
		return err
	}
	return nil
}

// processAlive returns true if the process with the given pid exists and
// has not yet exited. On Windows, signal(0) is not supported, so we use
// OpenProcess + GetExitCodeProcess instead.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	// A process is still alive if its exit code has not been set
	// (still running). STILI_ACTIVE = 0x103 = 259.
	var exitCode uint32
	if err := windows.GetExitCodeProcess(h, &exitCode); err != nil {
		return false
	}
	return exitCode == 259 // STILI_ACTIVE
}

// isPortFree returns true if the given TCP port can be bound on all
// interfaces. We try to bind then immediately close - if we succeed the
// port is free.
func isPortFree(port int) bool {
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

// waitForPortsFree polls isPortFree for both the gateway API (8080) and
// health (8081) ports until they are both free or the timeout expires.
// Returns true if both ports are free.
func waitForPortsFree(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if isPortFree(8080) && isPortFree(8081) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// findPidByPort uses `netstat -ano` to find the PID of the process
// listening on the given TCP port. Returns 0 if not found.
func findPidByPort(port int) int {
	portStr := fmt.Sprintf(":%d", port)
	out, err := exec.Command("netstat", "-ano", "-p", "tcp").Output()
	if err != nil {
		return 0
	}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
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
		if err != nil {
			continue
		}
		// Try to verify it's our process (image name contains "magic")
		if isOurProcessWindows(pid) {
			return pid
		}
		// Fallback: return the first match
		return pid
	}
	return 0
}

// isOurProcessWindows returns true if the process with the given pid has
// an image name containing "magic".
func isOurProcessWindows(pid int) bool {
	out, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/NH", "/FO", "CSV").Output()
	if err != nil {
		return false
	}
	s := strings.ToLower(string(out))
	return strings.Contains(s, "magic")
}

// silence unused import warnings on some toolchains
var _ = windows.GenerateConsoleCtrlEvent
