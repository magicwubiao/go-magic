package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/magicwubiao/go-magic/pkg/config"
)

// server 子命令的崩溃守卫。
//
// 背景（2026-11-13 排查）：桌面外壳直接拉起 `go-magic.exe server`，且不显示、
// 不落盘该子进程的 stderr —— runtime panic / 致命错误（含 OOM）直接丢失，
// 服务"戛然而止"时没有任何证据可查。多个会话并发跑 git 提交等重负载时
// 尤其需要这份证据。
//
// 方案：server 命令自我 re-exec 一层守卫进程：
//   - 内层进程 stderr 重定向到 logs/crash_<ts>.log（捕获 panic 栈与致命错误）；
//   - stdout 透传给父进程（保持现有终端输出不变）；
//   - 内层异常退出（非 0）时自动重启，10 分钟窗口内最多 3 次，超过则放弃
//     并以原退出码退出（避免把死循环包装成"永远健康"）；
//   - 每次启动/退出都追加记录到 logs/server_guard.log，事后可还原时间线；
//   - unix 下转发 SIGINT/SIGTERM 给内层并等待其退出（kill 外层不留孤儿）；
//     windows 下控制台 Ctrl+C 会同时送达同组进程、外壳按进程树结束，
//     守卫进程不需要也不能抢信号（抢了反而改变默认行为）。
//
// 内层通过 MAGIC_SERVER_GUARD_CHILD=1 标记，避免无限递归。

const serverGuardChildEnv = "MAGIC_SERVER_GUARD_CHILD"

// guard 快速重启限流参数：10 分钟窗口内最多 3 次异常重启。
const guardRestartWindow = 10 * time.Minute
const guardRestartLimit = 3

// guardShutdownFlag 在 unix 守卫转发过 SIGINT/SIGTERM 后置 true，
// 表示内层的非零退出是我们自己触发的关停，不该重启。
var guardShutdownFlag atomic.Bool

// isServerGuardChild 判断当前进程是否处于守卫的内层。
func isServerGuardChild() bool {
	return os.Getenv(serverGuardChildEnv) == "1"
}

// runServerGuarded 在外层守卫进程里循环拉起内层 server。
func runServerGuarded() {
	logDir := filepath.Join(config.GetMagicHome(), "logs")
	_ = os.MkdirAll(logDir, 0755)
	guardLogPath := filepath.Join(logDir, "server_guard.log")

	exe, err := os.Executable()
	if err != nil {
		// 连自身路径都拿不到，守卫没有意义，直接按原逻辑跑。
		fmt.Fprintf(os.Stderr, "server guard: cannot resolve executable: %v, running without guard\n", err)
		runServerInner()
		return
	}

	guardAppendf(guardLogPath, "=== guard start pid=%d exe=%s args=%v ===\n", os.Getpid(), exe, os.Args[1:])

	// unix：一次性装好信号通道，每轮转发给当前内层进程。
	var sigCh chan os.Signal
	if runtime.GOOS != "windows" {
		sigCh = make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	}

	// unix：单个转发 goroutine + 当前内层进程指针，避免每轮残留旧 goroutine
	// 去抢同一个信号通道。
	var currentCmd atomic.Pointer[exec.Cmd]
	if sigCh != nil {
		go func() {
			for s := range sigCh {
				guardShutdownFlag.Store(true)
				if c := currentCmd.Load(); c != nil && c.Process != nil {
					_ = c.Process.Signal(s)
				}
			}
		}()
	}

	var restarts []time.Time
	for attempt := 0; ; attempt++ {
		_ = attempt
		ts := time.Now().Format("2006-01-02_15-04-05")
		crashPath := filepath.Join(logDir, "crash_"+ts+".log")
		crashFile, crashErr := os.OpenFile(crashPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if crashErr == nil {
			fmt.Fprintf(crashFile, "=== server session start %s (stderr redirected for panic capture) ===\n",
				time.Now().Format("2006-01-02 15:04:05"))
		}
		// crash 文件打不开时退化为透传 stderr：守卫不能让可观测性更差。

		cmd := exec.Command(exe, os.Args[1:]...)
		cmd.Stdout = os.Stdout
		if crashFile != nil {
			cmd.Stderr = crashFile
		} else {
			cmd.Stderr = os.Stderr
		}
		cmd.Env = append(os.Environ(), serverGuardChildEnv+"=1")
		currentCmd.Store(cmd)

		if err := cmd.Start(); err != nil {
			guardAppendf(guardLogPath, "[%s] guard: start inner failed: %v\n",
				time.Now().Format("2006-01-02 15:04:05"), err)
			fmt.Fprintf(os.Stderr, "server guard: cannot start server: %v\n", err)
			os.Exit(1)
		}

		start := time.Now()
		runErr := cmd.Wait()
		if crashFile != nil {
			fmt.Fprintf(crashFile, "=== server session exit %s after %s: %v ===\n",
				time.Now().Format("2006-01-02 15:04:05"), time.Since(start).Round(time.Second), runErr)
			_ = crashFile.Close()
		}

		code := exitCodeOf(runErr)
		intentional := code == 0 || guardShutdownFlag.Load()

		guardAppendf(guardLogPath, "[%s] server exit after %s code=%d err=%v intentional=%t\n",
			time.Now().Format("2006-01-02 15:04:05"), time.Since(start).Round(time.Second), code, runErr, intentional)

		// 正常退出 / 收到关停信号：不重启，把退出码原样交给上层。
		if intentional {
			guardAppendf(guardLogPath, "=== guard stop pid=%d ===\n", os.Getpid())
			os.Exit(code)
		}

		// 快速重启限流：窗口内超过上限就放弃，宁可让外壳看到退出。
		now := time.Now()
		kept := restarts[:0]
		for _, t := range restarts {
			if now.Sub(t) < guardRestartWindow {
				kept = append(kept, t)
			}
		}
		restarts = kept
		if len(restarts) >= guardRestartLimit {
			guardAppendf(guardLogPath, "=== guard give up pid=%d: %d abnormal exits within %s, exiting with code %d ===\n",
				os.Getpid(), len(restarts), guardRestartWindow, code)
			fmt.Fprintf(os.Stderr, "server guard: too many abnormal exits (%d in %s), giving up\n",
				len(restarts), guardRestartWindow)
			os.Exit(code)
		}
		restarts = append(restarts, now)

		fmt.Fprintf(os.Stderr, "server guard: server exited abnormally (code=%d), restarting in 1s (attempt %d/%d)\n",
			code, len(restarts), guardRestartLimit)
		time.Sleep(1 * time.Second)
	}
}

func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}

// guardAppendf 追加一行守卫日志。append-only 日志，单写者，无需原子替换。
func guardAppendf(path, format string, args ...interface{}) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, format, args...)
}
