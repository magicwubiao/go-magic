package server

// 右侧栏 Web 终端的最小后端：不做 PTY（Windows ConPTY 复杂且依赖多），
// 而是维护每个终端会话的持久 cwd，每次 exec 用一次性 shell 进程在 cwd 里
// 执行命令并回传合并输出，再从输出尾部解析出新 cwd 实现 "cd" 的跨命令持久。
// 无持久进程意味着没有僵尸进程/连接泄漏问题，也天然支持多标签互不干扰。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// terminalExecTimeout 限制单条命令最长执行时间，防止 UI 端挂死。
const terminalExecTimeout = 120 * time.Second

// terminalSession 是一个 Web 终端实例的持久状态。
type terminalSession struct {
	mu  sync.Mutex
	id  string
	cwd string
}

// terminalFor 返回（或创建）指定 id 的终端会话。终端只保存 cwd，
// 不绑定生命周期敏感的资源，因此不做 TTL 清理；浏览器关掉侧栏时
// 会调 DELETE 主动释放，遗留条目也只是几十字节的内存。
func (s *Server) terminalFor(id string) *terminalSession {
	s.terminalMu.Lock()
	defer s.terminalMu.Unlock()
	if s.terminalSessions == nil {
		s.terminalSessions = make(map[string]*terminalSession)
	}
	return s.terminalSessions[id]
}

func (s *Server) terminalCreate(cwd string) *terminalSession {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	id := "term_" + hex.EncodeToString(buf)

	s.terminalMu.Lock()
	defer s.terminalMu.Unlock()
	if s.terminalSessions == nil {
		s.terminalSessions = make(map[string]*terminalSession)
	}
	ts := &terminalSession{id: id, cwd: cwd}
	s.terminalSessions[id] = ts
	return ts
}

