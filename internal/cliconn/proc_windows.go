//go:build windows

package cliconn

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// createNoWindow 让子进程不弹控制台窗口。
//
// 服务进程（gateway/TUI 之外的托盘版）拉起 cmd 时不加这个标志，用户桌面会
// 每调一次工具闪一个黑框——CLI 连接器的调用频率远比手动执行高。
const createNoWindow = 0x08000000

func setProcAttrs(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}

// procGuard 持有本次调用的进程树句柄（Windows 上是 job object）。
//
// 加锁是因为 Kill 由 context.AfterFunc 在另一个 goroutine 上调用，可能和
// Wait 返回后的 Close 撞上：stopWatch() 返回 false 只说明"已经开始执行"，
// 不保证它已经跑完。
type procGuard struct {
	mu   sync.Mutex
	job  windows.Handle
	proc *os.Process
}

// startCommand 启动子进程并把它放进一个 job object。
//
// 为什么不能只靠 exec.CommandContext：它只 Kill 直接子进程。而 checkExecutable
// 在拒绝 .bat 时明确建议用户改用 `cmd /c foo.bat`，这类包装下的孙进程才真正
// 干活——超时把 cmd.exe 杀掉，孙进程会继续跑到自己结束（构建、同步类命令可能
// 跑十几分钟，还会一直占着输出管道，让 Wait 迟迟不返回）。
//
// KILL_ON_JOB_CLOSE 让"守卫关闭"等价于"整棵树结束"：正常结束时 Close 顺手收掉
// 残留的孙进程，超时时 Kill 直接 TerminateJobObject。
//
// 建 job 失败不是致命错误：退化成"只杀直接子进程"（与改造前一致）。宁可少一层
// 保护，也不能让命令根本跑不起来。
func startCommand(cmd *exec.Cmd) (*procGuard, error) {
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	g := &procGuard{proc: cmd.Process}

	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return g, nil
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return g, nil
	}

	h, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false, uint32(cmd.Process.Pid))
	if err != nil {
		windows.CloseHandle(job)
		return g, nil
	}
	defer windows.CloseHandle(h)

	// 进程已经属于别的 job 时可能失败（Win8+ 支持嵌套 job）；失败就退化成
	// 直接杀进程，不影响命令本身。
	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		windows.CloseHandle(job)
		return g, nil
	}
	g.job = job
	return g, nil
}

// Kill 强杀整棵进程树。
func (g *procGuard) Kill() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.job != 0 {
		_ = windows.TerminateJobObject(g.job, 1)
		return
	}
	if g.proc != nil {
		_ = g.proc.Kill()
	}
}

// Close 释放 job 句柄。KILL_ON_JOB_CLOSE 意味着这会顺带收掉仍在运行的孙进程。
func (g *procGuard) Close() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.job == 0 {
		return
	}
	windows.CloseHandle(g.job)
	g.job = 0
}

// normalizeEnvKey 统一环境变量名的大小写：Windows 的变量名大小写不敏感，
// `Path` 与 `PATH` 是同一个变量，去重时必须归一，否则会同时塞两条进去。
func normalizeEnvKey(key string) string { return strings.ToUpper(key) }

// isBatchScript 报告 command 是否是 .bat/.cmd（不能直接 CreateProcess）。
func isBatchScript(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".bat", ".cmd":
		return true
	}
	return false
}
