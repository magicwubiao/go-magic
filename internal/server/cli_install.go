package server

// CLI 一键安装：目录里已经写好了各平台的安装命令，过去只让用户复制到终端。
//
// 边界（刻意收紧，这是唯一一处服务端替用户执行系统安装的地方）：
//   - 只执行**内置目录**登记的安装命令：请求体只能传目录 id 或插件名，
//     不接受任何命令字符串——否则这就是一个任意命令执行接口；
//   - 需要 sudo 的命令不代跑：服务端没有 TTY，必然卡在密码提示；
//   - 安装是长任务（winget / brew 可能几分钟），走"提交任务 + 轮询"，
//     而不是挂着一个 HTTP 请求等，避免中间层超时把用户晾在转圈里。

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/internal/agentplugin"
	"github.com/magicwubiao/go-magic/pkg/clicatalog"
)

const (
	// cliInstallTimeout 是整条安装链路的硬上限。
	cliInstallTimeout = 10 * time.Minute
	// cliInstallOutputLimit 是回显给前端的输出上限（超出后追加截断提示）：
	// winget/brew 的进度输出可能几百 KB，全塞进响应没有意义。
	cliInstallOutputLimit = 64 << 10
	// cliInstallJobTTL 是已完成任务的保留时间（供前端最后一次轮询取结果）。
	cliInstallJobTTL = 30 * time.Minute
)

// 任务状态。
const (
	cliInstallRunning = "running"
	cliInstallDone    = "done"
	cliInstallFailed  = "failed"
)

// cliInstallJob 是一次安装任务。
//
// 字段全部走 j.mu 保护：输出是子进程边跑边追加的，轮询接口并发读。
type cliInstallJob struct {
	mu          sync.Mutex
	id          string
	catalogID   string
	plugin      string
	commands    []string
	state       string
	output      bytes.Buffer
	truncated   bool
	binary      string
	binaryFound bool
	errText     string
	startedAt   time.Time
	finishedAt  time.Time
}

// snapshot 生成给前端的状态视图。
func (j *cliInstallJob) snapshot() map[string]any {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := map[string]any{
		"id":           j.id,
		"catalog_id":   j.catalogID,
		"state":        j.state,
		"commands":     j.commands,
		"output":       j.output.String(),
		"binary":       j.binary,
		"binary_found": j.binaryFound,
		"started_at":   j.startedAt.Format(time.RFC3339),
	}
	if j.plugin != "" {
		out["plugin"] = j.plugin
	}
	if j.errText != "" {
		out["error"] = j.errText
	}
	if !j.finishedAt.IsZero() {
		out["finished_at"] = j.finishedAt.Format(time.RFC3339)
	}
	if j.truncated {
		out["truncated"] = true
	}
	return out
}

// appendOutput 追加子进程输出，超过上限后只置截断标记。
func (j *cliInstallJob) appendOutput(p []byte) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.truncated {
		return
	}
	room := cliInstallOutputLimit - j.output.Len()
	if room <= 0 {
		j.truncated = true
		return
	}
	if len(p) > room {
		j.output.Write(p[:room])
		j.truncated = true
		return
	}
	j.output.Write(p)
}

// installJobWriter 把子进程输出接到 job 上。
type installJobWriter struct{ job *cliInstallJob }

func (w installJobWriter) Write(p []byte) (int, error) {
	w.job.appendOutput(p)
	return len(p), nil
}

