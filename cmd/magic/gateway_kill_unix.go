//go:build !windows
// +build !windows

package main

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// killProcessGroup sends sig to the process group identified by pid.
// On Linux, when the gateway is started with Setpgid+Setsid, killing the
// process group ensures all children (gateway, health server, api server)
// receive the signal and release their ports.
//
// Note: the gateway runs health/API in goroutines (not separate procs) so
// killing the group is mostly defensive, but it does handle the case where
// the gateway has spawned helper processes.
func killProcessGroup(pid int, sig syscall.Signal) error {
	// Negative pid means "process group with that id" in kill(2).
	// Ignore ESRCH (no such process) - that just means it's already dead.
	if err := syscall.Kill(-pid, sig); err != nil && err != syscall.ESRCH {
		return err
	}
	return nil
}

// processAlive returns true if the process with the given pid exists.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Sending signal 0 succeeds only if the process exists and we have
	// permission to signal it.
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return false
	}
	return true
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

// findPidByPort uses /proc/net/tcp (and /proc/net/tcp6) to find the PID
// of the process listening on the given TCP port. Returns 0 if not found.
//
// Port 8080 = 0x1F90, 8081 = 0x1F91 in hex.
func findPidByPort(port int) int {
	portHex := fmt.Sprintf("%04X", port)
	// Scan /proc/net/tcp and /proc/net/tcp6 for LISTEN sockets bound to
	// the given port, then look up the owning PID via /proc/<pid>/fd.
	pids := scanProcNet([]string{"/proc/net/tcp", "/proc/net/tcp6"}, portHex)
	for _, pid := range pids {
		if isOurProcess(pid) {
			return pid
		}
	}
	// Fallback: return the first PID we found, even if we can't verify
	// it's ours - the user can re-run if it's wrong.
	if len(pids) > 0 {
		return pids[0]
	}
	return 0
}

func scanProcNet(paths []string, portHex string) []int {
	seen := map[int]bool{}
	var result []int
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		for _, line := range lines[1:] {
			fields := strings.Fields(line)
			if len(fields) < 10 {
				continue
			}
			// local_address is field 1, e.g. "00000000:1F90"
			local := fields[1]
			// state 0A = LISTEN
			if fields[3] != "0A" {
				continue
			}
			colon := strings.LastIndex(local, ":")
			if colon < 0 {
				continue
			}
			if !strings.EqualFold(local[colon+1:], portHex) {
				continue
			}
			// uid is field 7, inode is field 9
			inode := fields[9]
			pid := findPidByInode(inode)
			if pid > 0 && !seen[pid] {
				seen[pid] = true
				result = append(result, pid)
			}
		}
	}
	return result
}

func findPidByInode(inode string) int {
	procDirs, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	for _, entry := range procDirs {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		fdDir := fmt.Sprintf("/proc/%d/fd", pid)
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			link, err := os.Readlink(fmt.Sprintf("%s/%s", fdDir, fd.Name()))
			if err != nil {
				continue
			}
			if strings.Contains(link, "socket:["+inode+"]") {
				return pid
			}
		}
	}
	return 0
}

// isOurProcess returns true if the process with the given pid appears to
// be a magic gateway (its exe path or comm matches). This is a best-effort
// check used to avoid killing unrelated processes that happen to be
// holding port 8080/8081.
func isOurProcess(pid int) bool {
	if pid <= 0 {
		return false
	}
	comm, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil {
		return false
	}
	name := strings.TrimSpace(string(comm))
	if strings.Contains(strings.ToLower(name), "magic") {
		return true
	}
	exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(exe), "magic")
}
