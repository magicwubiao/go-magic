package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 这一组测试覆盖「票据替代 ?token=」这条改造的核心安全断言：
//
//  1. 票据只能由持登录凭据者签发，且签名绑定作用域与路径；
//  2. 消费端只信签名载荷，客户端附带的 query 参数无法改变「读哪个文件」；
//  3. 作用域之间不可互相顶替（read 票据不能变成目录托管，不能订阅事件流）；
//  4. 过期/篡改/换密钥一律拒绝；
//  5. 即便载荷来自合法签名，消费端仍独立校验路径不越界。
//
// 第 2、5 条是本设计的立足点：如果消费端还会去读 ?path=，那票据就退化成了
// 一张「万能通行证 + 一个建议值」，等于把 ?token= 的风险原样保留下来。

// signWithScope 用登录凭据走真实的 /api/fs/sign 端点换取票据地址。
func (f *fsServeFixture) signWithScope(t *testing.T, scope, path, sessionID string, extra map[string]any) string {
	t.Helper()
	body := map[string]any{"scope": scope}
	if path != "" {
		body["path"] = path
	}
	if sessionID != "" {
		body["session_id"] = sessionID
	}
	for k, v := range extra {
		body[k] = v
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/fs/sign", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+fsServeTestToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("sign(scope=%s path=%q) = %d, body=%q", scope, path, rec.Code, rec.Body.String())
	}
	var out struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.URL == "" {
		t.Fatalf("sign response %q: err=%v", rec.Body.String(), err)
	}
	return out.URL
}

func newTicketServer(token string) *Server {
	return &Server{authToken: token, sessions: newWebSessionManager()}
}

func TestFSTicketRoundTrip(t *testing.T) {
	s := newTicketServer("ticket-test-secret")
	now := time.Now()

	sig, err := s.signFSTicket(fsTicket{Scope: fsScopeRead, Path: "/tmp/a.txt", Exp: now.Add(time.Hour)})
	if err != nil {
		t.Fatalf("signFSTicket: %v", err)
	}
	tk, err := s.parseFSTicket(sig, now)
	if err != nil {
		t.Fatalf("parseFSTicket: %v", err)
	}
	if tk.Scope != fsScopeRead || tk.Path != "/tmp/a.txt" {
		t.Errorf("round trip = {%q %q}, want {read /tmp/a.txt}", tk.Scope, tk.Path)
	}

	t.Run("过期票据必须失效", func(t *testing.T) {
		expired, err := s.signFSTicket(fsTicket{Scope: fsScopeRead, Path: "/tmp/a.txt", Exp: now.Add(-time.Second)})
		if err != nil {
			t.Fatalf("signFSTicket: %v", err)
		}
		if _, err := s.parseFSTicket(expired, now); err == nil {
			t.Error("过期票据被接受")
		}
	})

	t.Run("篡改签名必须失效", func(t *testing.T) {
		tampered := sig[:len(sig)-2] + "XX"
		if _, err := s.parseFSTicket(tampered, now); err == nil {
			t.Error("篡改后的签名被接受")
		}
	})

	t.Run("改载荷但保留签名必须失效", func(t *testing.T) {
		// 改掉载荷里的一个字节但不重签（等价于把 read 私自改成 serve 的提权
		// 尝试）：HMAC 覆盖整个载荷，必须拦住。
		payload, mac, ok := strings.Cut(sig, ".")
		if !ok {
			t.Fatalf("票据缺少分隔符: %q", sig)
		}
		forged := payload[:len(payload)-1] + "A" + "." + mac
		if _, err := s.parseFSTicket(forged, now); err == nil {
			t.Error("未重签的载荷被接受")
		}
	})

	t.Run("换密钥签发必须失效", func(t *testing.T) {
		other := newTicketServer("another-secret")
		otherSig, err := other.signFSTicket(fsTicket{Scope: fsScopeRead, Path: "/tmp/a.txt", Exp: now.Add(time.Hour)})
		if err != nil {
			t.Fatalf("signFSTicket: %v", err)
		}
		if _, err := s.parseFSTicket(otherSig, now); err == nil {
			t.Error("其它密钥签发的票据被接受")
		}
	})

	t.Run("未配置 authToken 时拒绝签发与校验", func(t *testing.T) {
		bare := newTicketServer("")
		if _, err := bare.signFSTicket(fsTicket{Scope: fsScopeRead, Path: "/tmp/a.txt", Exp: now.Add(time.Hour)}); err == nil {
			t.Error("authToken 为空时仍签发了票据（密钥会退化成公开常量）")
		}
		if _, err := bare.parseFSTicket(sig, now); err == nil {
			t.Error("authToken 为空时仍接受了票据")
		}
	})
}

