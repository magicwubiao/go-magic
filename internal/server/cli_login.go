package server

// CLI 登录的"委托模式"：由服务端代跑 CLI 自己的登录命令。
//
// 背景：很多 CLI 的凭据只能由它自己写入（wrangler login、vercel login 会写
// ~/.config 下的登录态），外部无法伪造一枚 token 塞进去。过去只能让用户
// 复制一条命令去终端跑——对不熟悉终端的用户，这一步就是天花板。
//
// 边界（与 cli_install.go 同一套纪律，甚至更严）：
//   - 只执行**内置目录**登记的 LoginExec argv：请求体只能传目录 id，
//     不接受任何命令字符串，避免变成任意命令执行入口；
//   - 命令打印的授权 URL/短码会被抓出来展示，用户只需在浏览器点"允许"；
//   - 交互式提问（"Press Enter" / "用哪个账号登录"）通过 stdin 转发接口
//     原样写进去——不解析、不改写，因为各家措辞完全不可预测；
//   - 流程在 goroutine 里跑，状态用轮询读，避免一个 HTTP 请求挂几分钟。

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/clicatalog"
)

const (
	// cliLoginTimeout 是单次登录的上限。设备码通常 5-15 分钟内有效，
	// 超过这个时间还没批就是没人批，继续挂着只会留一个僵尸进程。
	cliLoginTimeout = 10 * time.Minute
	// cliLoginOutputLimit 是回显上限：登录命令的输出只有几行，
	// 留 16KB 已经足够容纳报错栈。
	cliLoginOutputLimit = 16 << 10
	// cliLoginTTL 是结束后会话的保留时间（供前端最后一次轮询取结果）。
	cliLoginTTL = 30 * time.Minute
	// cliLoginCheckInterval 是"进程还在跑但已经登录成功"的探测间隔。
	// 有些 CLI 授权完不退出（继续守着回调），只能靠就绪检查判完成。
	cliLoginCheckInterval = 3 * time.Second
)

// CLI 登录会话状态。
const (
	cliLoginRunning = "running"
	cliLoginDone    = "done"
	cliLoginFailed  = "failed"
	cliLoginCancel  = "cancelled"
)

var (
	// cliLoginURLRe 抓授权页地址。尾部排除标点，避免把句子里的逗号喝进 URL。
	cliLoginURLRe = regexp.MustCompile(`https?://[^\s"'<>()\[\]，。；！]+`)
	// cliLoginCodeRe 抓设备码：优先"code"字样后面的短码（GH-XXXX / ABCD-EFGH），
	// 其次任何形如 ABCD-EFGH 的短码。
	cliLoginCodeRe     = regexp.MustCompile(`(?i)(?:code|代码)[^A-Za-z0-9]{0,6}([A-Z0-9]{4,8}(?:-[A-Z0-9]{4,8})?)`)
	cliLoginCodeBareRe = regexp.MustCompile(`\b([A-Z0-9]{4}-[A-Z0-9]{4})\b`)
)

// cliLoginSession 是一次委托登录。
//
// 字段全部走 mu：子进程边跑边写输出，轮询接口并发读；stdin 也从另一个
// goroutine 写。
type cliLoginSession struct {
	mu         sync.Mutex
	id         string
	catalogID  string
	argv       []string
	state      string
	output     bytes.Buffer
	truncated  bool
	authURL    string
	userCode   string
	errText    string
	checkMsg   string
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	startedAt  time.Time
	finishedAt time.Time
}

// snapshot 生成给前端的状态视图。
func (s *cliLoginSession) snapshot() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]any{
		"id":         s.id,
		"catalog_id": s.catalogID,
		"state":      s.state,
		"command":    strings.Join(s.argv, " "),
		"output":     s.output.String(),
		"started_at": s.startedAt.Format(time.RFC3339),
	}
	if s.authURL != "" {
		out["auth_url"] = s.authURL
	}
	if s.userCode != "" {
		out["user_code"] = s.userCode
	}
	if s.errText != "" {
		out["error"] = s.errText
	}
	if s.checkMsg != "" {
		out["check_message"] = s.checkMsg
	}
	if s.truncated {
		out["truncated"] = true
	}
	if !s.finishedAt.IsZero() {
		out["finished_at"] = s.finishedAt.Format(time.RFC3339)
	}
	return out
}

