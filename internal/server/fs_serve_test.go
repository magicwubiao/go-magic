package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/magicwubiao/go-magic/internal/session"
	appconfig "github.com/magicwubiao/go-magic/pkg/config"
)

// 这一组测试走**真实路由**（s.buildRouter()，含 CORS 与 requireAuth 中间件），
// 而不是直接调用 handler。
//
// 原因是一段被证伪的假设：旧测试直接调 handleFSServe，并在注释里断言
// “path/session_id/token 等 query 参数会随相对资源请求一起被浏览器带上”。
// 浏览器实际行为相反 —— 相对引用按 RFC 3986 §5.3 只做路径合并，query 被整段
// 丢弃，于是 index.html 里每个 css/js/图片都变成 401，而单测因为手工拼好了
// query 而全绿。测试必须复现“浏览器真正发出的那个请求”，否则等于没测。

const fsServeTestToken = "fs-serve-test-token"

type fsServeFixture struct {
	srv  *Server
	mux  http.Handler
	root string
	dist string
}

func newFSServeFixture(t *testing.T) *fsServeFixture {
	t.Helper()
	root := t.TempDir()
	dist := filepath.Join(root, "dist")

	write := func(rel, content string) {
		t.Helper()
		full := filepath.Join(dist, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", rel, err)
		}
	}
	write("index.html", `<html><head><link rel="stylesheet" href="assets/style.css"></head><body>hello-preview</body></html>`)
	write("assets/style.css", "body{color:red}")
	write("assets/vendor/reset.css", "html{margin:0}")
	write("my page.html", "<html><body>spaced-name</body></html>")
	// 没有 index.html 的目录：旧实现下会列出其中所有文件名
	write("noindex/readme.txt", "leaked-by-listing")
	// dist 之外的文件：任何穿越尝试都不许拿到
	if err := os.WriteFile(filepath.Join(root, "outside.env"), []byte("OUTSIDE=top-secret"), 0o644); err != nil {
		t.Fatalf("WriteFile outside.env: %v", err)
	}

	store, err := session.NewStore(filepath.Join(root, "sessions.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	s := &Server{
		cfg:          &appconfig.Config{WorkingDir: root},
		sessionStore: store,
		magicHome:    root,
		authToken:    fsServeTestToken,
		sessions:     newWebSessionManager(),
	}
	return &fsServeFixture{srv: s, mux: s.buildRouter(), root: root, dist: dist}
}

// sign 用登录凭据走真实的 /api/fs/sign 拿到预览地址。
func (f *fsServeFixture) sign(t *testing.T, path, sessionID string) string {
	t.Helper()
	body, err := json.Marshal(map[string]string{"path": path, "session_id": sessionID})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/fs/sign", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+fsServeTestToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("sign(%q) = %d, body=%q", path, rec.Code, rec.Body.String())
	}
	var out struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.URL == "" {
		t.Fatalf("sign response %q: err=%v", rec.Body.String(), err)
	}
	return out.URL
}

// get 发起一个**不带任何凭据**的请求 —— 这正是 iframe 内相对子资源请求的处境：
// 没有 Authorization header，也没有 cookie。
func (f *fsServeFixture) get(target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

// resolveRelative 复现浏览器对相对引用的解析（RFC 3986 §5.3 路径合并），
// 用它推导出的地址才是浏览器真正会去请求的地址。
func resolveRelative(t *testing.T, base, ref string) string {
	t.Helper()
	b, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse base %q: %v", base, err)
	}
	return b.ResolveReference(&url.URL{Path: ref}).String()
}

