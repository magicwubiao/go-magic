package server

// uploads_meta.go — persistent metadata for uploaded files.
//
// On-disk uploads are stored as "<magicHome>/uploads/<session>/<uuid>.<ext>":
// the uuid name keeps the file unique, path-safe and immune to same-name
// collisions, but it is unreadable to humans and any original filename is
// lost after the upload response is consumed. This store keeps the one-line
// mapping (disk uuid name -> client-supplied original name) in a tiny sqlite
// database so the Files page can show a readable name without ever renaming
// the on-disk file (which would break URLs already persisted in message
// history). Rows are deleted together with their file by the session-delete
// and GC cleanup paths.

import (
	"database/sql"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

// uploadsMetaFileName is the sqlite database next to sessions.db holding the
// disk-name -> original-name map for /api/upload files.
const uploadsMetaFileName = "uploads.db"

// uploadsMetaRow is one file mapping entry.
type uploadsMetaRow struct {
	DiskName  string // uuid.ext as stored on disk
	OrigName  string // readable client-supplied name
	Size      int64
	MIME      string
	CreatedAt int64
}

// uploadsMetaStore is a thin wrapper over the uploads metadata database.
type uploadsMetaStore struct {
	mu sync.Mutex
	db *sql.DB
}

// ensureUploadsMeta lazily opens the uploads metadata database under magicHome
// and returns the cached store. Creating the schema is idempotent.
func (s *Server) ensureUploadsMeta() *uploadsMetaStore {
	if s.uploadsMeta != nil {
		return s.uploadsMeta
	}
	st, err := openUploadsMeta(s.magicHome)
	if err != nil {
		// Fail-open: metadata is an enhancement for readability. Without it
		// uploads still work exactly as before (uuid names shown verbatim).
		return nil
	}
	s.uploadsMeta = st
	return st
}

// openUploadsMeta opens (creating if needed) the uploads metadata database.
func openUploadsMeta(magicHome string) (*uploadsMetaStore, error) {
	path := filepath.Join(magicHome, uploadsMetaFileName)
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	st := &uploadsMetaStore{db: db}
	if err := st.initSchema(); err != nil {
		db.Close()
		return nil, err
	}
	return st, nil
}

func (st *uploadsMetaStore) initSchema() error {
	_, err := st.db.Exec(`
CREATE TABLE IF NOT EXISTS uploads_meta (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT    NOT NULL DEFAULT '',
	disk_name  TEXT    NOT NULL,
	orig_name  TEXT    NOT NULL,
	size       INTEGER NOT NULL DEFAULT 0,
	mime       TEXT    NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_uploads_meta_disk
	ON uploads_meta(session_id, disk_name);
CREATE INDEX IF NOT EXISTS idx_uploads_meta_session
	ON uploads_meta(session_id);`)
	return err
}

// Upsert records (or refreshes) the mapping for one uploaded file.
func (st *uploadsMetaStore) Upsert(sessionID, diskName, origName string, size int64, mime string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	_, err := st.db.Exec(`
INSERT INTO uploads_meta (session_id, disk_name, orig_name, size, mime, created_at)
VALUES (?, ?, ?, ?, ?, unixepoch())
ON CONFLICT(session_id, disk_name) DO UPDATE SET
	orig_name = excluded.orig_name,
	size = excluded.size,
	mime = excluded.mime`,
		sessionID, diskName, origName, size, mime)
	return err
}

// Get returns the metadata for a single disk file. ok=false when absent.
func (st *uploadsMetaStore) Get(sessionID, diskName string) (uploadsMetaRow, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	var r uploadsMetaRow
	err := st.db.QueryRow(`
SELECT disk_name, orig_name, size, mime, created_at
FROM uploads_meta WHERE session_id = ? AND disk_name = ?`,
		sessionID, diskName).Scan(&r.DiskName, &r.OrigName, &r.Size, &r.MIME, &r.CreatedAt)
	if err != nil {
		return r, false
	}
	return r, true
}

// DeleteDisk removes the metadata row for one file (file deletion path).
func (st *uploadsMetaStore) DeleteDisk(sessionID, diskName string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	_, err := st.db.Exec(`DELETE FROM uploads_meta WHERE session_id = ? AND disk_name = ?`, sessionID, diskName)
	return err
}

// DeleteSession removes all metadata rows for a session directory (session
// delete / orphan GC path). Returns the number of rows removed.
func (st *uploadsMetaStore) DeleteSession(sessionID string) (int64, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	res, err := st.db.Exec(`DELETE FROM uploads_meta WHERE session_id = ?`, sessionID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// Close releases the database handle. Idempotent and nil-safe.
func (st *uploadsMetaStore) Close() error {
	if st == nil || st.db == nil {
		return nil
	}
	return st.db.Close()
}

// uploadsMetaPathFor mirrors openUploadsMeta's path derivation, used by tests
// to inspect the database location.
func uploadsMetaPathFor(magicHome string) string {
	return filepath.Join(magicHome, uploadsMetaFileName)
}