// newInstallJobID 生成任务 id（16 位十六进制，够用且不暴露顺序）。
func newInstallJobID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("job-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// addInstallJob 登记任务，并顺手清理过期任务。
func (s *Server) addInstallJob(job *cliInstallJob) {
	s.cliInstallMu.Lock()
	defer s.cliInstallMu.Unlock()
	if s.cliInstallJobs == nil {
		s.cliInstallJobs = map[string]*cliInstallJob{}
	}
	cutoff := time.Now().Add(-cliInstallJobTTL)
	for id, old := range s.cliInstallJobs {
		old.mu.Lock()
		finished := old.finishedAt
		old.mu.Unlock()
		if !finished.IsZero() && finished.Before(cutoff) {
			delete(s.cliInstallJobs, id)
		}
	}
	s.cliInstallJobs[job.id] = job
}

// runningInstallJob 返回同一条目的在途任务（同一包不并发装两次）。
func (s *Server) runningInstallJob(catalogID string) *cliInstallJob {
	s.cliInstallMu.Lock()
	defer s.cliInstallMu.Unlock()
	for _, job := range s.cliInstallJobs {
		job.mu.Lock()
		running := job.state == cliInstallRunning && job.catalogID == catalogID
		job.mu.Unlock()
		if running {
			return job
		}
	}
	return nil
}

// getInstallJob 按 id 取任务。
func (s *Server) getInstallJob(id string) *cliInstallJob {
	s.cliInstallMu.Lock()
	defer s.cliInstallMu.Unlock()
	return s.cliInstallJobs[id]
}

// handleCLIInstallStart 提交一次安装任务。
//
//	POST /api/agent-plugins/cli/install
//	body: {"id":"gh"}            // 目录条目（kind 缺省为 cli）
//	      {"plugin":"cli-gh"}    // 已安装插件：从 extensions 反查目录条目
func (s *Server) handleCLIInstallStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Kind   string `json:"kind"`
		ID     string `json:"id"`
		Plugin string `json:"plugin"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid json body: "+err.Error())
		return
	}

	kind := strings.TrimSpace(body.Kind)
	if kind == "" {
		kind = agentplugin.CatalogKindCLI
	}
	if kind != agentplugin.CatalogKindCLI {
		jsonError(w, http.StatusBadRequest, "only CLI catalog entries can be installed")
		return
	}

	catalogID := strings.TrimSpace(body.ID)
	pluginName := strings.TrimSpace(body.Plugin)
	if pluginName != "" {
		s.acquireServerMu("")
		mp := s.agentPlugins[pluginName]
		s.releaseServerMu()
		if mp == nil || mp.Plugin == nil {
			jsonError(w, http.StatusNotFound, "plugin not found")
			return
		}
		k, id, ok := agentplugin.CatalogSource(mp.Plugin)
		if !ok || k != agentplugin.CatalogKindCLI {
			jsonError(w, http.StatusBadRequest, "plugin was not installed from the CLI catalog")
			return
		}
		if catalogID == "" {
			catalogID = id
		}
	}
	if catalogID == "" {
		jsonError(w, http.StatusBadRequest, "missing id or plugin")
		return
	}

	entry, ok := clicatalog.Get(catalogID)
	if !ok {
		jsonError(w, http.StatusNotFound, "unknown catalog entry: "+catalogID)
		return
	}

	// 同一包已在装：直接把任务回给前端继续轮询，不重复拉起包管理器。
	if running := s.runningInstallJob(entry.ID); running != nil {
		jsonResponse(w, map[string]any{"ok": true, "reused": true, "job": running.snapshot()})
		return
	}

	commands, blocked := installCommandsFor(entry)
	if len(commands) == 0 {
		if blocked != "" {
			jsonError(w, http.StatusBadRequest, blocked)
			return
		}
		jsonError(w, http.StatusBadRequest,
			fmt.Sprintf("catalog entry %q has no install command for %s", entry.ID, runtime.GOOS))
		return
	}

	job := &cliInstallJob{
		id:        newInstallJobID(),
		catalogID: entry.ID,
		plugin:    pluginName,
		commands:  commands,
		state:     cliInstallRunning,
		binary:    entry.Connector.Command,
		startedAt: time.Now(),
	}
	s.addInstallJob(job)
	go s.runCLIInstall(job)

	jsonResponse(w, map[string]any{"ok": true, "job": job.snapshot()})
}

// handleCLIInstallStatus 轮询一次任务状态。
//
//	GET /api/agent-plugins/cli/install/{id}
func (s *Server) handleCLIInstallStatus(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	job := s.getInstallJob(strings.TrimSpace(id))
	if job == nil {
		jsonError(w, http.StatusNotFound, "install job not found")
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "job": job.snapshot()})
}

// installCommandsFor 取出当前平台要执行的安装命令，并过滤掉需要 sudo 的。
//
// 返回 (可执行命令, 全部被拒时的说明)。目录里同一平台常写多条互为备选的
// 命令（apt / dnf），按顺序试，第一条成功就停。
func installCommandsFor(entry clicatalog.Entry) ([]string, string) {
	raw := entry.Install.CommandsFor(runtime.GOOS)
	var out []string
	var rejected []string
	for _, c := range raw {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		// sudo 会等密码，服务端没有 TTY，跑了只会挂住十分钟。
		if strings.Contains(c, "sudo ") || strings.HasPrefix(c, "sudo") {
			rejected = append(rejected, c)
			continue
		}
		out = append(out, unattendedCommand(c))
	}
	if len(out) == 0 && len(rejected) > 0 {
		return nil, "需要管理员权限，请在自己的终端执行：" + strings.Join(rejected, " / ")
	}
	return out, ""
}

// unattendedCommand 给 winget 补上"接受协议"开关。
//
// winget 首次安装会停下来问包协议/源协议，没有 TTY 时直接失败；这两个开关
// 是官方提供的无人值守入口，语义等同于用户点了"同意"。
func unattendedCommand(command string) string {
	if runtime.GOOS != "windows" {
		return command
	}
	if !strings.HasPrefix(command, "winget ") {
		return command
	}
	// 已经带过开关的不重复追加。
	if strings.Contains(command, "--accept-package-agreements") {
		return command
	}
	return command + " --accept-package-agreements --accept-source-agreements"
}

// runCLIInstall 顺序执行安装命令，第一条成功即停。
func (s *Server) runCLIInstall(job *cliInstallJob) {
	ctx, cancel := context.WithTimeout(context.Background(), cliInstallTimeout)
	defer cancel()

	okAny := false
	var lastErr error
	for _, command := range job.commands {
		job.appendOutput([]byte("$ " + command + "\n"))
		err := runInstallCommand(ctx, command, job)
		if err == nil {
			okAny = true
			break
		}
		lastErr = err
		job.appendOutput([]byte(fmt.Sprintf("\n[exit] %v\n", err)))
	}

	// 安装完成立刻失效缓存：否则前端刷新时还会读到"命令不存在"。
	agentplugin.InvalidateCommandCache()
	found := agentplugin.CommandAvailable(job.binary)

	job.mu.Lock()
	job.binaryFound = found
	job.finishedAt = time.Now()
	switch {
	case okAny:
		job.state = cliInstallDone
		if !found {
			// 命令跑成功了但 PATH 里还没有：常见于需要重开终端的安装器
			// （Windows 的 PATH 是进程启动时快照的）。
			job.errText = "installed but command not found in PATH yet; a restart may be required"
		}
	case lastErr != nil:
		job.state = cliInstallFailed
		job.errText = lastErr.Error()
	default:
		job.state = cliInstallFailed
		job.errText = "no install command was executed"
	}
	job.mu.Unlock()
}

// runInstallCommand 跑一条安装命令，输出实时写进任务。
func runInstallCommand(ctx context.Context, command string, job *cliInstallJob) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/c", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	w := installJobWriter{job: job}
	cmd.Stdout = w
	cmd.Stderr = w
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Wait()
}
