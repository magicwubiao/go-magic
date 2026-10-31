package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// TestDeviceFlow 走一遍完整的设备码流程：
// 设备授权 → 第一次轮询 pending → 第二次拿到 token → 状态变 authorized。
func TestDeviceFlow(t *testing.T) {
	oldFloor := devicePollFloor
	devicePollFloor = 10 * time.Millisecond
	t.Cleanup(func() { devicePollFloor = oldFloor })

	var polls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/device":
			_ = r.ParseForm()
			if r.Form.Get("client_id") != "cid" {
				t.Errorf("device request client_id = %q", r.Form.Get("client_id"))
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":      "dev-1",
				"user_code":        "ABCD-1234",
				"verification_uri": "https://example.com/device",
				"expires_in":       60,
				"interval":         0, // 服务端没给间隔，走下限
			})
		case "/token":
			_ = r.ParseForm()
			if got := r.Form.Get("device_code"); got != "dev-1" {
				t.Errorf("token request device_code = %q", got)
			}
			if atomic.AddInt32(&polls, 1) == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "authorization_pending"})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "raw-token",
				"token_type":   "bearer",
				"expires_in":   3600,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	store := NewOAuthTokenStore(filepath.Join(t.TempDir(), "oauth_tokens.json"))
	mgr := NewOAuthManager(store)

	dc, err := mgr.StartDeviceAuthorization("cliconn:demo", DeviceFlowConfig{
		DeviceAuthorizationEndpoint: srv.URL + "/device",
		TokenEndpoint:               srv.URL + "/token",
		ClientID:                    "cid",
		Scopes:                      []string{"read"},
	})
	if err != nil {
		t.Fatalf("StartDeviceAuthorization: %v", err)
	}
	if dc == nil || dc.UserCode != "ABCD-1234" {
		t.Fatalf("device code = %+v", dc)
	}

	// 流程进行中：状态必须是 awaiting，且带上用户要输入的短码。
	if st := mgr.Status("cliconn:demo"); st.State != OAuthStateAwaitingAuthorization || st.UserCode != "ABCD-1234" {
		t.Fatalf("status while pending = %+v", st)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		st := mgr.Status("cliconn:demo")
		if st.State == OAuthStateAuthorized {
			break
		}
		if st.State == OAuthStateFailed {
			t.Fatalf("flow failed: %s", st.Error)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for authorization, status = %+v", st)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// token 落盘时把刷新所需的冗余字段一起存下来。
	tok, ok := store.Get("cliconn:demo")
	if !ok || tok.AccessToken != "raw-token" {
		t.Fatalf("stored token = %+v ok=%v", tok, ok)
	}
	if tok.TokenEndpoint != srv.URL+"/token" || tok.ClientID != "cid" {
		t.Fatalf("stored token missing refresh fields: %+v", tok)
	}

	// CLI 要的是裸串（GH_TOKEN=xxx），不是 Authorization 头。
	raw, err := mgr.EnsureAccessToken("cliconn:demo")
	if err != nil {
		t.Fatalf("EnsureAccessToken: %v", err)
	}
	if raw != "raw-token" {
		t.Fatalf("raw token = %q", raw)
	}

	// 再点一次"登录"：已有 token 必须幂等返回 nil，不能又发一个设备码。
	again, err := mgr.StartDeviceAuthorization("cliconn:demo", DeviceFlowConfig{
		DeviceAuthorizationEndpoint: srv.URL + "/device",
		TokenEndpoint:               srv.URL + "/token",
		ClientID:                    "cid",
	})
	if err != nil {
		t.Fatalf("second start: %v", err)
	}
	if again != nil {
		t.Fatalf("second start returned a new device code: %+v", again)
	}
}

// TestDeviceFlowDenied：用户拒绝时流程必须收尾为 failed，而不是一直挂着。
func TestDeviceFlowDenied(t *testing.T) {
	oldFloor := devicePollFloor
	devicePollFloor = 10 * time.Millisecond
	t.Cleanup(func() { devicePollFloor = oldFloor })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/device":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code": "dev-1", "user_code": "WXYZ-9876",
				"verification_uri": "https://example.com/device",
				"expires_in":       60, "interval": 1,
			})
		case "/token":
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "access_denied"})
		}
	}))
	defer srv.Close()

	mgr := NewOAuthManager(NewOAuthTokenStore(filepath.Join(t.TempDir(), "t.json")))
	if _, err := mgr.StartDeviceAuthorization("cliconn:demo", DeviceFlowConfig{
		DeviceAuthorizationEndpoint: srv.URL + "/device",
		TokenEndpoint:               srv.URL + "/token",
		ClientID:                    "cid",
	}); err != nil {
		t.Fatalf("start: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		st := mgr.Status("cliconn:demo")
		if st.State == OAuthStateFailed {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("denied flow did not fail, status = %+v", st)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
