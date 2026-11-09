package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/magicwubiao/go-magic/internal/bot"
	"github.com/magicwubiao/go-magic/pkg/config"
)

// newRoomFeatureServer builds a Server backed by a temp magic home with Bot
// Mode running (alice + bob) and no provider calls involved.
func newRoomFeatureServer(t *testing.T) *Server {
	t.Helper()
	home := t.TempDir()
	cfgData := map[string]interface{}{
		"provider": "custom",
		"providers": map[string]interface{}{
			"custom": map[string]interface{}{
				"api_key": "test-key",
				"models":  []string{"mock-model"},
			},
		},
		"bot_mode":    map[string]interface{}{"enabled": true},
		"working_dir": t.TempDir(),
	}
	raw, _ := json.Marshal(cfgData)
	if err := os.WriteFile(filepath.Join(home, "config.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GO_MAGIC_HOME", home)

	store, err := bot.NewStore(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"alice", "bob"} {
		if err := store.Save(&bot.Config{Name: n, Title: n}); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	mgr, err := bot.NewManager(cfg)
	if err != nil || mgr == nil {
		t.Fatalf("bot manager: %v", err)
	}
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("bot manager start: %v", err)
	}
	t.Cleanup(mgr.Stop)

	s := &Server{magicHome: home}
	s.botManager = mgr
	s.botModeStarted = true
	// Deleting a room opens uploads.db for metadata cleanup; on Windows an open
	// handle blocks TempDir removal, so close it before the test ends.
	t.Cleanup(func() {
		if s.uploadsMeta != nil {
			_ = s.uploadsMeta.Close()
			s.uploadsMeta = nil
		}
	})
	return s
}

// roomReq dispatches one request through the same handlers the router uses.
func (s *Server) roomReq(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, bytes.NewBufferString(body))
	}
	rec := httptest.NewRecorder()
	if path == "/api/rooms" {
		s.handleRooms(rec, req)
	} else {
		s.handleRoomByID(rec, req)
	}
	return rec
}

func decodeRoom(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode room: %v (%s)", err, rec.Body.String())
	}
	return out
}

// TestRoomFeatureEndpoints walks the notes / schedules / moderator surface the
// dashboard uses, end to end through the HTTP handlers.
func TestRoomFeatureEndpoints(t *testing.T) {
	s := newRoomFeatureServer(t)

	rec := s.roomReq(t, http.MethodPost, "/api/rooms", `{"name":"评审","members":["alice","bob"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create room = %d (%s)", rec.Code, rec.Body.String())
	}
	room := decodeRoom(t, rec)
	id, _ := room["id"].(string)
	if id == "" {
		t.Fatal("created room has no id")
	}
	base := "/api/rooms/" + id

	// --- moderator ---
	rec = s.roomReq(t, http.MethodPut, base, `{"host":"bob"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("set host = %d (%s)", rec.Code, rec.Body.String())
	}
	if room = decodeRoom(t, rec); room["host"] != "bob" {
		t.Fatalf("host = %v, want bob", room["host"])
	}
	if room["auto_route"] != true {
		t.Fatalf("auto_route = %v, want true by default", room["auto_route"])
	}
	rec = s.roomReq(t, http.MethodPut, base, `{"host":"ghost"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("host outside members = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}

	// --- notes ---
	rec = s.roomReq(t, http.MethodGet, base+"/notes", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get notes = %d (%s)", rec.Code, rec.Body.String())
	}
	rec = s.roomReq(t, http.MethodPut, base+"/notes", `{"content":"约定：先评审再合并"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("put notes = %d (%s)", rec.Code, rec.Body.String())
	}
	var notes struct {
		Content string `json:"content"`
		Limit   int    `json:"limit"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &notes)
	if notes.Content != "约定：先评审再合并" || notes.Limit <= 0 {
		t.Fatalf("notes response = %+v", notes)
	}

	// --- schedules ---
	rec = s.roomReq(t, http.MethodPost, base+"/schedules",
		`{"name":"早会","schedule":"0 9 * * *","prompt":"汇报进度","target":"@alice"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create schedule = %d (%s)", rec.Code, rec.Body.String())
	}
	room = decodeRoom(t, rec)
	schedules, _ := room["schedules"].([]interface{})
	if len(schedules) != 1 {
		t.Fatalf("schedules = %v, want 1", room["schedules"])
	}
	sc := schedules[0].(map[string]interface{})
	sid, _ := sc["id"].(string)
	if sc["enabled"] != true || sc["target"] != "alice" {
		t.Fatalf("schedule wire = %+v", sc)
	}

	rec = s.roomReq(t, http.MethodPatch, base+"/schedules/"+sid, `{"enabled":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("pause schedule = %d (%s)", rec.Code, rec.Body.String())
	}
	room = decodeRoom(t, rec)
	schedules, _ = room["schedules"].([]interface{})
	if sc = schedules[0].(map[string]interface{}); sc["enabled"] != false {
		t.Fatalf("schedule still enabled after pause: %+v", sc)
	}

	rec = s.roomReq(t, http.MethodPatch, base+"/schedules/"+sid, `{"schedule":"not a cron"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid cron = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}

	rec = s.roomReq(t, http.MethodDelete, base+"/schedules/"+sid, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete schedule = %d (%s)", rec.Code, rec.Body.String())
	}
	room = decodeRoom(t, rec)
	if schedules, _ = room["schedules"].([]interface{}); len(schedules) != 0 {
		t.Fatalf("schedules = %v after delete, want none", room["schedules"])
	}

	// --- delete room (also drops its notes) ---
	if rec = s.roomReq(t, http.MethodDelete, base, ""); rec.Code != http.StatusOK {
		t.Fatalf("delete room = %d (%s)", rec.Code, rec.Body.String())
	}
	if rec = s.roomReq(t, http.MethodGet, base+"/notes", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("notes after room delete = %d, want 404", rec.Code)
	}
}
