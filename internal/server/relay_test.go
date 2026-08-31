package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	appconfig "github.com/magicwubiao/go-magic/pkg/config"
)

// TestRelayDM_TokenMismatch: when bot_mode.relay_token is configured locally,
// a request with a wrong (or missing) token must be rejected with 403 before
// any bot work happens.
func TestRelayDM_TokenMismatch(t *testing.T) {
	s := &Server{cfg: &appconfig.Config{
		BotMode: &appconfig.BotModeConfig{Enabled: true, RelayToken: "sekret"},
	}}
	body := bytes.NewBufferString(`{"instance":"a-1","from":"cli","to":"worker","text":"hi","token":"wrong"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/relay/v1/dm", body)
	rec := httptest.NewRecorder()
	s.handleRelayDM(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestRelayDM_MissingTarget: with no relay token configured, a well-formed
// request must pass auth and fail at target-bot validation with 400.
func TestRelayDM_MissingTarget(t *testing.T) {
	s := &Server{cfg: &appconfig.Config{
		BotMode: &appconfig.BotModeConfig{Enabled: true},
	}}
	body := bytes.NewBufferString(`{"instance":"a-1","from":"cli","to":"","text":"hi"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/relay/v1/dm", body)
	rec := httptest.NewRecorder()
	s.handleRelayDM(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestRelayDM_BotModeDisabled: auth passes (no token configured) but bot mode
// is disabled -> the request must reach the bot manager and fail with 503.
func TestRelayDM_BotModeDisabled(t *testing.T) {
	s := &Server{cfg: &appconfig.Config{
		BotMode: &appconfig.BotModeConfig{Enabled: false},
	}}
	body := bytes.NewBufferString(`{"instance":"a-1","from":"cli","to":"worker","text":"hi"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/relay/v1/dm", body)
	rec := httptest.NewRecorder()
	s.handleRelayDM(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (body: %s)", rec.Code, rec.Body.String())
	}
}
