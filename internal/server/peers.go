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

// handlePeerByName routes /api/peers/{name} and /api/peers/{name}/dm.
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