// P0-1：预览页的相对资源必须能在**不带凭据**的请求下加载。
//
// 这是回归测试的核心：旧实现把托管根和 token 放在 query 上，浏览器解析
// href="assets/style.css" 时会把 query 丢掉，请求变成不带任何凭据的
// /api/fs/serve/assets/style.css，被 requireAuth 判 401 —— 预览页样式/脚本全挂。
func TestFSServeRelativeAssetsLoadWithoutCredentials(t *testing.T) {
	f := newFSServeFixture(t)

	base := f.sign(t, f.dist, "")
	if !strings.HasPrefix(base, fsServePrefix+"/") {
		t.Fatalf("signed url = %q, want prefix %q", base, fsServePrefix+"/")
	}
	// 凭据在路径里，地址本身不该再携带 token（否则等于把登录凭据交给预览页）
	if strings.Contains(base, "token=") || strings.Contains(base, "?") {
		t.Fatalf("signed url leaks credentials in query: %q", base)
	}
	// 目录根必须以 / 结尾，否则相对引用会退到上一级（base 被当成文件名）
	if !strings.HasSuffix(base, "/") {
		t.Fatalf("signed url = %q, want trailing slash so relative refs stay inside", base)
	}

	// 1) 预览入口本身
	rec := f.get(base)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview entry: code = %d, want 200 (body=%q)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "hello-preview") {
		t.Fatalf("preview entry: body = %q, want index.html content", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "html") {
		t.Errorf("preview entry: Content-Type = %q, want text/html", ct)
	}

	// 2) index.html 里的相对引用 —— 关键在于走 ResolveReference，
	//    它会像浏览器一样丢掉 query，从而真正复现线上那条 401 的请求。
	cases := []struct {
		ref     string
		want    string
		wantMim string
	}{
		{"assets/style.css", "color:red", "css"},
		{"assets/vendor/reset.css", "margin:0", "css"},
	}
	for _, tc := range cases {
		t.Run(tc.ref, func(t *testing.T) {
			target := resolveRelative(t, "http://preview.local"+base, tc.ref)
			if strings.Contains(target, "?") {
				t.Fatalf("relative ref kept a query (%q) — the browser will drop it", target)
			}
			target = strings.TrimPrefix(target, "http://preview.local")

			rec := f.get(target)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s (no credentials) = %d, want 200 — 这正是预览资源 401 的复现路径", target, rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("GET %s body = %q, want %q", target, rec.Body.String(), tc.want)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, tc.wantMim) {
				t.Errorf("GET %s Content-Type = %q, want %q", target, ct, tc.wantMim)
			}
		})
	}

	// 3) 单文件预览同样要能带动它的相对资源（旧实现走 ServeFile，相对资源全 404）
	fileBase := f.sign(t, filepath.Join(f.dist, "my page.html"), "")
	rec = f.get(fileBase)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "spaced-name") {
		t.Fatalf("single file preview: code = %d, body = %q", rec.Code, rec.Body.String())
	}
	rec = f.get(resolveRelative(t, "http://preview.local"+fileBase, "assets/style.css")[len("http://preview.local"):])
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "color:red") {
		t.Fatalf("single file relative asset: code = %d, body = %q", rec.Code, rec.Body.String())
	}

	// 4) 视频拖拽进度条依赖 Range，锁住 ServeContent 的行为
	req := httptest.NewRequest(http.MethodGet, fileBase, nil)
	req.Header.Set("Range", "bytes=0-4")
	rec = httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent {
		t.Errorf("range request: code = %d, want 206", rec.Code)
	}
}

// P0-2：目录列举必须关闭。
//
// 旧实现用 http.FileServer 托管目录，缺少 index.html 时它会直接生成目录
// 列表 —— 预览一个普通目录就能看到 .env / 密钥文件名。
func TestFSServeDisablesDirectoryListing(t *testing.T) {
	f := newFSServeFixture(t)
	base := f.sign(t, f.dist, "")

	for _, target := range []string{base + "noindex/", base + "noindex"} {
		rec := f.get(target)
		if rec.Code != http.StatusForbidden {
			t.Errorf("GET %s = %d, want 403（目录列举必须关闭）", target, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "readme.txt") || strings.Contains(rec.Body.String(), "leaked-by-listing") {
			t.Errorf("GET %s leaked directory contents: %q", target, rec.Body.String())
		}
	}

	// 目录自带 index.html 时仍要正常返回，而不是一并 403
	if rec := f.get(base); rec.Code != http.StatusOK {
		t.Errorf("dir with index.html = %d, want 200", rec.Code)
	}

	// 没有 index.html 的目录在签发阶段就该给出可读错误，
	// 而不是让 iframe 收到 403（iframe 会把它当普通文档，前端只看到空白）
	body, _ := json.Marshal(map[string]string{"path": filepath.Join(f.dist, "noindex")})
	req := httptest.NewRequest(http.MethodPost, "/api/fs/sign", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+fsServeTestToken)
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("sign(dir without index.html) = %d, want 400", rec.Code)
	}
}

// 签名是这条链路上唯一的准入条件：没有签名、签名被改、签名过期、签名换了密钥，
// 一律拒绝；旧的 ?token= 旁路必须失效。
func TestFSServeRequiresValidSignature(t *testing.T) {
	f := newFSServeFixture(t)
	base := f.sign(t, f.dist, "")
	asset := base + "assets/style.css"

	if rec := f.get(asset); rec.Code != http.StatusOK {
		t.Fatalf("sanity: signed asset = %d, want 200", rec.Code)
	}

	t.Run("旧式 query token 旁路必须失效", func(t *testing.T) {
		target := fsServePrefix + "/assets/style.css?path=" + url.QueryEscape(f.dist) + "&token=" + fsServeTestToken
		if rec := f.get(target); rec.Code == http.StatusOK {
			t.Errorf("query token bypass still works: %d", rec.Code)
		}
	})

	t.Run("Authorization header 不能替代签名", func(t *testing.T) {
		target := fsServePrefix + "/assets/style.css"
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("Authorization", "Bearer "+fsServeTestToken)
		rec := httptest.NewRecorder()
		f.mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("header-authenticated serve = %d, want 403（该路由不接受 header 凭据）", rec.Code)
		}
	})

	t.Run("签名字符被篡改", func(t *testing.T) {
		tampered := base[:len(base)-2] + "XX/"
		if rec := f.get(tampered + "assets/style.css"); rec.Code == http.StatusOK {
			t.Error("tampered signature was accepted")
		}
	})

	t.Run("签名过期", func(t *testing.T) {
		sig, err := f.srv.signFSServeDir(f.dist, time.Now().Add(-time.Minute))
		if err != nil {
			t.Fatalf("signFSServeDir: %v", err)
		}
		if rec := f.get(fsServePrefix + "/" + sig + "/assets/style.css"); rec.Code != http.StatusForbidden {
			t.Errorf("expired signature = %d, want 403", rec.Code)
		}
	})

	t.Run("换个密钥签的签名不认", func(t *testing.T) {
		other := &Server{authToken: "another-secret", sessions: newWebSessionManager()}
		sig, err := other.signFSServeDir(f.dist, time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("signFSServeDir: %v", err)
		}
		if rec := f.get(fsServePrefix + "/" + sig + "/assets/style.css"); rec.Code == http.StatusOK {
			t.Error("signature from a different key was accepted")
		}
	})
}

