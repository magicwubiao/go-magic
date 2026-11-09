package bot

import (
	"context"
	"strings"
	"testing"
)

// TestParseRoomDirective locks the moderator control syntax: only the FIRST
// line is a directive, and it is removed from the text that gets broadcast.
func TestParseRoomDirective(t *testing.T) {
	cases := []struct {
		name     string
		reply    string
		wantBody string
		order    []string
		stop     bool
		clear    bool
		note     string
	}{
		{
			name:     "route order",
			reply:    "@route @alice, @bob\n先请两位对齐一下",
			wantBody: "先请两位对齐一下",
			order:    []string{"alice", "bob"},
		},
		{
			name:     "route order with chinese separators",
			reply:    "@route @alice、@bob\n开始",
			wantBody: "开始",
			order:    []string{"alice", "bob"},
		},
		{
			name:     "route stop",
			reply:    "@route stop\n今天先到这里",
			wantBody: "今天先到这里",
			stop:     true,
		},
		{
			name:     "route reset",
			reply:    "@route all",
			wantBody: "",
			clear:    true,
		},
		{
			name:     "note",
			reply:    "@note 结论：采用方案 B\n正文内容",
			wantBody: "正文内容",
			note:     "结论：采用方案 B",
		},
		{
			name:     "plain reply untouched",
			reply:    "我觉得可以，@route stop 只是协议说明",
			wantBody: "我觉得可以，@route stop 只是协议说明",
		},
		{
			// A member explaining the protocol must not trigger it.
			name:     "protocol mentioned on a later line",
			reply:    "可以用 @route stop 结束\n就这样",
			wantBody: "可以用 @route stop 结束\n就这样",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, d := parseRoomDirective(tc.reply)
			if body != tc.wantBody {
				t.Errorf("body = %q, want %q", body, tc.wantBody)
			}
			if d.Stop != tc.stop || d.Clear != tc.clear || d.Note != tc.note {
				t.Errorf("directive = %+v, want stop=%v clear=%v note=%q", d, tc.stop, tc.clear, tc.note)
			}
			if strings.Join(d.Order, ",") != strings.Join(tc.order, ",") {
				t.Errorf("order = %v, want %v", d.Order, tc.order)
			}
		})
	}
}

// TestFilterMembers drops names that are not room members (or repeat), so a
// typo in @route cannot shrink the round to zero speakers.
func TestFilterMembers(t *testing.T) {
	got := filterMembers([]string{"@bob", "carol", "alice", "BOB", ""}, []string{"alice", "bob"})
	if strings.Join(got, ",") != "bob,alice" {
		t.Fatalf("filterMembers = %v, want [bob alice]", got)
	}
	if out := filterMembers([]string{"carol"}, []string{"alice", "bob"}); len(out) != 0 {
		t.Fatalf("non-member order should be empty, got %v", out)
	}
}

// TestRoomHostOpensRound: the moderator speaks first even though alice is
// earlier in the member list.
func TestRoomHostOpensRound(t *testing.T) {
	mgr, llm, roomID, _ := newRoomRig(t, "收到", 1)
	if _, err := mgr.UpdateRoom(roomID, func(c *RoomConfig) { c.Host = "bob" }); err != nil {
		t.Fatalf("set host: %v", err)
	}

	if _, err := mgr.SendToRoom(context.Background(), roomID, "开始讨论", ""); err != nil {
		t.Fatalf("SendToRoom: %v", err)
	}
	reqs := llm.snapshot()
	if len(reqs) < 2 {
		t.Fatalf("expected both members to speak, got %d request(s)", len(reqs))
	}
	first := requestText(reqs[0])
	if !strings.Contains(first, "You are @bob.") {
		t.Fatalf("host did not open the round; first prompt was:\n%s", first)
	}
	if second := requestText(reqs[1]); !strings.Contains(second, "You are @alice.") {
		t.Fatalf("expected alice second, prompt was:\n%s", second)
	}
}

