package gateway

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeWeComQRServer 返回企微官方真实报文形状的响应，并记录收到的请求。
func fakeWeComQRServer(t *testing.T, payload string, status int) (*httptest.Server, func() *http.Request) {
	t.Helper()
	var last *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		last = r
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, payload)
	}))
	t.Cleanup(srv.Close)
	return srv, func() *http.Request { return last }
}

func TestGenerateWeComAIBotQR(t *testing.T) {
	const body = `{"data":{"scode":"s_abc123","auth_url":"https://work.weixin.qq.com/ai/qc/c?s=s_abc123&for_native=true"}}`
	srv, getReq := fakeWeComQRServer(t, body, http.StatusOK)

	old := wecomAIQRGenerateURL
	wecomAIQRGenerateURL = srv.URL
	defer func() { wecomAIQRGenerateURL = old }()

	authURL, scode, err := GenerateWeComAIBotQR(context.Background())
	if err != nil {
		t.Fatalf("GenerateWeComAIBotQR: %v", err)
	}
	if scode != "s_abc123" {
		t.Fatalf("scode = %q, want s_abc123", scode)
	}
	if authURL != "https://work.weixin.qq.com/ai/qc/c?s=s_abc123&for_native=true" {
		t.Fatalf("auth_url = %q", authURL)
	}

	r := getReq()
	if r.Method != http.MethodGet {
		t.Fatalf("method = %s, want GET (POST returns 404 from the real endpoint)", r.Method)
	}
	if r.URL.RawQuery != "" {
		t.Fatalf("raw query = %q, want empty (query params break the real endpoint)", r.URL.RawQuery)
	}
	if r.Body != nil && r.ContentLength != 0 {
		t.Fatalf("unexpected request body")
	}
	if ua := r.Header.Get("User-Agent"); ua == "" || ua == "Go-http-client/1.1" {
		t.Fatalf("User-Agent = %q, want a browser UA", ua)
	}
}

func TestGenerateWeComAIBotQRNon200(t *testing.T) {
	srv, _ := fakeWeComQRServer(t, "<html>404</html>", http.StatusNotFound)
	old := wecomAIQRGenerateURL
	wecomAIQRGenerateURL = srv.URL
	defer func() { wecomAIQRGenerateURL = old }()

	_, _, err := GenerateWeComAIBotQR(context.Background())
	if err == nil {
		t.Fatalf("expected error on 404")
	}
}

func TestPollWeComAIBotQR(t *testing.T) {
	cases := []struct {
		name      string
		payload   string
		wantStat  string
		wantBotID string
		wantSec   string
	}{
		{"init", `{"data":{"status":"init"}}`, "wait", "", ""},
		{"wait", `{"data":{"status":"wait"}}`, "wait", "", ""},
		{"scaned", `{"data":{"status":"scaned"}}`, "scaned", "", ""},
		{"expired", `{"data":{"status":"expired"}}`, "expired", "", ""},
		{"success", `{"data":{"status":"success","bot_info":{"botid":"bot_9","secret":"sec_9"}}}`, "success", "bot_9", "sec_9"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv, getReq := fakeWeComQRServer(t, c.payload, http.StatusOK)
			old := wecomAIQRPollURL
			wecomAIQRPollURL = srv.URL
			defer func() { wecomAIQRPollURL = old }()

			status, botID, secret, err := PollWeComAIBotQR(context.Background(), "s_abc123")
			if err != nil {
				t.Fatalf("PollWeComAIBotQR: %v", err)
			}
			if status != c.wantStat {
				t.Fatalf("status = %q, want %q", status, c.wantStat)
			}
			if botID != c.wantBotID || secret != c.wantSec {
				t.Fatalf("bot credentials = (%q,%q), want (%q,%q)", botID, secret, c.wantBotID, c.wantSec)
			}
			if q := getReq().URL.Query().Get("scode"); q != "s_abc123" {
				t.Fatalf("scode query = %q, want s_abc123", q)
			}
		})
	}
}
