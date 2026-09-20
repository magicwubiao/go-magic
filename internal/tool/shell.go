package tool

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"sync"
)

// Shell resolution for tools that take a *command string* instead of an argv
// slice.
//
// Agent-authored commands are shell one-liners, so they must run through a
// shell. Every such call site used to hard-code `bash -c`, which made the whole
// command surface unusable on a Windows host without Git Bash — `process run`
// failed with "failed to start process: exec: bash: executable file not found
// in %PATH%", while `execute_command` (which does branch on GOOS) worked fine
// on the same machine. The defect was per-tool, so resolution now lives here
// and all tools share one implementation.
//
// On Windows the chosen shell matches execute_command / BasicSandbox
// (`powershell -Command`) so a given command string behaves identically no
// matter which tool receives it.

type shellSpec struct {
	Binary string
	Args   []string
}

var (
	shellOnce   sync.Once
	shellCached shellSpec
)

// resolveShell picks the shell for this host.
//
// Windows preference order: powershell -> pwsh -> cmd.exe -> bash.
//   - bash comes last on purpose: on Windows `bash` is normally the WSL shim at
//     System32\bash.exe, which runs a Linux shell that cannot see the Windows
//     working directory and rewrites paths into /mnt/c/... . Falling back to it
//     is better than failing outright, but it must never win over a native
//     shell.
//
// POSIX preference order: bash -> sh.
func resolveShell() shellSpec {
	if runtime.GOOS == "windows" {
		for _, name := range []string{"powershell", "pwsh"} {
			if p, err := exec.LookPath(name); err == nil {
				return shellSpec{Binary: p, Args: []string{"-Command"}}
			}
		}
		if p, err := exec.LookPath("cmd.exe"); err == nil {
			return shellSpec{Binary: p, Args: []string{"/c"}}
		}
		if p, err := exec.LookPath("bash"); err == nil {
			return shellSpec{Binary: p, Args: []string{"-c"}}
		}
		return shellSpec{}
	}

	for _, name := range []string{"bash", "sh"} {
		if p, err := exec.LookPath(name); err == nil {
			return shellSpec{Binary: p, Args: []string{"-c"}}
		}
	}
	return shellSpec{}
}

// hostShell returns the cached shell resolution (probing PATH once per process).
func hostShell() shellSpec {
	shellOnce.Do(func() { shellCached = resolveShell() })
	return shellCached
}

// shellCommand builds an exec.Cmd that runs command through the host shell.
//
// Pass a nil ctx for work that must outlive the tool call (e.g. the process
// tool's background jobs); exec.Command is used in that case.
func shellCommand(ctx context.Context, command string) (*exec.Cmd, error) {
	sh := hostShell()
	if sh.Binary == "" {
		return nil, fmt.Errorf(
			"no usable shell found on PATH: expected one of powershell/pwsh/cmd.exe/bash on Windows, bash/sh elsewhere")
	}
	args := append(append([]string{}, sh.Args...), command)
	if ctx == nil {
		return exec.Command(sh.Binary, args...), nil
	}
	return exec.CommandContext(ctx, sh.Binary, args...), nil
}

// shellName reports the shell that shellCommand would use, for error messages
// and result metadata. It never returns an empty string.
func shellName() string {
	sh := hostShell()
	if sh.Binary == "" {
		return "none"
	}
	return sh.Binary
}
