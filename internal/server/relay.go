package server

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/magicwubiao/go-magic/internal/peer"
)

// handleRelayDM POST /api/relay/v1/dm
//
// Cross-machine Bot Mode relay endpoint: accepts a DM from a remote go-magic
// instance ("peer"), synchronously drives the target bot through one turn and
// returns the reply. Authentication is NOT the dashboard auth token (remote
// instances don't have it); instead the request carries the optional relay
// secret in its body and is validated here:
//
//   - bot_mode.relay_token empty  -> anonymous requests accepted (trusted nets only)
//   - bot_mode.relay_token set    -> request Token must match (constant-time)
func (s *Server) handleRelayDM(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	var req peer.DMRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid relay request"}`, http.StatusBadRequest)
		return
	}

	// Relay secret check (enforced only when configured locally).
	if token := s.relayToken(); token != "" {
		if subtle.ConstantTimeCompare([]byte(req.Token), []byte(token)) != 1 {
			http.Error(w, `{"error":"invalid relay token"}`, http.StatusForbidden)
			return
		}
	}

	if strings.TrimSpace(req.To) == "" {
		http.Error(w, `{"error":"target bot is required"}`, http.StatusBadRequest)
		return
	}

	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}

	// Prefix the message with the sender identity so the remote bot knows who
	// is talking (e.g. "[relay cli@machine-a-1a2b3c4d5e6f] hello").
	sender := "remote"
	if req.From != "" {
		sender = req.From
	}
	if req.Instance != "" {
		sender = sender + "@" + req.Instance
	}
	text := req.Text
	if text != "" {
		text = fmt.Sprintf("[relay %s] %s", sender, req.Text)
	}

	reply, err := mgr.SendToBot(req.To, text)
	if err != nil {
		jsonResponse(w, peer.DMResponse{OK: false, Error: err.Error()})
		return
	}
	jsonResponse(w, peer.DMResponse{OK: true, Reply: reply})
}

// relayToken returns the configured relay secret (bot_mode.relay_token).
func (s *Server) relayToken() string {
	if s.cfg != nil && s.cfg.BotMode != nil {
		return s.cfg.BotMode.RelayToken
	}
	return ""
}
