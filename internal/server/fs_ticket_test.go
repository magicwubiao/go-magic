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
		// 上传内容是不可信用户内容：nosniff 恒在，CSP 按类型给（见
		// uploadTicketPolicyFor）。
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
		}
		// 图片的 CSP 里不能有 default-src：它会连 style-src 一起卡掉，而
		// Chrome 的图片文档正是靠自注入的样式把 <img> 居中的——带上下场是
		// 「新标签页打开图片被贴到左上角」（实测 rect 从居中变成 [0,0]）。
		if got := rec.Header().Get("Content-Security-Policy"); strings.Contains(got, "default-src") {
			t.Errorf("图片的 CSP = %q：default-src 会让图片在新标签页里不居中", got)
		}
	})

	t.Run("Content-Disposition 带真实文件名，图片可内联", func(t *testing.T) {
		// 裸 "attachment" 时浏览器只能用 URL 最后一段当文件名，而票据的 URL
		// 最后一段就是整张票据 —— 用户会下载到一个叫 djEAdXBsb2Fk… 的文件。
		// 这里盯住的是「名字必须是真实文件名」这条回归。
		if err := os.WriteFile(filepath.Join(uploadDir, "b.html"), []byte("<h1>hi</h1>"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		imgURL := f.signWithScope(t, "uploads", "/api/uploads/sess-1/a.png", "", nil)
		rec := f.get(imgURL)
		cd := rec.Header().Get("Content-Disposition")
		if !strings.HasPrefix(cd, "inline") {
			t.Errorf("png 的 Content-Disposition = %q, want inline（否则「在新标签页打开」变成下载）", cd)
		}
		if !strings.Contains(cd, `filename="a.png"`) {
			t.Errorf("png 的 Content-Disposition = %q, 缺少真实文件名", cd)
		}

		// HTML 现在内联渲染（预览不再是白框），代价必须由 CSP 付：关进不透明源
		// 后读不到 localStorage 里的登录凭据，请求也不带任何凭据。
		pageURL := f.signWithScope(t, "uploads", "/api/uploads/sess-1/b.html", "", nil)
		page := f.get(pageURL)
		cd = page.Header().Get("Content-Disposition")
		if !strings.HasPrefix(cd, "inline") {
			t.Errorf("html 的 Content-Disposition = %q, want inline（否则预览是个白框）", cd)
		}
		if !strings.Contains(cd, `filename="b.html"`) {
			t.Errorf("html 的 Content-Disposition = %q, 缺少真实文件名", cd)
		}
		hcsp := page.Header().Get("Content-Security-Policy")
		if !strings.Contains(hcsp, "sandbox") {
			t.Errorf("html 的 CSP = %q, 缺少 sandbox：被预览页面会在本站源里跑脚本", hcsp)
		}
		if strings.Contains(hcsp, "allow-same-origin") {
			t.Errorf("html 的 CSP = %q 带了 allow-same-origin：能读到本站 localStorage", hcsp)
		}
		if !strings.Contains(hcsp, "allow-scripts") {
			t.Errorf("html 的 CSP = %q 缺少 allow-scripts：上传的报告跑不起来", hcsp)
		}
		// HTML 正文必须原样送达，否则渲染出来的是半截页面。
		if page.Body.String() != "<h1>hi</h1>" {
			t.Errorf("html body = %q, want 原文", page.Body.String())
		}

		// svg 是可执行的 XML 文档，顶层打开就是在本站源里跑脚本：保持下载。
		if err := os.WriteFile(filepath.Join(uploadDir, "c.svg"), []byte("<svg/>"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		svgCD := f.get(f.signWithScope(t, "uploads", "/api/uploads/sess-1/c.svg", "", nil)).
			Header().Get("Content-Disposition")
		if !strings.HasPrefix(svgCD, "attachment") {
			t.Errorf("svg 的 Content-Disposition = %q, want attachment（可执行 XML 不许内联）", svgCD)
		}
	})

	t.Run("有元数据时用原始文件名，非 ASCII 走 filename*", func(t *testing.T) {
		meta := f.srv.ensureUploadsMeta()
		if meta == nil {
			t.Skip("uploads 元数据库不可用")
		}
		// 元数据库落在 magicHome（= 本测试的 TempDir）里，句柄必须在本子测试
		// 结束前关掉：否则 Windows 上 TempDir 清理会因文件占用而失败。
		t.Cleanup(func() {
			_ = meta.Close()
			f.srv.uploadsMeta = nil
		})
		if err := meta.Upsert("sess-1", "a.png", "截图 01.png", 7, "image/png"); err != nil {
			t.Fatalf("Upsert: %v", err)
		}

		rec := f.get(f.signWithScope(t, "uploads", "/api/uploads/sess-1/a.png", "", nil))
		cd := rec.Header().Get("Content-Disposition")
		if !strings.Contains(cd, `filename*=UTF-8''%E6%88%AA%E5%9B%BE%2001.png`) {
			t.Errorf("Content-Disposition = %q, 未带回原始中文名（或空格编码不对）", cd)
		}
		// ASCII 回退名里不能出现裸中文或裸空格编码错误之外的控制字符。
		if strings.Contains(cd, "截图") {
			t.Errorf("Content-Disposition = %q 里出现裸非 ASCII：响应头必须是 ASCII", cd)
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

// 上传票据的 Content-Disposition：分档正确 + 名字不可注入响应头。
func TestUploadContentDisposition(t *testing.T) {
	inline := []string{".png", ".PNG", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".ico", ".avif", ".mp4", ".webm", ".mp3", ".wav"}
	for _, ext := range inline {
		if got := uploadContentDisposition(ext, "a"+ext); !strings.HasPrefix(got, "inline") {
			t.Errorf("uploadContentDisposition(%q) = %q, want inline", ext, got)
		}
	}

	// html 与 pdf 也要内联渲染：它们的不安全之处由 CSP 兜（html 关进不透明源，
	// pdf 交给浏览器内置阅读器），而不是靠「一律下载」把预览做成白框。
	render := []string{".html", ".htm", ".HTML", ".pdf", ".PDF"}
	for _, ext := range render {
		if got := uploadContentDisposition(ext, "a"+ext); !strings.HasPrefix(got, "inline") {
			t.Errorf("uploadContentDisposition(%q) = %q, want inline（否则预览/新标签页是白框）", ext, got)
		}
	}

	// 这几类顶层打开就是「在本站源里执行代码」（svg 是可执行 XML），必须下载。
	// 空扩展名同样落到下载：类型判不出来时宁可给文件，也不赌它能安全渲染。
	download := []string{".svg", ".xhtml", ".xml", ".txt", ".js", ".exe", ".zip", "", ".bin"}
	for _, ext := range download {
		if got := uploadContentDisposition(ext, "a"+ext); !strings.HasPrefix(got, "attachment") {
			t.Errorf("uploadContentDisposition(%q) = %q, want attachment", ext, got)
		}
	}

	t.Run("CSP 与类型的配对", func(t *testing.T) {
		// 图片的 CSP 里**不能**有 default-src：Chrome 的图片文档靠自注入的样式
		// 把 <img> 居中，default-src 'none' 连 style-src 一起卡死，图片就贴到
		// 左上角（实测 rect=[0,0]）。这条断言盯的正是那个「看图不居中」的回归。
		media := uploadTicketCSP(".png")
		if strings.Contains(media, "default-src") {
			t.Errorf("图片 CSP = %q：default-src 会卡掉图片文档的居中样式", media)
		}
		if !strings.Contains(media, "sandbox") {
			t.Errorf("图片 CSP = %q，缺少 sandbox", media)
		}

		// HTML：必须能跑脚本（报告里的图表），但绝不能拿到本站源。
		html := uploadTicketCSP(".html")
		if !strings.Contains(html, "allow-scripts") {
			t.Errorf("HTML CSP = %q 缺少 allow-scripts：上传的报告跑不起来", html)
		}
		if strings.Contains(html, "allow-same-origin") {
			t.Errorf("HTML CSP = %q 带了 allow-same-origin：被预览页面能读到 localStorage 凭据", html)
		}

		// PDF 刻意不发 CSP：sandbox 会让内置阅读器初始化失败。
		if pdf := uploadTicketCSP(".pdf"); pdf != "" {
			t.Errorf("PDF CSP = %q, want 空（sandbox 会让内置阅读器起不来）", pdf)
		}

		// 下载档保留最紧的 CSP 作为第二道闸。
		if dl := uploadTicketCSP(".svg"); !strings.Contains(dl, "default-src 'none'") {
			t.Errorf("下载档 CSP = %q, want default-src 'none'; sandbox", dl)
		}
	})

	t.Run("名字来自原始客户端文件名，必须清洗", func(t *testing.T) {
		// CRLF 注入：展示名可能带着换行来到响应头，哪怕只有一处漏网都能
		// 伪造出额外的响应头。
		got := uploadContentDisposition(".png", "evil\r\nX-Injected: 1.png")
		if strings.ContainsAny(got, "\r\n") {
			t.Fatalf("Content-Disposition = %q 里出现裸换行：响应头可被注入", got)
		}
		if strings.Contains(got, "X-Injected:") {
			t.Errorf("Content-Disposition = %q 里保留了可被当作头的片段", got)
		}
		// 引号与反斜杠会提前闭合 filename="..."。
		got = uploadContentDisposition(".png", `a"b\c.png`)
		if strings.Count(got, `"`)%2 != 0 {
			t.Errorf("Content-Disposition = %q 的引号不成对", got)
		}
		if strings.Contains(got, `\`) {
			t.Errorf("Content-Disposition = %q 里出现裸反斜杠", got)
		}
	})

	t.Run("中文名两份都给：filename= 回退 + filename* 原名", func(t *testing.T) {
		got := uploadContentDisposition(".png", "核算表.png")
		if !strings.Contains(got, `filename*=UTF-8''`) {
			t.Errorf("Content-Disposition = %q 缺少 filename*（中文名会丢）", got)
		}
		if strings.Contains(got, "核算表") {
			t.Errorf("Content-Disposition = %q 里出现裸非 ASCII", got)
		}
		// ASCII 回退名有损（中文塌成下划线），但扩展名必须留住，否则下载出来的
		// 文件在系统里认不出类型。
		if !strings.Contains(got, `.png"`) {
			t.Errorf("Content-Disposition = %q 的 ASCII 回退名丢了扩展名", got)
		}
	})
}