// TestRoomHostStopDirective: "@route stop" ends the discussion immediately and
// the control line never reaches the room log as speech.
func TestRoomHostStopDirective(t *testing.T) {
	mgr, llm, roomID, _ := newRoomRig(t, "@route stop\n今天先到这里", 3)
	if _, err := mgr.UpdateRoom(roomID, func(c *RoomConfig) { c.Host = "bob"; c.MaxRounds = 3 }); err != nil {
		t.Fatalf("set host: %v", err)
	}

	if _, err := mgr.SendToRoom(context.Background(), roomID, "开个长会", ""); err != nil {
		t.Fatalf("SendToRoom: %v", err)
	}
	if got := len(llm.snapshot()); got != 1 {
		t.Fatalf("stop directive should end the round after the host, got %d requests", got)
	}
	msgs, err := mgr.RoomMessages(roomID)
	if err != nil {
		t.Fatalf("RoomMessages: %v", err)
	}
	var sawNotice, sawClean bool
	for _, m := range msgs {
		if strings.Contains(m.Content, "@route") {
			t.Errorf("control line leaked into the room log: %q", m.Content)
		}
		if m.From == "@system" && strings.Contains(m.Content, "结束") {
			sawNotice = true
		}
		if m.From == "bob" && m.Content == "今天先到这里" {
			sawClean = true
		}
	}
	if !sawNotice {
		t.Error("no system notice about the moderator ending the round")
	}
	if !sawClean {
		t.Error("host's actual reply is missing from the room log")
	}
}

// TestRoomNoteDirectivePersists: "@note" appends to the shared notes and is
// stripped from the broadcast reply.
func TestRoomNoteDirectivePersists(t *testing.T) {
	mgr, _, roomID, _ := newRoomRig(t, "@note 结论：采用方案B\n我同意方案B", 1)

	if _, err := mgr.SendToRoom(context.Background(), roomID, "定一下方案", ""); err != nil {
		t.Fatalf("SendToRoom: %v", err)
	}
	notes, err := mgr.RoomNotes(roomID)
	if err != nil {
		t.Fatalf("RoomNotes: %v", err)
	}
	if !strings.Contains(notes, "结论：采用方案B") {
		t.Fatalf("note missing from shared notes: %q", notes)
	}
	if !strings.Contains(notes, "@alice") {
		t.Fatalf("note should carry its author: %q", notes)
	}
	msgs, err := mgr.RoomMessages(roomID)
	if err != nil {
		t.Fatalf("RoomMessages: %v", err)
	}
	for _, m := range msgs {
		if strings.Contains(m.Content, "@note") {
			t.Errorf("note directive leaked into the room log: %q", m.Content)
		}
	}
}

// TestRoomNotesRoundTrip covers the store/manager surface used by the API.
func TestRoomNotesRoundTrip(t *testing.T) {
	mgr, _, roomID, _ := newRoomRig(t, "收到", 1)

	notes, err := mgr.RoomNotes(roomID)
	if err != nil || notes != "" {
		t.Fatalf("fresh room notes = %q, %v; want empty", notes, err)
	}
	if _, err := mgr.SaveRoomNotes(roomID, "  约定：先评审再合并  \n"); err != nil {
		t.Fatalf("SaveRoomNotes: %v", err)
	}
	got, err := mgr.RoomNotes(roomID)
	if err != nil {
		t.Fatalf("RoomNotes: %v", err)
	}
	if got != "约定：先评审再合并" {
		t.Fatalf("notes = %q, want the trimmed content", got)
	}

	room, err := mgr.GetRoom(roomID)
	if err != nil {
		t.Fatal(err)
	}
	prompt := mgr.buildRoomPrompt(room, "alice", nil, 0, 1)
	if !strings.Contains(prompt, "约定：先评审再合并") {
		t.Fatalf("notes are missing from the member prompt:\n%s", prompt)
	}

	if err := mgr.DeleteRoom(roomID); err != nil {
		t.Fatalf("DeleteRoom: %v", err)
	}
	if _, err := mgr.RoomNotes(roomID); err == nil {
		t.Fatal("notes survived room deletion")
	}
}
