package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/peer"
)

// peerDMTimeout bounds one dashboard-initiated relay DM. It mirrors the peer
// client's own HTTP timeout: a remote bot turn can legitimately run for minutes.
const peerDMTimeout = 6 * time.Minute

// peersHome returns the directory holding peers.json and instance_id.
func (s *Server) peersHome() string {
	if s.magicHome != "" {
		return s.magicHome
	}
	return peer.DefaultMagicHome()
}

// peerStore loads the peer table, failing the request when it is unreadable.
func (s *Server) peerStore(w http.ResponseWriter) (*peer.Store, bool) {
	store, err := peer.NewStore(s.peersHome())
	if err != nil {
		http.Error(w, "failed to load peer table: "+err.Error(), http.StatusInternalServerError)
		return nil, false
	}
	return store, true
}

// peerToResponse serializes one peer. The relay token is deliberately reduced
// to a boolean: it is a credential, and the dashboard has no need for its value
// (editing a peer is remove + add, which re-supplies the secret).
func peerToResponse(p *peer.Peer) map[string]interface{} {
	return map[string]interface{}{
		"name":       p.Name,
		"base_url":   p.BaseURL,
		"has_token":  p.Token != "",
		"created_at": p.CreatedAt,
	}
}

// handlePeers routes /api/peers (list, add).
func (s *Server) handlePeers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handlePeersList(w, r)
	case http.MethodPost:
		s.handlePeersAdd(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePeerByName routes /api/peers/{name}, /api/peers/{name}/dm,
// /api/peers/{name}/bots and /api/peers/{name}/ping.
func (s *Server) handlePeerByName(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/peers/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "peer name is required", http.StatusBadRequest)
		return
	}
	name := parts[0]

	switch {
	case len(parts) == 1 && r.Method == http.MethodDelete:
		s.handlePeerDelete(w, r, name)
	case len(parts) == 1 && (r.Method == http.MethodPut || r.Method == http.MethodPatch):
		s.handlePeerUpdate(w, r, name)
	case len(parts) == 2 && parts[1] == "bots" && r.Method == http.MethodGet:
		s.handlePeerBots(w, r, name)
	case len(parts) == 2 && parts[1] == "ping" && r.Method == http.MethodGet:
		s.handlePeerPing(w, r, name)
	case len(parts) == 2 && parts[1] == "dm" && r.Method == http.MethodPost:
		s.handlePeerDM(w, r, name)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// handlePeersList GET /api/peers — the peer table plus this instance's identity,
// which is what a remote operator needs to register this machine on their side.
func (s *Server) handlePeersList(w http.ResponseWriter, r *http.Request) {
	store, ok := s.peerStore(w)
	if !ok {
		return
	}

	instanceID, err := peer.InstanceID(s.peersHome())
	if err != nil {
		// Non-fatal: the peer list is still useful without it.
		instanceID = ""
	}

	peers := store.List()
	result := make([]map[string]interface{}, 0, len(peers))
	for _, p := range peers {
		result = append(result, peerToResponse(p))
	}
	jsonResponse(w, map[string]interface{}{
		"instance_id": instanceID,
		"magic_home":  s.peersHome(),
		"peers":       result,
	})
}

// handlePeersAdd POST /api/peers — register (or replace) a remote instance.
func (s *Server) handlePeersAdd(w http.ResponseWriter, r *http.Request) {
	store, ok := s.peerStore(w)
	if !ok {
		return
	}

	var req struct {
		Name    string `json:"name"`
		BaseURL string `json:"base_url"`
		Token   string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// "***" is the mask the dashboard renders for an existing token; storing it
	// verbatim would silently replace the real secret with three asterisks.
	if req.Token == maskedEnvValue {
		http.Error(w, "relay token looks like a mask; re-enter the actual secret", http.StatusBadRequest)
		return
	}

	p := &peer.Peer{
		Name:    strings.TrimSpace(req.Name),
		BaseURL: strings.TrimSpace(req.BaseURL),
		Token:   strings.TrimSpace(req.Token),
	}
	if err := store.Add(p); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	jsonResponse(w, peerToResponse(p))
}

// handlePeerDelete DELETE /api/peers/{name}
func (s *Server) handlePeerDelete(w http.ResponseWriter, r *http.Request, name string) {
	store, ok := s.peerStore(w)
	if !ok {
		return
	}
	if err := store.Remove(name); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	jsonResponse(w, map[string]interface{}{"deleted": name})
}

// handlePeerUpdate PUT|PATCH /api/peers/{name} — fix an address or rotate the
// relay secret without deleting and re-adding the peer. Deleting first (the
// only option before) also orphaned the dashboard's local conversation history,
// which is keyed by peer name.
func (s *Server) handlePeerUpdate(w http.ResponseWriter, r *http.Request, name string) {
	store, ok := s.peerStore(w)
	if !ok {
		return
	}
	if _, found := store.Get(name); !found {
		http.Error(w, "peer not found: "+name, http.StatusNotFound)
		return
	}

	var req struct {
		BaseURL string `json:"base_url"`
		Token   string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.BaseURL) == "" {
		http.Error(w, "base URL is required", http.StatusBadRequest)
		return
	}
	// Same guard as add: the dashboard renders "***" for a configured secret,
	// and round-tripping that value would overwrite the real token.
	if req.Token == maskedEnvValue {
		http.Error(w, "relay token looks like a mask; re-enter the actual secret", http.StatusBadRequest)
		return
	}

	updated, err := store.Update(name, req.BaseURL, req.Token)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	jsonResponse(w, peerToResponse(updated))
}

// handlePeerPing GET /api/peers/{name}/ping — is this peer reachable right now?
//
// The dashboard shows an online/offline dot per peer, which the DM path alone
// cannot provide (a peer that has never been messaged has no state at all).
// The probe reuses the bot listing, so one round trip yields reachability,
// latency and the remote bot count; a failure returns the remote's own reason
// as 502 (bad token vs. network need different fixes).
func (s *Server) handlePeerPing(w http.ResponseWriter, r *http.Request, name string) {
	store, ok := s.peerStore(w)
	if !ok {
		return
	}
	p, found := store.Get(name)
	if !found {
		http.Error(w, "peer not found: "+name, http.StatusNotFound)
		return
	}

	botCount, latency, err := peer.NewClient().Probe(r.Context(), p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	jsonResponse(w, map[string]interface{}{
		"ok":         true,
		"peer":       p.Name,
		"bot_count":  botCount,
		"latency_ms": latency.Milliseconds(),
	})
}

// handlePeerBots GET /api/peers/{name}/bots — proxy the peer's bot listing so
// the dashboard can offer a dropdown of remote bots instead of a free-text
// "type the bot name" field. Failures surface as 502 with the remote's own
// error text (e.g. "invalid relay token") so the operator can fix the peer
// entry rather than guess.
func (s *Server) handlePeerBots(w http.ResponseWriter, r *http.Request, name string) {
	store, ok := s.peerStore(w)
	if !ok {
		return
	}
	p, found := store.Get(name)
	if !found {
		http.Error(w, "peer not found: "+name, http.StatusNotFound)
		return
	}

	bots, err := peer.NewClient().ListBots(r.Context(), p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	jsonResponse(w, map[string]interface{}{
		"ok":   true,
		"peer": p.Name,
		"bots": bots,
	})
}

// handlePeerDM POST /api/peers/{name}/dm — relay a DM to a bot on a remote
// instance from the dashboard, so reachability can be verified without a shell.
func (s *Server) handlePeerDM(w http.ResponseWriter, r *http.Request, name string) {
	store, ok := s.peerStore(w)
	if !ok {
		return
	}
	p, found := store.Get(name)
	if !found {
		http.Error(w, "peer not found: "+name, http.StatusNotFound)
		return
	}

	var req struct {
		Bot     string `json:"bot"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Bot) == "" {
		http.Error(w, "target bot is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}

	instanceID, err := peer.InstanceID(s.peersHome())
	if err != nil {
		instanceID = "unknown"
	}

	ctx, cancel := context.WithTimeout(r.Context(), peerDMTimeout)
	defer cancel()

	reply, err := peer.NewClient().SendDM(ctx, p, instanceID, "dashboard", strings.TrimSpace(req.Bot), req.Message)
	if err != nil {
		// The remote instance (or the network) failed us, not the local request.
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	jsonResponse(w, map[string]interface{}{
		"ok":    true,
		"peer":  p.Name,
		"bot":   strings.TrimSpace(req.Bot),
		"reply": reply,
	})
}