// cutTicket 只用于构造「改载荷但保留签名」的畸形输入。
func cutTicket(sig string) (payload, mac, rest string) {
	for i := 0; i < len(sig); i++ {
		if sig[i] == '.' {
			return sig[:i], sig[i+1:], sig[i:]
		}
	}
	return sig, "", ""
}

// 作用域必须严格隔离，否则相邻作用域之间就是一条提权缝隙。
func TestFSTicketScopesDoNotCross(t *testing.T) {
	s := newTicketServer("ticket-test-secret")
	now := time.Now()

	readSig, err := s.signFSTicket(fsTicket{Scope: fsScopeRead, Path: "/tmp/dir", Exp: now.Add(time.Hour)})
	if err != nil {
		t.Fatalf("signFSTicket: %v", err)
	}
	// read 票据被塞进 serve 的路径位：如果不校验作用域，它就把「读一个文件」
	// 偷换成了「托管整个目录」。
	if _, err := s.parseFSServeSig(readSig, now); err == nil {
		t.Error("read 票据被 serve 接受：作用域未隔离")
	}

	downloadSig, err := s.signFSTicket(fsTicket{Scope: fsScopeDownload, Path: "/tmp/dir", Exp: now.Add(time.Hour)})
	if err != nil {
		t.Fatalf("signFSTicket: %v", err)
	}
	if _, err := s.parseFSServeSig(downloadSig, now); err == nil {
		t.Error("download 票据被 serve 接受：作用域未隔离")
	}
}

