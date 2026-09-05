package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// uploadsRootName is the on-disk name of the uploads directory under <magicHome>.
// All uploads now live in <magicHome>/uploads/<session_id>/<uuid>.<ext> so we
// can clean them up deterministically when a session is deleted.
const uploadsRootName = "uploads"

// fileNameSafeRe restricts persisted file names to a tiny ASCII set. Anything
// outside this set (path separators, control chars, NUL bytes, exotic unicode)
// is collapsed to '_' so the on-disk filename cannot escape uploads root via
// crafted Content-Disposition values from the client.
var fileNameSafeRe = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// maxUploadHeaderBytes is how much we let the multipart parser buffer in memory.
// Anything larger spills to disk; we cap total bytes via io.CopyN so the actual
// limit is enforced independent of how Go partitions the stream.
const maxUploadHeaderBytes = 1 << 20

// uploadsAuditBatch caps audit log line batching for /api/upload events.
const uploadsAuditBatch = 32

// uploadsRoot returns the absolute uploads root, creating it if needed.
func (s *Server) uploadsRoot() string {
	return filepath.Join(s.magicHome, uploadsRootName)
}

// uploadsDirFor returns the per-session uploads directory, e.g.
// <magicHome>/uploads/<session_id>. Empty sessionID falls back to a shared
// "_shared" bucket — only used when the frontend did not provide a session id
// (e.g. legacy callers). Files in _shared still respect the orphan TTL.
func (s *Server) uploadsDirFor(sessionID string) string {
	root := s.uploadsRoot()
	if sessionID == "" {
		return filepath.Join(root, "_shared")
	}
	safe := fileNameSafeRe.ReplaceAllString(sessionID, "_")
	if safe == "" {
		return filepath.Join(root, "_shared")
	}
	return filepath.Join(root, safe)
}

// uploadsGCState is reserved for future per-server bookkeeping. We keep a
// mutex so addUploadAudit can grab it cheaply when batching.
type uploadsGCState struct {
	mu sync.Mutex
}

var uploadsGC uploadsGCState

// startUploadsGC starts the background GC ticker. Idempotent.
func startUploadsGC() {
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			runUploadsGC(30 * 24 * time.Hour)
		}
	}()
}

// runUploadsGC walks the uploads directory and removes:
//  1. Per-session subdirectories whose session no longer exists in the session
//     store (orphan sessions, e.g. user deleted the chat).
//  2. Files inside _shared/ older than orphanTTL.
//  3. Empty subdirectories anywhere under the uploads root.
func runUploadsGC(orphanTTL time.Duration) {
	uploadsGC.mu.Lock()
	defer uploadsGC.mu.Unlock()

	refs := globalServerRefs()
	for _, ref := range refs {
		runUploadsGCFor(ref, orphanTTL)
	}
}

func runUploadsGCFor(ref *serverRef, orphanTTL time.Duration) {
	root := filepath.Join(ref.root, uploadsRootName)
	if _, err := os.Stat(root); err != nil {
		return
	}

	// Phase 1: mtime-based cleanup of _shared/ files older than orphanTTL.
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if info.ModTime().After(time.Now().Add(-orphanTTL)) {
			return nil
		}
		if strings.Contains(path, string(filepath.Separator)+"_shared"+string(filepath.Separator)) {
			_ = os.Remove(path)
		}
		return nil
	})

	// Phase 2: remove orphan per-session dirs (session id no longer exists).
	if ref.sessionLookup != nil {
		entries, err := os.ReadDir(root)
		if err == nil {
			for _, entry := range entries {
				if !entry.IsDir() || entry.Name() == "_shared" {
					continue
				}
				sid := entry.Name()
				if _, err := ref.sessionLookup(sid); err == nil {
					continue
				}
				orphanPath := filepath.Join(root, sid)
				if err := os.RemoveAll(orphanPath); err == nil {
					if ref.logf != nil {
						ref.logf("[uploads] gc: removed orphan session uploads %s", sid)
					}
				}
			}
		}
	}

	// Phase 3: remove now-empty per-session subdirectories.
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() || path == root {
			return nil
		}
		entries, _ := os.ReadDir(path)
		if len(entries) == 0 {
			_ = os.Remove(path)
		}
		return nil
	})
}

// cleanupSessionUploads removes all uploads for a given session id. Called
// when the session is deleted with delete_files=true.
func (s *Server) cleanupSessionUploads(sessionID string) {
	if sessionID == "" {
		return
	}
	safe := fileNameSafeRe.ReplaceAllString(sessionID, "_")
	if safe == "" {
		return
	}
	target := filepath.Join(s.uploadsRoot(), safe)
	if err := os.RemoveAll(target); err != nil {
		log.Warnf("[uploads] cleanup %s failed: %v", safe, err)
		return
	}
	log.Infof("[uploads] cleaned session uploads: %s", safe)
}

