package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/pkg/clicatalog"
)

// CLI 登录/OAuth 路由契约：
//   - /api/cli/catalog/login 只接受目录 id，没有代跑命令的条目返回 4xx；
//   - /api/cli/catalog/login/{session} 能到达处理器（未知会话 404，而不是 405）；
//   - /api/cli/connectors/{name}/oauth/status 能到达处理器（此前整段被当成名字）。
func TestCLILoginRoutes(t *testing.T) {
	s := &Server{}

	// 未安装/不支持代跑的条目：docker 只有安装命令，没有 login_exec。
	rec := httptest.NewRecorder()
	s.handleCLILoginStart(rec, httptest.NewRequest(http.MethodPost, "/api/cli/catalog/login",
		strings.NewReader(`{"id":"docker"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unsupported entry code = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "terminal") {
		t.Fatalf("error should point at running it in a terminal: %s", rec.Body.String())
	}

	// 未知目录 id。
	rec = httptest.NewRecorder()
	s.handleCLILoginStart(rec, httptest.NewRequest(http.MethodPost, "/api/cli/catalog/login",
		strings.NewReader(`{"id":"nope"}`)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown entry code = %d", rec.Code)
	}

	// 未知会话：必须进到处理器。
	rec = httptest.NewRecorder()
	s.handleCLILoginByID(rec, httptest.NewRequest(http.MethodGet, "/api/cli/catalog/login/abc", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown session code = %d body=%s", rec.Code, rec.Body.String())
	}

	// 连接器 OAuth 子路由：未知连接器 404（说明没被当成连接器名里的斜杠）。
	rec = httptest.NewRecorder()
	s.handleCLIConnectorByID(rec, httptest.NewRequest(http.MethodGet,
		"/api/cli/connectors/demo/oauth/status", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("connector oauth route code = %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestCatalogLoginExecCoverage：登记了 LoginExec 的条目必须同时有展示用的
// Login 命令——否则 UI 会只显示一个"登录"按钮，用户不知道实际跑的是什么。
func TestCatalogLoginExecCoverage(t *testing.T) {
	// 这里只做静态一致性检查，具体的命令正确性靠人工验证（各家 CLI 行为不同）。
	for _, id := range []string{"wrangler", "vercel"} {
		entry, ok := clicatalog.Get(id)
		if !ok {
			t.Fatalf("catalog entry %q not found", id)
		}
		if !entry.LoginRunnable() {
			t.Fatalf("%s should declare login_exec", id)
		}
		if len(entry.Login.CommandsFor("linux")) == 0 {
			t.Fatalf("%s should keep a display login command", id)
		}
	}
}

// TestCLILoginSessionParsesAuthOutput：登录命令的输出里混着 URL、短码和
// 一堆提示语，前端只该看到"去哪个页面、输什么码"。
func TestCLILoginSessionParsesAuthOutput(t *testing.T) {
	sess := &cliLoginSession{state: cliLoginRunning}
	sess.appendOutput([]byte("Opening browser to https://dash.example.com/device?code=ABCD-1234\n"))
	sess.appendOutput([]byte("Your one-time code: WXYZ-9876\n"))
	sess.appendOutput([]byte("Verification URL: https://example.com/activate\nWaiting for approval...\n"))

	snap := sess.snapshot()
	if snap["user_code"] != "WXYZ-9876" {
		t.Fatalf("user_code = %v, want the latest code", snap["user_code"])
	}
	if snap["auth_url"] != "https://example.com/activate" {
		t.Fatalf("auth_url = %v, want the latest URL", snap["auth_url"])
	}
	if got := snap["output"].(string); !strings.Contains(got, "Waiting for approval") {
		t.Fatalf("output = %q", got)
	}
}

// TestCLILoginInputAfterFinish：流程结束后再写 stdin 必须是可读错误，
// 而不是静默成功让用户以为操作生效了。
func TestCLILoginInputAfterFinish(t *testing.T) {
	sess := &cliLoginSession{state: cliLoginRunning}
	if err := sess.writeInput("hello", false); err == nil || !strings.Contains(err.Error(), "not accepting input") {
		t.Fatalf("writeInput without stdin = %v", err)
	}

	sess.state = cliLoginDone
	if err := sess.writeInput("hello", false); err == nil || !strings.Contains(err.Error(), "no longer running") {
		t.Fatalf("writeInput after finish = %v", err)
	}
}

// TestCLILoginSecretInputNotMirrored：secret 输入转发给 stdin，但输出缓冲里
// 只能有占位掩码——密码落进会话输出/日志是真实泄漏面。
func TestCLILoginSecretInputNotMirrored(t *testing.T) {
	sess := newLoginSessionWithPipe(t)
	if err := sess.writeInput("hunter2\n", true); err != nil {
		t.Fatalf("writeInput secret = %v", err)
	}
	if got := sess.snapshot()["output"].(string); strings.Contains(got, "hunter2") {
		t.Fatalf("secret input leaked into output: %q", got)
	}
	if got := sess.snapshot()["output"].(string); !strings.Contains(got, "••••") {
		t.Fatalf("secret input should leave a mask placeholder: %q", got)
	}

	// 非 secret 输入仍然镜像（stdin 是管道时 CLI 不回显普通输入）
	sess2 := newLoginSessionWithPipe(t)
	if err := sess2.writeInput("myaccount@example.com", false); err != nil {
		t.Fatalf("writeInput plain = %v", err)
	}
	if got := sess2.snapshot()["output"].(string); !strings.Contains(got, "myaccount@example.com") {
		t.Fatalf("plain input should be mirrored: %q", got)
	}
}

// newLoginSessionWithPipe 返回带可用 stdin 的会话（os.Pipe 模拟子进程管道）。
func newLoginSessionWithPipe(t *testing.T) *cliLoginSession {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close(); w.Close() })
	// 排空读端，避免写入阻塞。
	go func() { _, _ = io.Copy(io.Discard, r) }()
	return &cliLoginSession{state: cliLoginRunning, stdin: w}
}

// TestCLILoginCarriageReturnFold：进度条 \r 重绘帧折叠后，中间帧不进输出。
func TestCLILoginCarriageReturnFold(t *testing.T) {
	sess := &cliLoginSession{state: cliLoginRunning}
	sess.appendOutput([]byte("progress 10%\rprogress 50%\rprogress 99%\r\nhttps://example.com/activate\n"))
	snap := sess.snapshot()
	out := snap["output"].(string)
	if strings.Contains(out, "progress 10%") || strings.Contains(out, "progress 50%") {
		t.Fatalf("intermediate progress frames should be folded away: %q", out)
	}
	if !strings.Contains(out, "progress 99%") {
		t.Fatalf("the final frame before \\n must survive: %q", out)
	}
	if snap["auth_url"] != "https://example.com/activate" {
		t.Fatalf("auth_url = %v", snap["auth_url"])
	}
}