// 消费端只信签名载荷：客户端补上的 ?path= 不能改变实际读取的文件。
func TestFSTicketEndpointIgnoresQuerySuppliedPath(t *testing.T) {
	f := newFSServeFixture(t)

	wantContent := "signed-file-content"
	signed := filepath.Join(f.dist, "signed.txt")
	other := filepath.Join(f.root, "outside.env") // 内容为 OUTSIDE=top-secret
	if err := os.WriteFile(signed, []byte(wantContent), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	target := f.signWithScope(t, "read", signed, "", nil)

	// 1) 裸票据：返回被签名的那个文件
	rec := f.get(target)
	if rec.Code != http.StatusOK {
		t.Fatalf("ticket read = %d, want 200 (body=%q)", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != wantContent {
		t.Errorf("body = %q, want %q", rec.Body.String(), wantContent)
	}

	// 2) 追加 ?path=<别处文件>：必须仍然返回被签名的那个文件。
	//    若这里返回了 other 的内容，票据就退化成了「万能通行证 + 一个建议值」，
	//    与 ?token= 的风险等价。
	rec2 := f.get(target + "?path=" + url.QueryEscape(other))
	if rec2.Code != http.StatusOK {
		t.Fatalf("ticket read with query path = %d, want 200", rec2.Code)
	}
	if rec2.Body.String() != wantContent {
		t.Errorf("query 里的 path 影响了读取结果：body = %q（应为 %q）", rec2.Body.String(), wantContent)
	}
}

func TestFSTicketEndpointRejectsBadTickets(t *testing.T) {
	f := newFSServeFixture(t)
	signedFile := filepath.Join(f.dist, "assets", "style.css")

	t.Run("过期票据", func(t *testing.T) {
		sig, err := f.srv.signFSTicket(fsTicket{
			Scope: fsScopeRead,
			Path:  signedFile,
			Exp:   time.Now().Add(-time.Minute),
		})
		if err != nil {
			t.Fatalf("signFSTicket: %v", err)
		}
		if rec := f.get(fsTicketPrefix + "/" + sig); rec.Code != http.StatusForbidden {
			t.Errorf("过期票据 = %d, want 403", rec.Code)
		}
	})

	t.Run("篡改签名", func(t *testing.T) {
		target := f.signWithScope(t, "read", signedFile, "", nil)
		tampered := target[:len(target)-2] + "XX"
		if rec := f.get(tampered); rec.Code != http.StatusForbidden {
			t.Errorf("篡改票据 = %d, want 403", rec.Code)
		}
	})

	t.Run("serve 票据不能从这个入口取文件", func(t *testing.T) {
		// serve 票据只走 /api/fs/serve（那里才做 subpath 解析）。这里直接构造
		// 一张 serve 票据塞进 ticket 入口：必须被拒，否则两种入口的行为可以
		// 互相顶替，作用域的隔离就失效了。
		// 注意不能用 signWithScope("serve")——那个签发端返回的是 /api/fs/serve
		// 地址，测到的会是另一条路由。
		sig, err := f.srv.signFSTicket(fsTicket{
			Scope: fsScopeServe,
			Path:  f.dist,
			Exp:   time.Now().Add(time.Hour),
		})
		if err != nil {
			t.Fatalf("signFSTicket: %v", err)
		}
		if rec := f.get(fsTicketPrefix + "/" + sig); rec.Code != http.StatusForbidden {
			t.Errorf("serve 票据打到 ticket 入口 = %d, want 403", rec.Code)
		}
	})

	t.Run("票据后多带路径段直接 404", func(t *testing.T) {
		// 票据必须恰好占一个路径段。这里刻意不用 ".."——那会被 ServeMux 当成
		// 非规范路径先 301 掉，测到的是路由层而不是本处理器的判定。
		target := f.signWithScope(t, "read", signedFile, "", nil)
		if rec := f.get(target + "/extra"); rec.Code != http.StatusNotFound {
			t.Errorf("票据后多带路径段 = %d, want 404", rec.Code)
		}
	})

	t.Run("未签名请求不能取文件", func(t *testing.T) {
		if rec := f.get("/api/fs/read?path=" + url.QueryEscape(signedFile)); rec.Code == http.StatusOK {
			t.Error("未经签名的 /api/fs/read 竟然返回了文件")
		}
	})

	t.Run("目录不能被当成文件读", func(t *testing.T) {
		// 签发端应提前拒绝，而不是签出一张使用时必然失败的票据。
		body, _ := json.Marshal(map[string]any{"scope": "read", "path": f.dist})
		req := httptest.NewRequest(http.MethodPost, "/api/fs/sign", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+fsServeTestToken)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		f.mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("对目录签 read 票据 = %d, want 400", rec.Code)
		}
	})
}

// 上传作用域：即便票据载荷来自合法签名，消费端仍独立校验路径不越界。
func TestFSTicketUploadsScope(t *testing.T) {
	f := newFSServeFixture(t)

	uploadDir := filepath.Join(f.root, "uploads", "sess-1")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	img := filepath.Join(uploadDir, "a.png")
	if err := os.WriteFile(img, []byte("PNGDATA"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Run("合法上传票据可取到附件", func(t *testing.T) {
		target := f.signWithScope(t, "uploads", "/api/uploads/sess-1/a.png", "", nil)
		rec := f.get(target)
		if rec.Code != http.StatusOK {
			t.Fatalf("uploads 票据 = %d, want 200 (body=%q)", rec.Code, rec.Body.String())
		}
		if rec.Body.String() != "PNGDATA" {
			t.Errorf("body = %q, want PNGDATA", rec.Body.String())
		}
		// 上传内容是不可信用户内容，必须沿用 /api/uploads/ 的加固头。
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
		}
		if got := rec.Header().Get("Content-Security-Policy"); got == "" {
			t.Error("缺少 CSP：上传的 SVG/HTML 可能在本源内渲染")
		}
	})

	t.Run("载荷里的越界路径仍被拒绝", func(t *testing.T) {
		// 直接签一张 path 指向上传根之外的票据：签名本身是合法的，
		// 检验的正是消费端有没有独立做路径校验。
		for _, bad := range []string{"../../outside.env", "../uploads/../../outside.env", "/etc/passwd"} {
			sig, err := f.srv.signFSTicket(fsTicket{
				Scope: fsScopeUploads,
				Path:  bad,
				Exp:   time.Now().Add(time.Hour),
			})
			if err != nil {
				t.Fatalf("signFSTicket: %v", err)
			}
			if rec := f.get(fsTicketPrefix + "/" + sig); rec.Code != http.StatusForbidden {
				t.Errorf("越界路径 %q = %d, want 403", bad, rec.Code)
			}
		}
	})
}

// 签发入口本身必须仍然要登录凭据，否则任何人都能自助领票。
func TestFSTicketSignStillRequiresAuth(t *testing.T) {
	f := newFSServeFixture(t)
	body, _ := json.Marshal(map[string]any{"scope": "read", "path": filepath.Join(f.dist, "assets", "style.css")})
	req := httptest.NewRequest(http.MethodPost, "/api/fs/sign", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("未认证签发 = %d, want 401", rec.Code)
	}
}

// SSE 事件流的授权判定：支持 events 票据与请求头，但必须彻底关掉 ?token=。
func TestEventsRequestAuthorized(t *testing.T) {
	s := newTicketServer("events-secret")

	if s.eventsRequestAuthorized(httptest.NewRequest(http.MethodGet, "/api/events", nil)) {
		t.Error("无凭据被放行")
	}

	// 这条断言是本次改造的核心：登录凭据出现在 query 里必须不再被接受。
	legacy := httptest.NewRequest(http.MethodGet, "/api/events?token=events-secret", nil)
	if s.eventsRequestAuthorized(legacy) {
		t.Error("?token= 后门仍在：事件流仍可用登录凭据从 query 订阅")
	}

	sig, err := s.signFSTicket(fsTicket{Scope: fsScopeEvents, Exp: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatalf("signFSTicket: %v", err)
	}
	ok := httptest.NewRequest(http.MethodGet, "/api/events?sig="+url.QueryEscape(sig), nil)
	if !s.eventsRequestAuthorized(ok) {
		t.Error("有效 events 票据被拒")
	}

	// 作用域不符：read 票据不能用来订阅事件流。
	readSig, err := s.signFSTicket(fsTicket{Scope: fsScopeRead, Path: "/tmp/x", Exp: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatalf("signFSTicket: %v", err)
	}
	cross := httptest.NewRequest(http.MethodGet, "/api/events?sig="+url.QueryEscape(readSig), nil)
	if s.eventsRequestAuthorized(cross) {
		t.Error("read 票据被用于订阅事件流：作用域未隔离")
	}

	// 过期票据同样拒绝。
	expiredSig, err := s.signFSTicket(fsTicket{Scope: fsScopeEvents, Exp: time.Now().Add(-time.Minute)})
	if err != nil {
		t.Fatalf("signFSTicket: %v", err)
	}
	expired := httptest.NewRequest(http.MethodGet, "/api/events?sig="+url.QueryEscape(expiredSig), nil)
	if s.eventsRequestAuthorized(expired) {
		t.Error("过期 events 票据被接受")
	}

	// 请求头凭据（其它客户端 / 测试）仍然支持。
	hdr := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	hdr.Header.Set("Authorization", "Bearer events-secret")
	if !s.eventsRequestAuthorized(hdr) {
		t.Error("请求头凭据被拒")
	}
}
