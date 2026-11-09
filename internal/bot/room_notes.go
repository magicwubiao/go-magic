package bot

import (
	"fmt"
	"strings"
	"time"
)

// Shared room notes (群共享笔记).
//
// A room's notes are a small markdown file every member reads on every turn:
// the room's standing decisions, agreed conventions, open questions. They are
// deliberately NOT part of the message log — the log is capped (HistoryCap) and
// trimmed, so anything written 600 messages ago silently disappears. Notes
// survive that, and they are also the only place a human can correct the room's
// shared memory without pretending to be a participant.

// RoomNotes returns a room's shared notes ("" when the room has none yet).
func (m *Manager) RoomNotes(roomID string) (string, error) {
	key := strings.ToLower(strings.TrimSpace(roomID))
	if _, err := m.store.LoadRoom(key); err != nil {
		return "", fmt.Errorf("room not found: %s", key)
	}
	notes, err := m.store.LoadRoomNotes(key)
	if err != nil {
		return "", fmt.Errorf("failed to read room notes: %w", err)
	}
	return notes, nil
}

// SaveRoomNotes replaces a room's shared notes. Returns the stored text (the
// server trims trailing whitespace and enforces the size cap).
func (m *Manager) SaveRoomNotes(roomID, content string) (string, error) {
	key := strings.ToLower(strings.TrimSpace(roomID))
	if _, err := m.store.LoadRoom(key); err != nil {
		return "", fmt.Errorf("room not found: %s", key)
	}
	content = strings.TrimSpace(content)
	m.notesMu.Lock()
	defer m.notesMu.Unlock()
	if err := m.store.SaveRoomNotes(key, content); err != nil {
		return "", err
	}
	return content, nil
}

// appendRoomNote adds one attributed bullet to a room's notes. Used by the
// @note routing directive: a member can persist a conclusion without the human
// having to open the notes editor.
func (m *Manager) appendRoomNote(roomID, author, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	key := strings.ToLower(strings.TrimSpace(roomID))
	m.notesMu.Lock()
	defer m.notesMu.Unlock()

	notes, err := m.store.LoadRoomNotes(key)
	if err != nil {
		return err
	}
	line := fmt.Sprintf("- [@%s %s] %s\n", strings.TrimPrefix(author, "@"), time.Now().Format("2006-01-02 15:04"), text)
	updated := strings.TrimRight(notes, "\n")
	if updated != "" {
		updated += "\n"
	}
	updated += line
	if len(updated) > MaxRoomNotesBytes {
		// Keep the newest notes instead of refusing the write: the cap exists
		// to bound prompt size, and dropping the just-written note would make
		// the directive silently do nothing.
		cut := len(updated) - MaxRoomNotesBytes
		if cut < len(updated) {
			updated = updated[cut:]
		}
	}
	return m.store.SaveRoomNotes(key, updated)
}

// roomNotesPrompt renders the notes section of a room prompt. Empty notes
// still get a one-line section: members otherwise have no idea the mechanism
// exists, and @note would be used by nobody.
func (m *Manager) roomNotesPrompt(roomID string) string {
	notes, err := m.store.LoadRoomNotes(strings.ToLower(roomID))
	if err != nil {
		return ""
	}
	notes = strings.TrimSpace(notes)
	if notes == "" {
		return "Shared room notes: (empty). Append durable conclusions with a line starting \"@note <text>\"."
	}
	if len(notes) > 6000 {
		notes = notes[len(notes)-6000:]
	}
	return "Shared room notes (visible to every member, not part of the message log):\n" + notes +
		"\n(To add a durable note, start a line with \"@note <text>\".)"
}
