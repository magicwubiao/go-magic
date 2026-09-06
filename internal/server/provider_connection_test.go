package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appconfig "github.com/magicwubiao/go-magic/pkg/config"
)

// fakeOpenAI spins up a minimal OpenAI-compatible /chat/completions endpoint
// so provider tests never touch the real internet.
func fakeOpenAI(t *testing.T, status int) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"choices": []map[string]interface{}{{"index": 0, "message": map[string]string{"role": "assistant", "content": "pong"}}},
		})
	}))
	t.Cleanup(ts.Close)
	return ts
}

func postTestRequest(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/providers/custom/test", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleProvidersSubRoutes(rr, req)
	return rr
}

func TestProviderTestConnectionOK(t *testing.T) {
	ts := fakeOpenAI(t, http.StatusOK)
	s := &Server{cfg: &appconfig.Config{
		Providers: map[string]appconfig.ProviderConfig{
			"custom": {APIKey: "sk-live", BaseURL: ts.URL, Models: []string{"test-model"}},
		},
	}}

	rr := postTestRequest(t, s, `{}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var out struct {
		OK         bool   `json:"ok"`
		Model      string `json:"model"`
		LatencyMs  int64  `json:"latencyMs"`
		ReplyChars int    `json:"replyChars"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if !out.OK {
		t.Fatalf("expected ok=true, got %s", rr.Body.String())
	}
	if out.Model != "test-model" {
		t.Fatalf("model = %q, want test-model", out.Model)
	}
}

func TestProviderTestConnectionFormOverride(t *testing.T) {
	// No saved config at all — the form values (base_url) must be used.
	ts := fakeOpenAI(t, http.StatusOK)
	s := &Server{cfg: &appconfig.Config{}}

	body := `{"api_key":"sk-form","base_url":"` + ts.URL + `","model":"form-model"}`
	rr := postTestRequest(t, s, body)
	var out struct {
		OK    bool   `json:"ok"`
		Model string `json:"model"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &out)
	if !out.OK {
		t.Fatalf("form override test failed: %s", rr.Body.String())
	}
	if out.Model != "form-model" {
		t.Fatalf("model = %q, want form-model", out.Model)
	}
}

func TestProviderTestConnectionUpstreamError(t *testing.T) {
	// 401 from upstream → the handler must surface a failed test (not 500),
	// so the UI can show the provider-side auth error.
	ts := fakeOpenAI(t, http.StatusUnauthorized)
	s := &Server{cfg: &appconfig.Config{
		Providers: map[string]appconfig.ProviderConfig{
			"custom": {APIKey: "sk-bad", BaseURL: ts.URL, Models: []string{"test-model"}},
		},
	}}

	rr := postTestRequest(t, s, `{}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("handler should return 200 with ok=false, got %d", rr.Code)
	}
	var out struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &out)
	if out.OK {
		t.Fatal("expected ok=false for upstream 401")
	}
	if out.Error == "" {
		t.Fatal("expected a non-empty error message")
	}
}

func TestProviderTestConnectionNoModel(t *testing.T) {
	s := &Server{cfg: &appconfig.Config{
		Providers: map[string]appconfig.ProviderConfig{
			"custom": {APIKey: "sk-x"},
		},
	}}

	rr := postTestRequest(t, s, `{}`)
	var out struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &out)
	if out.OK {
		t.Fatal("expected ok=false when no model is configured")
	}
}

func putProvider(t *testing.T, s *Server, body string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/providers/custom", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleProvidersSubRoutes(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", rr.Code, rr.Body.String())
	}
}

// vision declaration must survive the update round-trip in all three states:
// true/false set it, null clears it back to auto-detection.
func TestProviderUpdateVision(t *testing.T) {
	s := &Server{cfg: &appconfig.Config{
		Providers: map[string]appconfig.ProviderConfig{
			"custom": {APIKey: "sk-live", Models: []string{"m1"}},
		},
	}}

	putProvider(t, s, `{"vision": true}`)
	if s.cfg.Providers["custom"].Vision == nil || !*s.cfg.Providers["custom"].Vision {
		t.Fatalf("vision=true not saved: %+v", s.cfg.Providers["custom"].Vision)
	}

	putProvider(t, s, `{"vision": false}`)
	if s.cfg.Providers["custom"].Vision == nil || *s.cfg.Providers["custom"].Vision {
		t.Fatalf("vision=false not saved: %+v", s.cfg.Providers["custom"].Vision)
	}

	putProvider(t, s, `{"vision": null}`)
	if s.cfg.Providers["custom"].Vision != nil {
		t.Fatalf("vision=null should clear the declaration, got %+v", *s.cfg.Providers["custom"].Vision)
	}

	// Key absent → stored value untouched.
	putProvider(t, s, `{"vision": true}`)
	putProvider(t, s, `{"api_key": "sk-new"}`)
	if s.cfg.Providers["custom"].Vision == nil || !*s.cfg.Providers["custom"].Vision {
		t.Fatal("vision should be untouched when key absent")
	}
}
