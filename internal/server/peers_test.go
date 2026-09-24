package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newPeerServer builds a Server whose peer table lives in a temp magic home.
func newPeerServer(t *testing.T) *Server {
	t.Helper()
	return &Server{magicHome: t.TempDir()}
}

// peerReq issues one request against the peer endpoints, dispatching to the same
// handlers the router wires up (/api/peers vs /api/peers/{name}[/dm]).
func (s *Server) peerReq(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, bytes.NewBufferString(body))
	}
	rec := httptest.NewRecorder()
	if path == "/api/peers" {
		s.handlePeers(rec, req)
	} else {
		s.handlePeerByName(rec, req)
	}
	return rec
}

// TestPeersCRUD covers the peer table endpoints the dashboard uses.
func TestPeersCRUD(t *testing.T) {
	s := newPeerServer(t)

	// Empty table, but the instance identity is still reported.
	rec := s.peerReq(t, http.MethodGet, "/api/peers", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/peers = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var overview struct {
		InstanceID string                   `json:"instance_id"`
		MagicHome  string                   `json:"magic_home"`
		Peers      []map[string]interface{} `json:"peers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &overview); err != nil {
		t.Fatalf("decode overview: %v (%s)", err, rec.Body.String())
	}
	if overview.InstanceID == "" {
		t.Error("instance_id should be reported so a remote operator can register this machine")
	}
	if overview.MagicHome != s.magicHome {
		t.Errorf("magic_home = %q, want %q", overview.MagicHome, s.magicHome)
	}
	if len(overview.Peers) != 0 {
		t.Errorf("fresh peer table is not empty: %v", overview.Peers)
	}

	// Add.
	rec = s.peerReq(t, http.MethodPost, "/api/peers",
		`{"name":"lab-b","base_url":"http://192.168.1.20:8642","token":"shared"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/peers = %d, want 201 (body: %s)", rec.Code, rec.Body.String())
	}
	var added map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &added); err != nil {
		t.Fatal(err)
	}
	if added["name"] != "lab-b" || added["has_token"] != true {
		t.Errorf("unexpected add response: %v", added)
	}

	// The relay token must never be serialized back to the dashboard.
	rec = s.peerReq(t, http.MethodGet, "/api/peers", "")
	if strings.Contains(rec.Body.String(), "shared") {
		t.Errorf("peer token leaked to the dashboard: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "lab-b") {
		t.Errorf("added peer missing from the list: %s", rec.Body.String())
	}

	// Invalid peer is rejected by the store's validation.
	rec = s.peerReq(t, http.MethodPost, "/api/peers", `{"name":"bad","base_url":"not-a-url"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("POST with a bad URL = %d, want 400", rec.Code)
	}

	// The bot-env mask must not be stored as a literal secret.
	rec = s.peerReq(t, http.MethodPost, "/api/peers",
		`{"name":"masked","base_url":"http://host:1","token":"`+maskedEnvValue+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("POST with a masked token = %d, want 400", rec.Code)
	}

	// Delete, then delete again (now unknown).
	if rec = s.peerReq(t, http.MethodDelete, "/api/peers/lab-b", ""); rec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/peers/lab-b = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if rec = s.peerReq(t, http.MethodDelete, "/api/peers/lab-b", ""); rec.Code != http.StatusNotFound {
		t.Errorf("deleting a missing peer = %d, want 404", rec.Code)
	}
}

// TestPeersRoutingAndMethodGuards locks in the subroute dispatch.
func TestPeersRoutingAndMethodGuards(t *testing.T) {
	s := newPeerServer(t)

	if rec := s.peerReq(t, http.MethodPut, "/api/peers", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("PUT /api/peers = %d, want 405", rec.Code)
	}
	if rec := s.peerReq(t, http.MethodGet, "/api/peers/", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("GET /api/peers/ = %d, want 400 (missing name)", rec.Code)
	}
	if rec := s.peerReq(t, http.MethodPost, "/api/peers/lab-b/dm", `{"bot":"x","message":"hi"}`); rec.Code != http.StatusNotFound {
		t.Errorf("DM to an unknown peer = %d, want 404", rec.Code)
	}
	if rec := s.peerReq(t, http.MethodGet, "/api/peers/lab-b", ""); rec.Code != http.StatusNotFound {
		t.Errorf("unsupported subroute = %d, want 404", rec.Code)
	}
}

// PeerDMResponseBody mirrors the DM endpoint's success payload.
type PeerDMResponseBody struct {
	OK    bool   `json:"ok"`
	Peer  string `json:"peer"`
	Bot   string `json:"bot"`
	Reply string `json:"reply"`
}

// TestPeerDMRelaysToRemoteInstance drives the dashboard DM endpoint against a
// stub relay server, so the whole path (lookup -> client -> relay POST) is
// exercised without needing a second go-magic instance.
func TestPeerDMRelaysToRemoteInstance(t *testing.T) {
	var got map[string]string
	var gotPath string
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "reply": "roger that"})
	}))
	defer relay.Close()

	s := newPeerServer(t)
	if rec := s.peerReq(t, http.MethodPost, "/api/peers",
		`{"name":"lab","base_url":"`+relay.URL+`","token":"shared"}`); rec.Code != http.StatusCreated {
		t.Fatalf("add peer = %d (body: %s)", rec.Code, rec.Body.String())
	}

	rec := s.peerReq(t, http.MethodPost, "/api/peers/lab/dm", `{"bot":"researcher","message":"status?"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/peers/lab/dm = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var out PeerDMResponseBody
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode dm response: %v (%s)", err, rec.Body.String())
	}
	if !out.OK || out.Reply != "roger that" || out.Bot != "researcher" {
		t.Errorf("unexpected dm response: %+v", out)
	}

	if gotPath != "/api/relay/v1/dm" {
		t.Errorf("relayed to %q, want /api/relay/v1/dm", gotPath)
	}
	if got["to"] != "researcher" || got["text"] != "status?" || got["token"] != "shared" {
		t.Errorf("relayed payload mismatch: %+v", got)
	}
	if got["from"] != "dashboard" {
		t.Errorf("relayed sender = %q, want dashboard", got["from"])
	}
	if got["instance"] == "" {
		t.Error("relayed request should carry this instance's id")
	}

	// Both fields are required.
	if rec := s.peerReq(t, http.MethodPost, "/api/peers/lab/dm", `{"bot":"","message":"hi"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("DM without a target bot = %d, want 400", rec.Code)
	}
	if rec := s.peerReq(t, http.MethodPost, "/api/peers/lab/dm", `{"bot":"x","message":"  "}`); rec.Code != http.StatusBadRequest {
		t.Errorf("DM without a message = %d, want 400", rec.Code)
	}
}

// TestPeerDMReportsRemoteFailure: when the remote instance refuses or is down,
// the dashboard must get a 502 carrying the reason (not a 200 with ok:false).
func TestPeerDMReportsRemoteFailure(t *testing.T) {
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"this instance has no bot_mode.relay_token configured"}`, http.StatusForbidden)
	}))
	defer relay.Close()

	s := newPeerServer(t)
	if rec := s.peerReq(t, http.MethodPost, "/api/peers",
		`{"name":"lab","base_url":"`+relay.URL+`"}`); rec.Code != http.StatusCreated {
		t.Fatalf("add peer = %d", rec.Code)
	}

	rec := s.peerReq(t, http.MethodPost, "/api/peers/lab/dm", `{"bot":"researcher","message":"status?"}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("remote failure = %d, want 502 (body: %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "403") {
		t.Errorf("the upstream status should be surfaced: %s", rec.Body.String())
	}
}