// uploadsUploadResult mirrors the JSON contract returned by /api/upload.
type uploadsUploadResult struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Filename string `json:"filename"`
	URL      string `json:"url"`
	Size     int64  `json:"size"`
	MIME     string `json:"mime,omitempty"`
}

// uploadsAuditEntry is a single audit log line.
type uploadsAuditEntry struct {
	Time      time.Time
	SessionID string
	User      string
	Action    string // upload, delete, gc
	Filename  string
	Size      int64
	IP        string
}

var (
	uploadsAuditMu  sync.Mutex
	uploadsAuditBuf []uploadsAuditEntry
)

func (s *Server) recordUploadAudit(e uploadsAuditEntry) {
	uploadsAuditMu.Lock()
	uploadsAuditBuf = append(uploadsAuditBuf, e)
	if len(uploadsAuditBuf) >= uploadsAuditBatch {
		batch := uploadsAuditBuf
		uploadsAuditBuf = nil
		uploadsAuditMu.Unlock()
		s.flushUploadAudit(batch)
		return
	}
	uploadsAuditMu.Unlock()
}

func (s *Server) flushUploadAudit(batch []uploadsAuditEntry) {
	for _, e := range batch {
		log.Infof("[uploads] audit action=%s session=%s user=%s file=%s size=%d ip=%s t=%s",
			e.Action, e.SessionID, e.User, e.Filename, e.Size, e.IP, e.Time.Format("15:04:05"))
	}
}

// flushAllUploadAudits drains the buffer; called from server shutdown.
func (s *Server) flushAllUploadAudits() {
	uploadsAuditMu.Lock()
	batch := uploadsAuditBuf
	uploadsAuditBuf = nil
	uploadsAuditMu.Unlock()
	if len(batch) > 0 {
		s.flushUploadAudit(batch)
	}
}

// safeFilename sanitizes a client-supplied filename into something we can put
// on disk. Strips directory components, collapses unsafe chars to '_', trims
// leading dots, and caps length.
func safeFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return ""
	}
	if strings.ContainsRune(name, 0) {
		return ""
	}
	name = fileNameSafeRe.ReplaceAllString(name, "_")
	name = strings.TrimLeft(name, ".")
	if name == "" {
		return ""
	}
	const maxLen = 80
	if len(name) > maxLen {
		ext := filepath.Ext(name)
		if len(ext) < maxLen {
			name = name[:maxLen-len(ext)] + ext
		} else {
			name = name[:maxLen]
		}
	}
	return name
}

// resolveExtension decides which extension the on-disk file should use.
// Prefers the client-supplied extension (sanitized); falls back to
// sniff-derived extension; falls back to ".bin" only for truly unknown
// binaries. Extensionless text files (LICENSE, Makefile, README...) sniff as
// "text/plain; charset=utf-8" — note the charset parameter — so the sniff
// value must be normalized (parameter stripped) before matching, otherwise
// they would all fall through to ".bin".
func resolveExtension(clientName string, sniff string) string {
	ext := strings.ToLower(filepath.Ext(clientName))
	if ext != "" && len(ext) <= 8 {
		// check ext body (after the dot) contains only safe chars
		body := ext[1:]
		if fileNameSafeRe.ReplaceAllString(body, "") == body && body != "" {
			return ext
		}
	}
	if sniff != "" {
		// Strip parameters: "text/plain; charset=utf-8" -> "text/plain".
		mime := sniff
		if i := strings.Index(mime, ";"); i >= 0 {
			mime = strings.TrimSpace(mime[:i])
		}
		switch mime {
		case "text/plain":
			return ".txt"
		case "text/html":
			return ".html"
		case "text/css":
			return ".css"
		case "text/csv":
			return ".csv"
		case "text/xml", "application/xml":
			return ".xml"
		case "application/json":
			return ".json"
		case "application/javascript", "text/javascript":
			return ".js"
		case "application/x-yaml", "text/yaml", "text/x-yaml":
			return ".yaml"
		case "image/png":
			return ".png"
		case "image/jpeg":
			return ".jpg"
		case "image/gif":
			return ".gif"
		case "image/webp":
			return ".webp"
		case "image/bmp":
			return ".bmp"
		case "image/avif":
			return ".avif"
		case "image/svg+xml":
			return ".svg"
		case "application/pdf":
			return ".pdf"
		case "application/zip":
			return ".zip"
		case "application/gzip", "application/x-gzip":
			return ".gz"
		case "application/x-tar":
			return ".tar"
		case "application/wasm":
			return ".wasm"
		case "video/mp4":
			return ".mp4"
		case "video/webm":
			return ".webm"
		case "audio/mpeg":
			return ".mp3"
		case "audio/wav", "audio/x-wav":
			return ".wav"
		case "audio/ogg":
			return ".ogg"
		}
	}
	return ".bin"
}

