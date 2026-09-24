package peer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStoreAddListRemove(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if store.Count() != 0 {
		t.Fatalf("expected empty store, got %d", store.Count())
	}

	// Add with a trailing slash is normalized.
	if err := store.Add(&Peer{Name: "Machine-B", BaseURL: "http://192.168.1.20:8642/"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// Case-insensitive lookup.
	p, ok := store.Get("machine-b")
	if !ok {
		t.Fatalf("Get(machine-b) not found")
	}
	if p.BaseURL != "http://192.168.1.20:8642" {
		t.Fatalf("BaseURL not normalized: %q", p.BaseURL)
	}
	if p.CreatedAt == 0 {
		t.Fatalf("CreatedAt not set")
	}

	if err := store.Add(&Peer{Name: "c", BaseURL: "https://peer.example.com"}); err != nil {
		t.Fatalf("Add c: %v", err)
	}
	if got := store.Count(); got != 2 {
		t.Fatalf("Count = %d, want 2", got)
	}
	list := store.List()
	if len(list) != 2 || list[0].Name != "Machine-B" || list[1].Name != "c" {
		t.Fatalf("List not sorted: %+v", list)
	}

	if err := store.Remove("MACHINE-B"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, ok := store.Get("machine-b"); ok {
		t.Fatalf("peer still present after Remove")
	}
	if err := store.Remove("nope"); err == nil {
		t.Fatalf("Remove of unknown peer should fail")
	}
}

func TestStoreInvalidAdd(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)
	cases := []struct{ name, base string }{
		{"", "http://x"},
		{"x", ""},
		{"x", "not-a-url"},
		{"x", "ftp://x"},
		{"x", "http://"},
	}
	for _, c := range cases {
		if err := store.Add(&Peer{Name: c.name, BaseURL: c.base}); err == nil {
			t.Fatalf("Add(%q, %q) should fail", c.name, c.base)
		}
	}
}

func TestStoreReload(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)
	if err := store.Add(&Peer{Name: "a", BaseURL: "http://10.0.0.1:8642", Token: "secret"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// A second store reading the same file must see the persisted peer (incl. token).
	store2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore reload: %v", err)
	}
	p, ok := store2.Get("a")
	if !ok {
		t.Fatalf("peer a missing after reload")
	}
	if p.Token != "secret" {
		t.Fatalf("token not persisted: %q", p.Token)
	}
}

func TestInstanceIDPersists(t *testing.T) {
	dir := t.TempDir()
	id1, err := InstanceID(dir)
	if err != nil {
		t.Fatalf("InstanceID: %v", err)
	}
	if !strings.Contains(id1, "-") {
		t.Fatalf("instance id %q missing separator", id1)
	}
	// File must exist; permission bits are only meaningful on Unix.
	fi, err := os.Stat(filepath.Join(dir, "instance_id"))
	if err != nil {
		t.Fatalf("instance_id file missing: %v", err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("instance_id mode = %o, want 600", fi.Mode().Perm())
	}

	id2, err := InstanceID(dir)
	if err != nil {
		t.Fatalf("InstanceID second call: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("instance id not stable: %q vs %q", id1, id2)
	}
}

// TestClientSendDMSuccess covers the outbound half of the relay: the request the
// remote instance receives (path, headers, payload) and the reply it returns.
func TestClientSendDMSuccess(t *testing.T) {
	var got DMRequest
	var gotPath, gotInstance, gotUA, gotCT string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotInstance = r.Header.Get("X-Peer-Instance")
		gotUA = r.Header.Get("User-Agent")
		gotCT = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("relay handler could not decode the body: %v", err)
		}
		json.NewEncoder(w).Encode(DMResponse{OK: true, Reply: "pong"})
	}))
	defer srv.Close()

	reply, err := NewClient().SendDM(
		context.Background(),
		&Peer{Name: "remote", BaseURL: srv.URL, Token: "shared"},
		"this-host-1a2b", "cli", "worker", "ping",
	)
	if err != nil {
		t.Fatalf("SendDM: %v", err)
	}
	if reply != "pong" {
		t.Errorf("reply = %q, want %q", reply, "pong")
	}

	if gotPath != "/api/relay/v1/dm" {
		t.Errorf("request path = %q, want /api/relay/v1/dm", gotPath)
	}
	if got.Instance != "this-host-1a2b" || got.From != "cli" || got.To != "worker" || got.Text != "ping" {
		t.Errorf("payload mismatch: %+v", got)
	}
	if got.Token != "shared" {
		t.Errorf("relay token not forwarded: %q", got.Token)
	}
	if gotInstance != "this-host-1a2b" {
		t.Errorf("X-Peer-Instance = %q, want the sender instance id", gotInstance)
	}
	if gotUA != "go-magic/peer" {
		t.Errorf("User-Agent = %q, want go-magic/peer", gotUA)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotCT)
	}
}

// TestClientSendDMErrors: a non-200 response and an application-level {ok:false}
// must both surface as errors carrying the remote message, never as an empty
// successful reply.
func TestClientSendDMErrors(t *testing.T) {
	t.Run("http status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"error":"invalid relay token"}`, http.StatusForbidden)
		}))
		defer srv.Close()

		_, err := NewClient().SendDM(context.Background(),
			&Peer{Name: "remote", BaseURL: srv.URL}, "me", "cli", "worker", "ping")
		if err == nil {
			t.Fatal("a 403 response should fail the DM")
		}
		if !strings.Contains(err.Error(), "403") {
			t.Errorf("error should mention the status code: %v", err)
		}
	})

	t.Run("application error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(DMResponse{OK: false, Error: "unknown bot: ghost"})
		}))
		defer srv.Close()

		_, err := NewClient().SendDM(context.Background(),
			&Peer{Name: "remote", BaseURL: srv.URL}, "me", "cli", "ghost", "ping")
		if err == nil {
			t.Fatal("{ok:false} should fail the DM")
		}
		if !strings.Contains(err.Error(), "unknown bot: ghost") {
			t.Errorf("remote error text lost: %v", err)
		}
	})

	t.Run("not json", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("<html>not the relay endpoint</html>"))
		}))
		defer srv.Close()

		// The relay endpoint is unauthenticated by design and may be fronted by
		// something else; a non-JSON 200 must be a clear error, not a silent "".
		if _, err := NewClient().SendDM(context.Background(),
			&Peer{Name: "remote", BaseURL: srv.URL}, "me", "cli", "worker", "ping"); err == nil {
			t.Fatal("a non-JSON response should fail the DM")
		}
	})
}