// appendOutput 追加输出并顺手解析授权 URL / 短码。
func (s *cliLoginSession) appendOutput(p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.truncated {
		room := cliLoginOutputLimit - s.output.Len()
		if room <= 0 {
			s.truncated = true
		} else if len(p) > room {
			s.output.Write(p[:room])
			s.truncated = true
		} else {
			s.output.Write(p)
		}
	}
	s.parseLocked(s.output.String())
}

// parseLocked 从累计输出里挑最新的 URL 与短码。调用方必须持有 s.mu。
func (s *cliLoginSession) parseLocked(text string) {
	if m := cliLoginURLRe.FindAllString(text, -1); len(m) > 0 {
		s.authURL = m[len(m)-1]
	}
	if m := cliLoginCodeRe.FindAllStringSubmatch(text, -1); len(m) > 0 {
		s.userCode = strings.ToUpper(m[len(m)-1][1])
		return
	}
	if m := cliLoginCodeBareRe.FindAllStringSubmatch(text, -1); len(m) > 0 {
		s.userCode = strings.ToUpper(m[len(m)-1][1])
	}
}

// newCLILoginID 生成会话 id。
func newCLILoginID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("login-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// addLoginSession 登记会话并清理过期项。
func (s *Server) addLoginSession(sess *cliLoginSession) {
	s.cliLoginMu.Lock()
	defer s.cliLoginMu.Unlock()
	if s.cliLoginSessions == nil {
		s.cliLoginSessions = map[string]*cliLoginSession{}
	}
	cutoff := time.Now().Add(-cliLoginTTL)
	for id, old := range s.cliLoginSessions {
		old.mu.Lock()
		finished := old.finishedAt
		old.mu.Unlock()
		if !finished.IsZero() && finished.Before(cutoff) {
			delete(s.cliLoginSessions, id)
		}
	}
	s.cliLoginSessions[sess.id] = sess
}

// getLoginSession 按 id 取会话。
func (s *Server) getLoginSession(id string) *cliLoginSession {
	s.cliLoginMu.Lock()
	defer s.cliLoginMu.Unlock()
	return s.cliLoginSessions[id]
}

// runningLoginSession 返回同一条目的在途会话（同一流程不并发跑两份）。
func (s *Server) runningLoginSession(catalogID string) *cliLoginSession {
	s.cliLoginMu.Lock()
	defer s.cliLoginMu.Unlock()
	for _, sess := range s.cliLoginSessions {
		sess.mu.Lock()
		running := sess.state == cliLoginRunning && sess.catalogID == catalogID
		sess.mu.Unlock()
		if running {
			return sess
		}
	}
	return nil
}

// handleCLILoginStart 开始（或复用）一次委托登录。
//
//	POST /api/cli/catalog/login  body: {"id":"wrangler"}
func (s *Server) handleCLILoginStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := decodeCLIBody(r, &body); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	entry, ok := clicatalog.Get(strings.TrimSpace(body.ID))
	if !ok {
		jsonError(w, http.StatusNotFound, "unknown catalog entry: "+body.ID)
		return
	}
	if !entry.LoginRunnable() {
		jsonError(w, http.StatusBadRequest,
			"catalog entry "+entry.ID+" has no non-interactive login command; run it in your terminal instead")
		return
	}
	path, err := exec.LookPath(entry.Connector.Command)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "command "+entry.Connector.Command+" is not installed")
		return
	}
	if running := s.runningLoginSession(entry.ID); running != nil {
		jsonResponse(w, map[string]any{"ok": true, "reused": true, "session": running.snapshot()})
		return
	}

	argv := append([]string{path}, entry.LoginExec...)
	sess := &cliLoginSession{
		id:        newCLILoginID(),
		catalogID: entry.ID,
		argv:      argv,
		state:     cliLoginRunning,
		startedAt: time.Now(),
	}
	s.addLoginSession(sess)
	go s.runCLILogin(sess, entry)

	jsonResponse(w, map[string]any{"ok": true, "session": sess.snapshot()})
}

