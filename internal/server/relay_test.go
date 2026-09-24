package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	appconfig "github.com/magicwubiao/go-magic/pkg/config"
)

// loopbackRemoteAddr marks a request as coming from the local machine, which is
// what the relay endpoint requires when no token is configured.
const loopbackRemoteAddr = "127.0.0.1:54321"

// newRelayRequest builds a relay POST with an explicit peer address. RemoteAddr
// matters: it is the only trusted source of the caller's identity, and the
// no-token policy keys off it (httptest defaults to 192.0.2.1, i.e. "remote").
func newRelayRequest(body, remoteAddr string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/relay/v1/dm", bytes.NewBufferString(body))
	req.RemoteAddr = remoteAddr
	return req
}

func relayServer(cfg *appconfig.BotModeConfig) *Server {
	return &Server{cfg: &appconfig.Config{BotMode: cfg}}
}

// TestRelayDM_TokenMismatch: when bot_mode.relay_token is configured locally,
// a request with a wrong (or missing) token must be rejected with 403 before
// any bot work happens — even from localhost.
func TestRelayDM_TokenMismatch(t *testing.T) {
	s := relayServer(&appconfig.BotModeConfig{Enabled: true, RelayToken: "sekret"})
	req := newRelayRequest(`{"instance":"a-1","from":"cli","to":"worker","text":"hi","token":"wrong"}`, loopbackRemoteAddr)
	rec := httptest.NewRecorder()
	s.handleRelayDM(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestRelayDM_AnonymousRemoteRejected: with no relay token configured, a
// non-loopback caller must be refused. This is the security-relevant default:
// an unauthenticated relay reachable from the network lets anyone drive this
// instance's bots and burn its model budget.
func TestRelayDM_AnonymousRemoteRejected(t *testing.T) {
	s := relayServer(&appconfig.BotModeConfig{Enabled: true})
	req := newRelayRequest(`{"instance":"a-1","from":"cli","to":"worker","text":"hi"}`, "203.0.113.9:40000")
	rec := httptest.NewRecorder()
	s.handleRelayDM(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for anonymous remote relay (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestRelayDM_MissingTarget: with no relay token configured, a well-formed
// loopback request must pass auth and fail at target-bot validation with 400.
func TestRelayDM_MissingTarget(t *testing.T) {
	s := relayServer(&appconfig.BotModeConfig{Enabled: true})
	req := newRelayRequest(`{"instance":"a-1","from":"cli","to":"","text":"hi"}`, loopbackRemoteAddr)
	rec := httptest.NewRecorder()
	s.handleRelayDM(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestRelayDM_BotModeDisabled: auth passes (loopback, no token configured) but
// bot mode is disabled -> the request must reach the bot manager and fail 503.
func TestRelayDM_BotModeDisabled(t *testing.T) {
	s := relayServer(&appconfig.BotModeConfig{Enabled: false})
	req := newRelayRequest(`{"instance":"a-1","from":"cli","to":"worker","text":"hi"}`, loopbackRemoteAddr)
	rec := httptest.NewRecorder()
	s.handleRelayDM(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestRelayDM_RateLimited: a caller that keeps hammering the endpoint is
// throttled with 429 instead of being allowed to queue unbounded bot turns.
func TestRelayDM_RateLimited(t *testing.T) {
	s := relayServer(&appconfig.BotModeConfig{Enabled: true})
	body := `{"instance":"a-1","from":"cli","to":"","text":"hi"}`

	for i := 0; i < relayMaxPerWindow; i++ {
		rec := httptest.NewRecorder()
		s.handleRelayDM(rec, newRelayRequest(body, loopbackRemoteAddr))
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d/%d was throttled too early", i+1, relayMaxPerWindow)
		}
	}

	rec := httptest.NewRecorder()
	s.handleRelayDM(rec, newRelayRequest(body, loopbackRemoteAddr))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 after %d requests from one IP", rec.Code, relayMaxPerWindow)
	}
}

// TestRelayDM_RateLimitIsPerIP: one noisy peer must not throttle a different
// one — the limiter is keyed by remote IP.
func TestRelayDM_RateLimitIsPerIP(t *testing.T) {
	s := relayServer(&appconfig.BotModeConfig{Enabled: true})
	body := `{"instance":"a-1","from":"cli","to":"","text":"hi"}`

	for i := 0; i < relayMaxPerWindow; i++ {
		rec := httptest.NewRecorder()
		s.handleRelayDM(rec, newRelayRequest(body, loopbackRemoteAddr))
	}

	rec := httptest.NewRecorder()
	s.handleRelayDM(rec, newRelayRequest(body, "127.0.0.2:54321"))
	if rec.Code == http.StatusTooManyRequests {
		t.Fatal("a second source IP must have its own budget")
	}
}