// handleTerminal 处理终端会话的创建（POST）/查询（GET）/释放（DELETE）。
func (s *Server) handleTerminal(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req struct {
			SessionID string `json:"session_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		cwd, err := s.resolveFSPath("", req.SessionID)
		if err != nil {
			jsonResponse(w, map[string]interface{}{"error": err.Error()})
			return
		}
		ts := s.terminalCreate(cwd)
		jsonResponse(w, map[string]interface{}{"id": ts.id, "cwd": cwd})

	case http.MethodGet:
		id := r.URL.Query().Get("id")
		ts := s.terminalFor(id)
		if ts == nil {
			jsonResponse(w, map[string]interface{}{"error": "terminal not found"})
			return
		}
		ts.mu.Lock()
		cwd := ts.cwd
		ts.mu.Unlock()
		jsonResponse(w, map[string]interface{}{"id": ts.id, "cwd": cwd})

	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		s.terminalMu.Lock()
		delete(s.terminalSessions, id)
		s.terminalMu.Unlock()
		jsonResponse(w, map[string]interface{}{"ok": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleTerminalExec 在终端会话的当前 cwd 里执行一条命令。
// 输出尾部追加一条探测命令（cd/pwd），从输出末行解析出新 cwd，
// 让 "cd sub && ls" 这类组合在后续命令中生效。
func (s *Server) handleTerminalExec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID      string `json:"id"`
		Command string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, map[string]interface{}{"error": "invalid body"})
		return
	}
	ts := s.terminalFor(req.ID)
	if ts == nil {
		jsonResponse(w, map[string]interface{}{"error": "terminal not found"})
		return
	}
	cmdline := strings.TrimSpace(req.Command)
	if cmdline == "" {
		jsonResponse(w, map[string]interface{}{"error": "command is required"})
		return
	}

	ts.mu.Lock()
	cwd := ts.cwd
	ts.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), terminalExecTimeout)
	defer cancel()

	var execCmd *exec.Cmd
	var fullCmd string
	if runtime.GOOS == "windows" {
		// cmd /S /C "<cmd> & cd"：即使前半段失败也执行 cd 探测；
		// /S 保证含引号的整串按单引号参数处理。
		fullCmd = cmdline + " & cd"
		execCmd = exec.CommandContext(ctx, "cmd", "/S", "/C", fullCmd)
	} else {
		fullCmd = "{ " + cmdline + " ; } ; pwd"
		execCmd = exec.CommandContext(ctx, "sh", "-c", fullCmd)
	}
	execCmd.Dir = cwd

	// 中文 Windows 的控制台代码页是 GBK（cp936）：命令行输入要按 GBK 编码
	// 写入，输出也要从 GBK 解码回 UTF-8，否则双向都是乱码。非 Windows 的
	// sh 默认 UTF-8，不需要转。
	utf8ToConsole := func(s string) string { return s }
	consoleToUTF8 := func(b []byte) string { return string(b) }
	if runtime.GOOS == "windows" {
		enc := simplifiedchinese.GBK.NewEncoder()
		dec := simplifiedchinese.GBK.NewDecoder()
		utf8ToConsole = func(s string) string {
			out, err := enc.String(s)
			if err != nil {
				return s
			}
			return out
		}
		consoleToUTF8 = func(b []byte) string {
			out, err := dec.Bytes(b)
			if err != nil {
				// 严格解码失败（混入非 GBK 字节）时退回容错模式逐字节转，
				// 至少保证可显示字符不整段丢失。
				out, _ = simplifiedchinese.GBK.NewDecoder().Bytes(b)
				return string(out)
			}
			return string(out)
		}
		// 命令行参数按 GBK 传入。
		fullCmd = utf8ToConsole(fullCmd)
	}

	start := time.Now()
	output, err := execCmd.CombinedOutput()
	duration := time.Since(start)

	decodedOut := consoleToUTF8(output)

	exitCode := 0
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			exitCode = 124
			decodedOut += "\n(Error: command timed out after 120s)"
		} else if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			exitCode = 1
			decodedOut += "\n(Error: " + err.Error() + ")"
		}
	}

	newCwd := parseTerminalCwd(decodedOut, runtime.GOOS == "windows")
	if newCwd != "" {
		ts.mu.Lock()
		ts.cwd = newCwd
		ts.mu.Unlock()
		cwd = newCwd
		// 探测命令（cmd & cd / sh ; pwd）打印的路径是内部实现细节，
		// 不该出现在用户看到的输出里——剥掉尾部这一行再回传。
		decodedOut = strings.TrimRight(decodedOut, "\r\n")
		if i := strings.LastIndexByte(decodedOut, '\n'); i >= 0 {
			decodedOut = decodedOut[:i+1]
		} else {
			decodedOut = ""
		}
	}

	jsonResponse(w, map[string]interface{}{
		"output":      decodedOut,
		"exit_code":   exitCode,
		"cwd":         cwd,
		"duration_ms": duration.Milliseconds(),
	})
}

// parseTerminalCwd 从合并输出中解析尾部探测命令的 cwd。
// 只认「最后一行是绝对路径」的情况；其余情况返回空串表示 cwd 未变
// （沿用旧值，UI 提示符保持不变）。
func parseTerminalCwd(out string, isWindows bool) string {
	lines := strings.Split(strings.TrimRight(out, "\r\n"), "\n")
	if len(lines) == 0 {
		return ""
	}
	last := strings.TrimSpace(lines[len(lines)-1])
	last = strings.Trim(last, `"`)
	if last == "" {
		return ""
	}
	if isWindows {
		// "D:\a\b" / "D:/a/b" 都算绝对路径；cd 在失败盘符下会打印
		// 错误信息而非路径，path.IsAbs 已能区分。
		if filepath.IsAbs(strings.ReplaceAll(last, "/", "\\")) {
			return filepath.Clean(last)
		}
		return ""
	}
	if strings.HasPrefix(last, "/") {
		return filepath.Clean(last)
	}
	return ""
}