// handleFileUpload — POST /api/upload
func (s *Server) handleFileUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseMultipartForm(maxUploadHeaderBytes); err != nil {
		http.Error(w, "failed to parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "failed to get file: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	sessionID := strings.TrimSpace(r.FormValue("session_id"))
	clientName := header.Filename
	safe := safeFilename(clientName)
	if safe == "" {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}

	// Validate session id: if provided, it must exist (or we silently bucket
	// into _shared). This avoids filling uploads/ with random unknown ids.
	if sessionID != "" && s.sessionStore != nil {
		if _, err := s.sessionStore.LoadSession(context.Background(), sessionID); err != nil {
			log.Warnf("[uploads] session id %q not found, falling back to _shared", sessionID)
			sessionID = ""
		}
	}

	dstDir := s.uploadsDirFor(sessionID)
	if err := os.MkdirAll(dstDir, 0o700); err != nil {
		http.Error(w, "failed to create uploads dir", http.StatusInternalServerError)
		return
	}
	_ = os.Chmod(dstDir, 0o700)

	maxBytes := s.cfg.Server.GetUploadMaxBytes()
	blocked := s.cfg.Server.GetUploadBlockedExts()

	extLower := strings.ToLower(filepath.Ext(safe))
	if _, bad := blocked[extLower]; bad {
		s.recordUploadAudit(uploadsAuditEntry{
			Time: time.Now(), SessionID: sessionID,
			Action: "upload-rejected", Filename: safe, Size: 0, IP: clientIP(r),
		})
		w.Header().Set("X-Error-Code", "upload_type_not_allowed")
		http.Error(w, fmt.Sprintf("file type %q is not allowed", extLower), http.StatusUnsupportedMediaType)
		return
	}

	// Sniff first 512 bytes for true MIME type (defense against renamed binaries).
	var headBuf [512]byte
	headLen, _ := io.ReadFull(file, headBuf[:])
	sniffed := http.DetectContentType(headBuf[:headLen])

	// Reject text/image extensions with mismatched content.
	if sniffed != "" && extLower != "" {
		isImageExt := strings.HasPrefix(extLower, ".png") || strings.HasPrefix(extLower, ".jpg") ||
			strings.HasPrefix(extLower, ".jpeg") || strings.HasPrefix(extLower, ".gif") ||
			strings.HasPrefix(extLower, ".webp") || strings.HasPrefix(extLower, ".svg")
		if isImageExt && !strings.HasPrefix(sniffed, "image/") {
			w.Header().Set("X-Error-Code", "upload_content_mismatch")
			http.Error(w, fmt.Sprintf("file content (%s) does not match image extension", sniffed),
				http.StatusUnsupportedMediaType)
			return
		}
	}

	diskExt := resolveExtension(safe, sniffed)

	fileID := uuid.New().String()
	diskName := fileID + diskExt
	dstPath := filepath.Join(dstDir, diskName)

	out, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		http.Error(w, "failed to save file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Write the already-consumed sniff head bytes back to the destination file
	// first, then stream the rest with io.LimitReader so we can enforce the
	// hard size cap (one extra byte tells us if the upload was oversized).
	if headLen > 0 {
		if _, err := out.Write(headBuf[:headLen]); err != nil {
			out.Close()
			os.Remove(dstPath)
			http.Error(w, "failed to write file: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	limited := io.LimitReader(file, maxBytes+1)
	written := int64(headLen)
	copied, err := io.Copy(out, limited)
	if err != nil {
		out.Close()
		os.Remove(dstPath)
		http.Error(w, "failed to write file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	written += copied
	if written > maxBytes {
		out.Close()
		os.Remove(dstPath)
		w.Header().Set("X-Error-Code", "upload_too_large")
		http.Error(w, fmt.Sprintf("file exceeds maximum allowed size of %d bytes", maxBytes),
			http.StatusRequestEntityTooLarge)
		return
	}
	if err := out.Close(); err != nil {
		os.Remove(dstPath)
		http.Error(w, "failed to finalize file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = os.Chmod(dstPath, 0o600)

	// Build the public URL. Prefer cfg.Server.UploadURLPrefix if set; otherwise
	// return the per-session-relative path so the file server can serve it via
	// /api/uploads/<session>/<file>.
	var fileURL string
	safeSession := fileNameSafeRe.ReplaceAllString(sessionID, "_")
	if s.cfg.Server.UploadURLPrefix != "" {
		base := strings.TrimRight(s.cfg.Server.UploadURLPrefix, "/")
		if safeSession == "" {
			fileURL = fmt.Sprintf("%s/_shared/%s", base, diskName)
		} else {
			fileURL = fmt.Sprintf("%s/%s/%s", base, safeSession, diskName)
		}
	} else {
		if safeSession == "" {
			fileURL = fmt.Sprintf("/api/uploads/_shared/%s", diskName)
		} else {
			fileURL = fmt.Sprintf("/api/uploads/%s/%s", safeSession, diskName)
		}
	}

	s.recordUploadAudit(uploadsAuditEntry{
		Time: time.Now(), SessionID: sessionID,
		Action: "upload", Filename: diskName, Size: written, IP: clientIP(r),
	})

	jsonResponse(w, uploadsUploadResult{
		ID:       fileID,
		Name:     clientName,
		Filename: diskName,
		URL:      fileURL,
		Size:     written,
		MIME:     sniffed,
	})
}

// handleFileList — GET /api/files
func (s *Server) handleFileList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	root := s.uploadsRoot()
	sessionFilter := strings.TrimSpace(r.URL.Query().Get("session_id"))
	if sessionFilter != "" {
		sessionFilter = fileNameSafeRe.ReplaceAllString(sessionFilter, "_")
	}

	type fileEntry struct {
		Filename  string `json:"filename"`
		Size      int64  `json:"size"`
		URL       string `json:"url"`
		Updated   string `json:"updated"`
		SessionID string `json:"session_id,omitempty"`
	}

	files := []fileEntry{}

	getFileURL := func(sid string, name string) string {
		if s.cfg.Server.UploadURLPrefix != "" {
			base := strings.TrimRight(s.cfg.Server.UploadURLPrefix, "/")
			if sid == "" {
				return fmt.Sprintf("%s/_shared/%s", base, name)
			}
			return fmt.Sprintf("%s/%s/%s", base, sid, name)
		}
		if sid == "" {
			return fmt.Sprintf("/api/uploads/_shared/%s", name)
		}
		return fmt.Sprintf("/api/uploads/%s/%s", sid, name)
	}

	scanDir := func(dir string, sid string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			files = append(files, fileEntry{
				Filename:  info.Name(),
				Size:      info.Size(),
				URL:       getFileURL(sid, info.Name()),
				Updated:   info.ModTime().Format("2006-01-02 15:04:05"),
				SessionID: sid,
			})
		}
	}

	if sessionFilter != "" {
		scanDir(filepath.Join(root, sessionFilter), sessionFilter)
	} else {
		entries, err := os.ReadDir(root)
		if err != nil {
			jsonResponse(w, map[string]interface{}{"files": files})
			return
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			scanDir(filepath.Join(root, entry.Name()), entry.Name())
		}
	}

	jsonResponse(w, map[string]interface{}{"files": files})
}

// handleFileDelete — DELETE /api/files/{session_id}/{filename}
// Legacy form /api/files/{filename} is accepted as a fallback for shared bucket.
func (s *Server) handleFileDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/api/files/")
	if rest == "" {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	var sessionID, filename string
	if len(parts) == 2 {
		sessionID = parts[0]
		filename = parts[1]
	} else {
		filename = parts[0]
	}

	if filename == "" || strings.Contains(filename, "..") || strings.ContainsRune(filename, 0) {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}
	filename = filepath.Base(filename)
	if filename == "." || filename == ".." || filename == "" {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}

	safe := safeFilename(filename)
	if safe == "" {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}

	dir := s.uploadsDirFor(sessionID)
	absDir, err := filepath.Abs(dir)
	if err != nil {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	absRoot, _ := filepath.Abs(s.uploadsRoot())
	if !strings.HasPrefix(absDir, absRoot) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	target := filepath.Join(absDir, filename)
	absTarget, _ := filepath.Abs(target)
	if !strings.HasPrefix(absTarget, absRoot) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	if err := os.Remove(absTarget); err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "file not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to delete file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.recordUploadAudit(uploadsAuditEntry{
		Time: time.Now(), SessionID: sessionID,
		Action: "delete", Filename: filename, Size: 0, IP: clientIP(r),
	})

	jsonResponse(w, map[string]interface{}{"success": true})
}

// uploadsServeHandler serves /api/uploads/ from the per-session layout. Each
// request is auth-gated by requireAuth in the router. We never list dirs.
func (s *Server) uploadsServeHandler() http.Handler {
	root := s.uploadsRoot()
	fs := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upath := r.URL.Path
		if !strings.HasPrefix(upath, "/api/uploads/") {
			http.NotFound(w, r)
			return
		}
		rel := strings.TrimPrefix(upath, "/api/uploads/")
		if strings.Contains(rel, "..") {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		absRoot, _ := filepath.Abs(root)
		resolved, err := filepath.Abs(filepath.Join(absRoot, filepath.FromSlash(rel)))
		if err != nil || !strings.HasPrefix(resolved, absRoot) {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		info, err := os.Stat(resolved)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if info.IsDir() {
			http.NotFound(w, r)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/" + filepath.ToSlash(rel)
		fs.ServeHTTP(w, r2)
	})
}

// serverRef is a tiny indirection used by runUploadsGC to access per-server
// state (magicHome, session store) without import cycles. Populated by
// registerUploadsServer during NewServer.
type serverRef struct {
	root          string
	sessionLookup func(string) (any, error)
	logf          func(string, ...interface{})
}

var (
	globalServerMu sync.RWMutex
	globalServers  []*serverRef
)

// globalServerRefs returns a snapshot of currently registered servers.
func globalServerRefs() []*serverRef {
	globalServerMu.RLock()
	defer globalServerMu.RUnlock()
	out := make([]*serverRef, len(globalServers))
	copy(out, globalServers)
	return out
}

// registerUploadsServer wires a server into the GC's server list. Safe to call
// multiple times.
func registerUploadsServer(root string, lookup func(string) (any, error), logf func(string, ...interface{})) {
	if root == "" || logf == nil {
		return
	}
	ref := &serverRef{root: root, sessionLookup: lookup, logf: logf}
	globalServerMu.Lock()
	defer globalServerMu.Unlock()
	globalServers = append(globalServers, ref)
}

// clientIP returns a best-effort client IP. Used purely for audit logs.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.Index(xff, ","); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if h := r.Header.Get("X-Real-IP"); h != "" {
		return h
	}
	return r.RemoteAddr
}

// workdirAttachmentsDir is the per-session attachment area inside the agent's
// working directory. Uploaded files are copied here at stream time so the
// model's file tools — which are sandboxed to the workdir — can read them.
// The dotted name avoids colliding with a user project's own uploads/ folder.
const workdirAttachmentsDir = ".magic-uploads"

// uploadToMaterialize pairs a display name with its canonical location under
// the uploads root, collected while parsing the stream request body.
type uploadToMaterialize struct {
	Name string // original display filename (e.g. "核算表.xlsx")
	Src  string // absolute canonical path, e.g. <uploadsRoot>/<session>/<uuid>.<ext>
}

// materializeUploads copies uploaded attachments into <workDir>/.magic-uploads/
// so agent file tools can access them, and returns a human-readable summary of
// the workdir-relative paths ("" when nothing was materialized). Copying is
// idempotent: re-sending the same attachment simply overwrites.
func materializeUploads(items []uploadToMaterialize, workDir string) string {
	if len(items) == 0 || workDir == "" {
		return ""
	}
	dstDir := filepath.Join(workDir, workdirAttachmentsDir)
	if err := os.MkdirAll(dstDir, 0700); err != nil {
		log.Warnf("[uploads] materialize: mkdir %s failed: %v", dstDir, err)
		return ""
	}

	var lines []string
	for _, it := range items {
		if it.Src == "" || it.Name == "" {
			continue
		}
		dst := filepath.Join(dstDir, filepath.Base(it.Src))
		if err := copyUploadFile(it.Src, dst); err != nil {
			log.Warnf("[uploads] materialize %s -> %s failed: %v", it.Src, dst, err)
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s → %s/%s（工作目录内）", it.Name, workdirAttachmentsDir, filepath.Base(dst)))
	}
	if len(lines) == 0 {
		return ""
	}
	return "本次消息的附件已放入工作目录，可直接用文件工具读取：\n" + strings.Join(lines, "\n")
}

// copyUploadFile copies src to dst atomically enough for our purposes
// (write temp + rename) with 0600 permissions.
func copyUploadFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err = out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	// Rename over an existing destination works on both POSIX and Windows
	// (Go maps it to MoveFileEx with REPLACE_EXISTING).
	if err = os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