// 未配置 authToken 时密钥会退化成一个公开常量，任何人都能伪造签名；
// 这种状态下必须拒绝签发与校验，而不是「看起来能用」。
func TestFSServeRefusesToSignWithoutAuthToken(t *testing.T) {
	s := &Server{sessions: newWebSessionManager()}
	if _, err := s.fsTicketKey(); err == nil {
		t.Error("fsTicketKey() 在 authToken 为空时应报错")
	}
	if _, err := s.signFSServeDir("/tmp", time.Now().Add(time.Hour)); err == nil {
		t.Error("signFSServeDir() 在 authToken 为空时应拒绝签发")
	}
	if _, err := s.parseFSServeSig("anything.anything", time.Now()); err == nil {
		t.Error("parseFSServeSig() 在 authToken 为空时应拒绝")
	}
}

func TestFSServeSignRouteRequiresAuth(t *testing.T) {
	f := newFSServeFixture(t)
	body, _ := json.Marshal(map[string]string{"path": f.dist})
	req := httptest.NewRequest(http.MethodPost, "/api/fs/sign", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated sign = %d, want 401", rec.Code)
	}
}

// 目录穿越：显式 .. 段、以及指向托管根之外的符号链接，都必须被拒绝。
func TestFSServeRejectsTraversal(t *testing.T) {
	f := newFSServeFixture(t)

	// 1) 单元级：路径归一化必须显式拒绝 .. 而不是静默改写
	if _, err := safeJoinFSServe(f.dist, "../outside.env"); err == nil {
		t.Error("safeJoinFSServe 允许了 .. 穿越")
	}
	if _, err := safeJoinFSServe(f.dist, "assets/../../outside.env"); err == nil {
		t.Error("safeJoinFSServe 允许了嵌套 .. 穿越")
	}
	if _, err := safeJoinFSServe(f.dist, "assets/style.css"); err != nil {
		t.Errorf("safeJoinFSServe 拒绝了正常路径: %v", err)
	}

	// 2) 符号链接指向托管根之外
	outside := filepath.Join(f.root, "outside.env")
	link := filepath.Join(f.dist, "escape.env")
	if err := os.Symlink(outside, link); err == nil {
		if _, err := safeJoinFSServe(f.dist, "escape.env"); err == nil {
			t.Error("safeJoinFSServe 允许了指向根外的符号链接")
		}
		base := f.sign(t, f.dist, "")
		rec := f.get(base + "escape.env")
		if rec.Code == http.StatusOK || strings.Contains(rec.Body.String(), "top-secret") {
			t.Errorf("符号链接逃逸成功: code=%d body=%q", rec.Code, rec.Body.String())
		}
	} else {
		t.Logf("跳过符号链接用例（环境不支持）: %v", err)
	}

	// 3) 经路由的穿越尝试：mux 会先做一次路径清洗（可能 301），
	//    无论哪种处理，都不能 200 拿到根外文件。
	base := f.sign(t, f.dist, "")
	rec := f.get(base + "../outside.env")
	if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "top-secret") {
		t.Errorf("穿越拿到根外文件: code=%d body=%q", rec.Code, rec.Body.String())
	}
}

// session 场景：凭据覆盖的是 resolveFSPath 解析后的目录，因此
// session 之外的目标在签发阶段就被拒绝。
func TestFSServeSessionScopedSigning(t *testing.T) {
	f := newFSServeFixture(t)

	if err := f.srv.sessionStore.SaveSession(context.Background(), &session.Session{
		ID:       "sess-serve",
		Profile:  "test",
		Platform: "web",
		WorkDir:  f.dist,
	}); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	// 合法：session 根目录
	base := f.sign(t, "", "sess-serve")
	if rec := f.get(base); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "hello-preview") {
		t.Fatalf("session root: code = %d, body = %q", rec.Code, rec.Body.String())
	}

	// 非法：越出 session workdir
	body, _ := json.Marshal(map[string]string{"path": "../../etc", "session_id": "sess-serve"})
	req := httptest.NewRequest(http.MethodPost, "/api/fs/sign", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+fsServeTestToken)
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Errorf("session 穿越被签发: %d body=%q", rec.Code, rec.Body.String())
	}
}
