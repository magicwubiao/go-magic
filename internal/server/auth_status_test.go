package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// statusBody 是 /api/auth/status 的响应体（只取测试关心的两个字段）。
type statusBody struct {
	Configured    bool `json:"configured"`
	Authenticated bool `json:"authenticated"`
}

func decodeStatus(t *testing.T, rec *httptest.ResponseRecorder) statusBody {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200 (body=%q)", rec.Code, rec.Body.String())
	}
	var body statusBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad json %q: %v", rec.Body.String(), err)
	}
	return body
}

// /api/auth/status 的 authenticated 必须与认证中间件的放行判定完全一致：
// 前端路由守卫把它当作"能不能进主界面"的唯一依据，两者一旦漂移，残留的失效
// token 就会带着十几个 401 冲进主界面，把错误提示弹到认证页上。
func TestAuthStatusAuthenticated(t *testing.T) {
	const legacyHash = "legacy-bcrypt-or-sha256-hash"
	s := &Server{authToken: legacyHash, sessions: newWebSessionManager()}

	live := s.sessions.create(false)
	if live == "" {
		t.Fatal("sessions.create 返回空 token")
	}

	cases := []struct {
		name   string
		header string
		xToken string
		query  string
		want   bool
	}{
		{name: "无凭据", want: false},
		{name: "陈旧/伪造的会话 token", header: "Bearer deadbeefdeadbeef", want: false},
		{name: "有效会话 token", header: "Bearer " + live, want: true},
		{name: "旧式静态 token（Authorization）", header: "Bearer " + legacyHash, want: true},
		{name: "旧式静态 token（X-Magic-Session-Token）", xToken: legacyHash, want: true},
		{name: "旧式静态 token（query）", query: legacyHash, want: true},
		{name: "错误凭据", header: "Bearer nope", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := "/api/auth/status"
			if tc.query != "" {
				target += "?token=" + tc.query
			}
			req := httptest.NewRequest(http.MethodGet, target, nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			if tc.xToken != "" {
				req.Header.Set("X-Magic-Session-Token", tc.xToken)
			}

			if got := s.authorized(req); got != tc.want {
				t.Errorf("authorized() = %v, want %v", got, tc.want)
			}

			rec := httptest.NewRecorder()
			s.handleAuthStatus(rec, req)
			body := decodeStatus(t, rec)
			if !body.Configured {
				t.Error("configured = false, want true（已设置 authToken）")
			}
			if body.Authenticated != tc.want {
				t.Errorf("authenticated = %v, want %v", body.Authenticated, tc.want)
			}
		})
	}
}

// 会话被吊销（改密码 / 重置认证）之后，上一批 token 必须立刻变成
// authenticated=false —— 这正是用户"有时候出现弹窗错误"的触发条件。
func TestAuthStatusAfterRevokeAll(t *testing.T) {
	s := &Server{authToken: "hash", sessions: newWebSessionManager()}
	live := s.sessions.create(true)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	req.Header.Set("Authorization", "Bearer "+live)

	before := httptest.NewRecorder()
	s.handleAuthStatus(before, req)
	if body := decodeStatus(t, before); !body.Authenticated {
		t.Fatal("有效会话应为 authenticated=true")
	}

	s.sessions.revokeAll()

	rec := httptest.NewRecorder()
	s.handleAuthStatus(rec, req)
	if body := decodeStatus(t, rec); body.Authenticated {
		t.Error("会话被吊销后仍报 authenticated=true，守卫会放行到主界面")
	}
}

// 未设置密码：configured=false，且不能报 authenticated=true（否则守卫会放行）。
func TestAuthStatusUnconfigured(t *testing.T) {
	s := &Server{sessions: newWebSessionManager()}
	req := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	req.Header.Set("Authorization", "Bearer anything")

	rec := httptest.NewRecorder()
	s.handleAuthStatus(rec, req)
	body := decodeStatus(t, rec)
	if body.Configured || body.Authenticated {
		t.Errorf("configured=%v authenticated=%v, want both false", body.Configured, body.Authenticated)
	}
}
