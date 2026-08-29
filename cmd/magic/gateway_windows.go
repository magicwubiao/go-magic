//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000 // CREATE_NO_WINDOW

func setSysProcAttr(cmd *exec.Cmd) {
	// Windows does not support Setpgid. Hide the child console window so
	// launching the gateway subprocess doesn't flash a black cmd box.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}
