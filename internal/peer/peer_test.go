package peer

import (
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