// handleCLILoginByID 处理 /api/cli/catalog/login/{id}[/input|/cancel]。
func (s *Server) handleCLILoginByID(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/cli/catalog/login/")
	rest = strings.Trim(rest, "/")
	if rest == "" {
		http.NotFound(w, r)
		return
	}
	id, action, _ := strings.Cut(rest, "/")
	sess := s.getLoginSession(id)
	if sess == nil {
		jsonError(w, http.StatusNotFound, "login session not found")
		return
	}

	switch {
	case action == "" && r.Method == http.MethodGet:
		jsonResponse(w, map[string]any{"ok": true, "session": sess.snapshot()})
	case action == "input" && r.Method == http.MethodPost:
		var body struct {
			Text string `json:"text"`
		}
		if err := decodeCLIBody(r, &body); err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := sess.writeInput(body.Text); err != nil {
			jsonError(w, http.StatusConflict, err.Error())
			return
		}
		jsonResponse(w, map[string]any{"ok": true})
	case action == "cancel" && r.Method == http.MethodPost:
		sess.cancel()
		jsonResponse(w, map[string]any{"ok": true, "session": sess.snapshot()})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// writeInput 把用户在弹窗里输入的文本转发给子进程 stdin。
func (s *cliLoginSession) writeInput(text string) error {
	s.mu.Lock()
	stdin, state := s.stdin, s.state
	s.mu.Unlock()
	if state != cliLoginRunning {
		return fmt.Errorf("login process is no longer running")
	}
	if stdin == nil {
		return fmt.Errorf("login process is not accepting input")
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	s.appendOutput([]byte(text)) // 让用户看见自己输入了什么（不回显的密码除外）
	_, err := io.WriteString(stdin, text)
	return err
}

// cancel 终止登录进程。
func (s *cliLoginSession) cancel() {
	s.mu.Lock()
	cmd := s.cmd
	s.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

// runCLILogin 起子进程、转发输出、按就绪检查判定完成。
func (s *Server) runCLILogin(sess *cliLoginSession, entry clicatalog.Entry) {
	ctx, cancel := context.WithTimeout(context.Background(), cliLoginTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, sess.argv[0], sess.argv[1:]...)
	cmd.WaitDelay = 3 * time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		s.finishLogin(sess, cliLoginFailed, "cannot open stdin: "+err.Error(), nil)
		return
	}
	w := loginLogWriter{sess: sess}
	cmd.Stdout = w
	cmd.Stderr = w

	sess.mu.Lock()
	sess.cmd = cmd
	sess.stdin = stdin
	sess.mu.Unlock()

	sess.appendOutput([]byte("$ " + strings.Join(sess.argv, " ") + "\n"))
	if err := cmd.Start(); err != nil {
		s.finishLogin(sess, cliLoginFailed, "cannot start: "+err.Error(), nil)
		return
	}

	// 与等待进程并行地做就绪探测：有些 CLI 授权成功后不退出。
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	ticker := time.NewTicker(cliLoginCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case waitErr := <-waitCh:
			_ = stdin.Close()
			ready, msg := probeCatalogReadiness(context.Background(), entry)
			switch {
			case ready != nil && *ready:
				s.finishLogin(sess, cliLoginDone, "", &msg)
			case waitErr != nil:
				s.finishLogin(sess, cliLoginFailed, waitErr.Error(), &msg)
			default:
				// 退出码 0 但没有就绪检查：按成功处理（有些 CLI 的登录态
				// 只能从后续调用看出来，没配 Check 就没法再验证）。
				s.finishLogin(sess, cliLoginDone, "", &msg)
			}
			return
		case <-ticker.C:
			ready, msg := probeCatalogReadiness(context.Background(), entry)
			if ready != nil && *ready {
				_ = cmd.Process.Kill() // 已登录成功，不必再等它自己退出
				s.finishLogin(sess, cliLoginDone, "", &msg)
				return
			}
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			s.finishLogin(sess, cliLoginFailed,
				fmt.Sprintf("login did not complete within %s", cliLoginTimeout), nil)
			return
		}
	}
}

// finishLogin 收尾并写状态。
func (s *Server) finishLogin(sess *cliLoginSession, state, errText string, checkMsg *string) {
	sess.mu.Lock()
	if sess.state != cliLoginRunning {
		sess.mu.Unlock()
		return
	}
	sess.state = state
	sess.errText = errText
	if checkMsg != nil {
		sess.checkMsg = *checkMsg
	}
	sess.finishedAt = time.Now()
	sess.stdin = nil
	sess.mu.Unlock()
}

// loginLogWriter 把子进程输出接到会话上。
type loginLogWriter struct{ sess *cliLoginSession }

func (w loginLogWriter) Write(p []byte) (int, error) {
	w.sess.appendOutput(p)
	return len(p), nil
}
