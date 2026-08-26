package server

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

func (s *Server) handleFileDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != "DELETE" {
		http.Error(w, "method not allowed", 405)
		return
	}

	// Extract filename from URL: /api/files/{filename}
	filename := strings.TrimPrefix(r.URL.Path, "/api/files/")
	if filename == "" || strings.Contains(filename, "..") {
		http.Error(w, "invalid filename", 400)
		return
	}

	uploadsDir := filepath.Join(s.magicHome, "uploads")
	filePath := filepath.Join(uploadsDir, filename)

	// Security: ensure path is within uploads directory
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		http.Error(w, "invalid path", 400)
		return
	}
	absUploadsDir, _ := filepath.Abs(uploadsDir)
	if !strings.HasPrefix(absPath, absUploadsDir) {
		http.Error(w, "invalid path", 400)
		return
	}

	err = os.Remove(filePath)
	if err != nil {
		http.Error(w, "failed to delete file: "+err.Error(), 500)
		return
	}

	jsonResponse(w, map[string]interface{}{"success": true})
}

func (s *Server) handleFSDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Path      string `json:"path"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, map[string]interface{}{"error": "invalid body"})
		return
	}

	if req.Path == "" {
		jsonResponse(w, map[string]interface{}{"error": "path is required"})
		return
	}

	absPath, err := s.resolveFSPath(req.Path, req.SessionID)
	if err != nil {
		jsonResponse(w, map[string]interface{}{"error": err.Error()})
		return
	}

	if req.SessionID != "" {
		sess, err := s.sessionStore.LoadSession(context.Background(), req.SessionID)
		if err == nil && sess.WorkDir != "" {
			absSessionDir, err := filepath.Abs(sess.WorkDir)
			if err == nil {
				absSessionDir = filepath.Clean(absSessionDir)
				absPathClean := filepath.Clean(absPath)
				if absPathClean == absSessionDir {
					jsonResponse(w, map[string]interface{}{"error": "cannot delete session root directory"})
					return
				}
			}
		}
	} else if absPath == s.cfg.WorkingDir {
		jsonResponse(w, map[string]interface{}{"error": "cannot delete root working directory"})
		return
	}

	info, err := os.Stat(absPath)
	if err != nil {
		jsonResponse(w, map[string]interface{}{"error": "file not found"})
		return
	}

	if info.IsDir() {
		if err := os.RemoveAll(absPath); err != nil {
			jsonResponse(w, map[string]interface{}{"error": "cannot delete directory: " + err.Error()})
			return
		}
	} else {
		if err := os.Remove(absPath); err != nil {
			jsonResponse(w, map[string]interface{}{"error": "cannot delete file: " + err.Error()})
			return
		}
	}

	jsonResponse(w, map[string]interface{}{"success": true})
}

func saveUploadedFile(src io.Reader, dst string) error {
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = out.ReadFrom(src)
	return err
}

func (s *Server) handleFileList(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", 405)
		return
	}

	uploadsDir := filepath.Join(s.magicHome, "uploads")
	entries, err := os.ReadDir(uploadsDir)
	if err != nil {
		jsonResponse(w, map[string]interface{}{"files": []map[string]interface{}{}})
		return
	}

	// Build URL based on configuration
	var getFileURL func(name string) string
	if s.cfg.Server.UploadURLPrefix != "" {
		getFileURL = func(name string) string {
			return s.cfg.Server.UploadURLPrefix + "/" + name
		}
	} else {
		getFileURL = func(name string) string {
			return "/api/uploads/" + name
		}
	}

	files := []map[string]interface{}{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, map[string]interface{}{
			"filename": info.Name(),
			"size":     info.Size(),
			"url":      getFileURL(info.Name()),
			"updated":  info.ModTime().Format("2006-01-02 15:04:05"),
		})
	}

	jsonResponse(w, map[string]interface{}{"files": files})
}

func (s *Server) getSessionWorkDir(sessionID string, sessionName string) string {
	baseDir := s.cfg.WorkingDir
	if baseDir == "" {
		baseDir = filepath.Join(s.magicHome, "workspace")
	}
	baseDir = filepath.Join(baseDir, "chat")
	safeName := sanitizeDirName(sessionName)
	if safeName == "" {
		safeName = "session"
	}
	shortID := sessionID[:8]
	return filepath.Join(baseDir, fmt.Sprintf("%s-%s", safeName, shortID))
}

func (s *Server) handleListDirs(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		path, _ = os.Getwd()
	}

	// Security: resolve to absolute path and prevent traversal above root
	absPath, err := filepath.Abs(path)
	if err != nil {
		jsonResponse(w, map[string]interface{}{"dirs": []string{}, "error": "invalid path"})
		return
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		jsonResponse(w, map[string]interface{}{"dirs": []string{}, "error": "cannot read directory"})
		return
	}

	type dirEntry struct {
		Path  string `json:"path"`
		Name  string `json:"name"`
		IsDir bool   `json:"is_dir"`
	}

	var result []dirEntry
	// Add parent directory option
	parent := filepath.Dir(absPath)
	if parent != absPath {
		result = append(result, dirEntry{Path: parent, Name: "..", IsDir: true})
	}

	// Windows: 处于盘符根目录(如 C:\)时，列出所有可用盘符作为可切换入口，
	// 否则用户最上层只能停留在执行文件所在盘符，无法选择 D: 等其他盘。
	if runtime.GOOS == "windows" {
		vol := filepath.VolumeName(absPath)
		// absPath 形如 "C:\\" 时，清理后与 vol+"\" 相等，表示已是该盘根目录
		if vol != "" && filepath.Clean(absPath) == vol+string(filepath.Separator) {
			for letter := 'A'; letter <= 'Z'; letter++ {
				drive := string(letter) + ":" + string(filepath.Separator)
				if _, err := os.Stat(drive); err == nil {
					name := string(letter) + ":"
					// 标记当前所在盘符，避免重复显示
					if strings.EqualFold(name, vol) {
						continue
					}
					result = append(result, dirEntry{Path: drive, Name: name, IsDir: true})
				}
			}
		}
	}

	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			result = append(result, dirEntry{
				Path:  filepath.Join(absPath, entry.Name()),
				Name:  entry.Name(),
				IsDir: true,
			})
		}
	}

	jsonResponse(w, map[string]interface{}{
		"current": absPath,
		"dirs":    result,
	})
}

func (s *Server) handleFSShared(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/api/fs/shared/")
	token = strings.TrimSuffix(token, "/")
	if token == "" {
		http.Error(w, "missing token", http.StatusBadRequest)
		return
	}

	s.shareTokensMu.RLock()
	tk, ok := s.shareTokens[token]
	s.shareTokensMu.RUnlock()
	if !ok {
		http.Error(w, "share link not found or expired", http.StatusNotFound)
		return
	}
	if time.Now().After(tk.ExpiresAt) {
		s.shareTokensMu.Lock()
		delete(s.shareTokens, token)
		s.shareTokensMu.Unlock()
		http.Error(w, "share link expired", http.StatusGone)
		return
	}

	info, err := os.Stat(tk.Path)
	if err != nil {
		http.Error(w, "resource not found", http.StatusNotFound)
		return
	}

	if info.IsDir() {
		entries, err := os.ReadDir(tk.Path)
		if err != nil {
			http.Error(w, "cannot read directory", http.StatusInternalServerError)
			return
		}
		type fsEntry struct {
			Path     string `json:"path"`
			Name     string `json:"name"`
			IsDir    bool   `json:"is_dir"`
			Size     int64  `json:"size"`
			Modified int64  `json:"modified"`
		}
		result := []fsEntry{}
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			ei, ierr := entry.Info()
			var size int64
			var modTime int64
			if ierr == nil {
				size = ei.Size()
				modTime = ei.ModTime().Unix()
			}
			result = append(result, fsEntry{
				Path:     filepath.Join(tk.Path, name),
				Name:     name,
				IsDir:    entry.IsDir(),
				Size:     size,
				Modified: modTime,
			})
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"name":      tk.Name,
			"path":      tk.Path,
			"entries":   result,
			"expire_at": tk.ExpiresAt.Unix(),
		})
		return
	}

	// Single file: serve with content type
	ext := strings.ToLower(filepath.Ext(tk.Path))
	contentType := "application/octet-stream"
	switch ext {
	case ".txt", ".md", ".log", ".csv":
		contentType = "text/plain; charset=utf-8"
	case ".json":
		contentType = "application/json; charset=utf-8"
	case ".html", ".htm":
		contentType = "text/html; charset=utf-8"
	case ".css":
		contentType = "text/css; charset=utf-8"
	case ".js":
		contentType = "application/javascript; charset=utf-8"
	case ".xml":
		contentType = "application/xml; charset=utf-8"
	case ".png":
		contentType = "image/png"
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".gif":
		contentType = "image/gif"
	case ".webp":
		contentType = "image/webp"
	case ".svg":
		contentType = "image/svg+xml"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", "inline; filename=\""+tk.Name+"\"")
	http.ServeFile(w, r, tk.Path)
}

func (s *Server) handleFSWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Path      string `json:"path"`
		Content   string `json:"content"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, map[string]interface{}{"error": "invalid body"})
		return
	}

	if req.Path == "" {
		jsonResponse(w, map[string]interface{}{"error": "path is required"})
		return
	}

	absPath, err := s.resolveFSPath(req.Path, req.SessionID)
	if err != nil {
		jsonResponse(w, map[string]interface{}{"error": err.Error()})
		return
	}

	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		jsonResponse(w, map[string]interface{}{"error": "cannot create directory: " + err.Error()})
		return
	}

	if err := os.WriteFile(absPath, []byte(req.Content), 0600); err != nil {
		jsonResponse(w, map[string]interface{}{"error": "write failed: " + err.Error()})
		return
	}

	info, err := os.Stat(absPath)
	var size int64
	if err == nil {
		size = info.Size()
	}

	jsonResponse(w, map[string]interface{}{"path": absPath, "size": size})
}

func (s *Server) handleFSUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	dirPath := r.URL.Query().Get("path")
	sessionID := r.URL.Query().Get("session_id")

	if dirPath == "" {
		jsonResponse(w, map[string]interface{}{"error": "path (target directory) is required"})
		return
	}

	absDirPath, err := s.resolveFSPath(dirPath, sessionID)
	if err != nil {
		jsonResponse(w, map[string]interface{}{"error": err.Error()})
		return
	}

	info, err := os.Stat(absDirPath)
	if err != nil || !info.IsDir() {
		jsonResponse(w, map[string]interface{}{"error": "target path is not a directory"})
		return
	}

	if err := r.ParseMultipartForm(64 << 20); err != nil {
		jsonResponse(w, map[string]interface{}{"error": "failed to parse form: " + err.Error()})
		return
	}

	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		f, _, err := r.FormFile("file")
		if err != nil {
			jsonResponse(w, map[string]interface{}{"error": "no files provided"})
			return
		}
		f.Close()
		files = r.MultipartForm.File["file"]
	}

	type uploadResult struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Size int64  `json:"size"`
	}

	results := make([]uploadResult, 0, len(files))

	for _, fh := range files {
		filename := filepath.Base(fh.Filename)
		if filename == "" || filename == "." || filename == ".." {
			continue
		}

		dstPath := filepath.Join(absDirPath, filename)

		dstDir := filepath.Dir(dstPath)
		rel, err := filepath.Rel(absDirPath, dstDir)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}

		src, err := fh.Open()
		if err != nil {
			continue
		}

		out, err := os.Create(dstPath)
		if err != nil {
			src.Close()
			continue
		}

		written, err := io.Copy(out, src)
		src.Close()
		out.Close()
		if err != nil {
			os.Remove(dstPath)
			continue
		}

		results = append(results, uploadResult{
			Name: filename,
			Path: dstPath,
			Size: written,
		})
	}

	jsonResponse(w, map[string]interface{}{
		"uploaded": results,
		"count":    len(results),
	})
}

func sanitizeDirName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	var safeName strings.Builder
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == ' ' ||
			unicode.Is(unicode.Han, r) {
			safeName.WriteRune(r)
		} else {
			safeName.WriteRune('_')
		}
	}
	result := strings.ReplaceAll(strings.TrimSpace(safeName.String()), " ", "_")
	if len(result) > 64 {
		result = result[:64]
	}
	return result
}

func (s *Server) resolveFSPath(path string, sessionID string) (string, error) {
	if sessionID != "" {
		sess, err := s.sessionStore.LoadSession(context.Background(), sessionID)
		if err != nil {
			return "", fmt.Errorf("session not found: %w", err)
		}
		sessionWorkDir := sess.WorkDir
		if sessionWorkDir == "" {
			return "", fmt.Errorf("session has no work_dir")
		}
		if err := s.ensureSessionWorkDir(sessionWorkDir); err != nil {
			return "", fmt.Errorf("failed to ensure session workdir: %w", err)
		}
		absSessionDir, err := filepath.Abs(sessionWorkDir)
		if err != nil {
			return "", fmt.Errorf("failed to resolve session workdir: %w", err)
		}
		absSessionDir = filepath.Clean(absSessionDir)
		if path == "" {
			return absSessionDir, nil
		}
		var targetPath string
		if filepath.IsAbs(path) {
			targetPath = filepath.Clean(path)
		} else {
			targetPath = filepath.Clean(filepath.Join(absSessionDir, path))
		}
		relPath, err := filepath.Rel(absSessionDir, targetPath)
		if err != nil {
			return "", fmt.Errorf("path outside session directory")
		}
		if relPath == "." {
			return absSessionDir, nil
		}
		if strings.HasPrefix(relPath, ".."+string(filepath.Separator)) || relPath == ".." {
			return "", fmt.Errorf("path outside session directory")
		}
		realPath, err := filepath.EvalSymlinks(targetPath)
		if err != nil {
			return "", fmt.Errorf("invalid path")
		}
		realRelPath, err := filepath.Rel(absSessionDir, realPath)
		if err != nil || strings.HasPrefix(realRelPath, ".."+string(filepath.Separator)) || realRelPath == ".." {
			return "", fmt.Errorf("path outside session directory")
		}
		return realPath, nil
	}
	if path == "" {
		path = s.cfg.WorkingDir
	}
	return sanitizeFSPath(path)
}

func (s *Server) handleFSRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Path      string `json:"path"`
		NewName   string `json:"new_name"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, map[string]interface{}{"error": "invalid body"})
		return
	}

	if req.Path == "" || req.NewName == "" {
		jsonResponse(w, map[string]interface{}{"error": "path and new_name are required"})
		return
	}

	if strings.ContainsAny(req.NewName, "/\\") || strings.HasPrefix(req.NewName, ".") {
		jsonResponse(w, map[string]interface{}{"error": "invalid name"})
		return
	}

	absPath, err := s.resolveFSPath(req.Path, req.SessionID)
	if err != nil {
		jsonResponse(w, map[string]interface{}{"error": err.Error()})
		return
	}

	if _, err := os.Stat(absPath); err != nil {
		jsonResponse(w, map[string]interface{}{"error": "file not found"})
		return
	}

	parent := filepath.Dir(absPath)
	newPath := filepath.Join(parent, req.NewName)

	if _, err := os.Stat(newPath); err == nil {
		jsonResponse(w, map[string]interface{}{"error": "target already exists"})
		return
	}

	if err := os.Rename(absPath, newPath); err != nil {
		jsonResponse(w, map[string]interface{}{"error": "rename failed: " + err.Error()})
		return
	}

	jsonResponse(w, map[string]interface{}{"path": newPath, "name": req.NewName})
}

func (s *Server) handleFSList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := r.URL.Query().Get("path")
	sessionID := r.URL.Query().Get("session_id")
	// hidden=1 includes dot-prefixed files/dirs in the listing. Default keeps
	// the old behaviour (hidden files filtered out).
	showHidden := r.URL.Query().Get("hidden") == "1"

	absPath, err := s.resolveFSPath(path, sessionID)
	if err != nil {
		jsonResponse(w, map[string]interface{}{"error": err.Error()})
		return
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		jsonResponse(w, map[string]interface{}{"error": "cannot read directory: " + err.Error()})
		return
	}

	type fsEntry struct {
		Path     string `json:"path"`
		Name     string `json:"name"`
		IsDir    bool   `json:"is_dir"`
		Size     int64  `json:"size"`
		Modified int64  `json:"modified"`
		Hidden   bool   `json:"hidden"`
	}

	var result []fsEntry
	if sessionID == "" {
		parent := filepath.Dir(absPath)
		if parent != absPath {
			result = append(result, fsEntry{Path: parent, Name: "..", IsDir: true})
		}
	}

	for _, entry := range entries {
		name := entry.Name()
		isHidden := strings.HasPrefix(name, ".")
		if isHidden && !showHidden {
			continue
		}
		info, err := entry.Info()
		var size int64
		var modTime int64
		if err == nil {
			size = info.Size()
			modTime = info.ModTime().Unix()
		}
		result = append(result, fsEntry{
			Path:     filepath.Join(absPath, name),
			Name:     name,
			IsDir:    entry.IsDir(),
			Size:     size,
			Modified: modTime,
			Hidden:   isHidden,
		})
	}

	jsonResponse(w, map[string]interface{}{
		"current": absPath,
		"entries": result,
	})
}

func extractZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	// Check if zip has a single top-level directory
	// If so, we'll extract contents directly into destDir instead of preserving nested structure
	hasSingleTopLevelDir := false
	var topLevelDir string

	if len(r.File) > 0 {
		firstName := r.File[0].Name
		slashIdx := strings.Index(firstName, "/")
		if slashIdx > 0 {
			topLevelDir = firstName[:slashIdx]
			// Check if ALL entries start with this directory
			hasSingleTopLevelDir = true
			for _, f := range r.File {
				if !strings.HasPrefix(f.Name, topLevelDir+"/") && f.Name != topLevelDir {
					hasSingleTopLevelDir = false
					break
				}
			}
		}
	}

	// 预先计算 destDir 绝对路径用于安全校验
	absDest, err := filepath.Abs(filepath.Clean(destDir))
	if err != nil {
		return fmt.Errorf("invalid dest dir: %w", err)
	}

	for _, f := range r.File {
		// Skip hidden files and __MACOSX
		if strings.HasPrefix(f.Name, ".") || strings.Contains(f.Name, "__MACOSX") {
			continue
		}

		// Strip top-level directory if zip has single-level nesting
		fpathName := f.Name
		if hasSingleTopLevelDir && topLevelDir != "" {
			fpathName = strings.TrimPrefix(f.Name, topLevelDir+"/")
		}
		if fpathName == "" {
			continue
		}

		// 安全拼接：用 safepath.SafeJoin 校验，防止路径穿越
		fpath, err := SafeJoin(absDest, fpathName)
		if err != nil {
			return fmt.Errorf("unsafe zip entry %q: %w", fpathName, err)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(fpath, f.Mode()); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()

		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) handleFSZip(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := r.URL.Query().Get("path")
	sessionID := r.URL.Query().Get("session_id")
	// hidden=1 includes dot-prefixed files/dirs in the archive. Default keeps
	// the old behaviour (hidden entries skipped).
	showHidden := r.URL.Query().Get("hidden") == "1"

	absPath, err := s.resolveFSPath(path, sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	info, err := os.Stat(absPath)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	// Build the archive name
	archiveName := filepath.Base(absPath) + ".zip"

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+archiveName+"\"")

	zw := zip.NewWriter(w)
	defer zw.Close()

	if info.IsDir() {
		base := absPath
		err = filepath.Walk(base, func(p string, fi os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			// Skip hidden files/dirs unless explicitly requested via ?hidden=1
			name := fi.Name()
			if !showHidden && strings.HasPrefix(name, ".") && p != base {
				if fi.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			rel, relErr := filepath.Rel(base, p)
			if relErr != nil {
				return relErr
			}
			if rel == "." {
				return nil
			}
			// Always use forward slashes inside the zip
			zipName := filepath.ToSlash(rel)
			if fi.IsDir() {
				_, err := zw.CreateHeader(&zip.FileHeader{
					Name:     zipName + "/",
					Method:   zip.Deflate,
					Modified: fi.ModTime(),
				})
				return err
			}
			header := &zip.FileHeader{
				Name:     zipName,
				Method:   zip.Deflate,
				Modified: fi.ModTime(),
			}
			fw, err := zw.CreateHeader(header)
			if err != nil {
				return err
			}
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.Copy(fw, f)
			return err
		})
		if err != nil {
			// Headers already sent, best effort log via stderr.
			fmt.Fprintf(os.Stderr, "zip walk error: %v\n", err)
			return
		}
		return
	}

	// Single file archive
	header := &zip.FileHeader{
		Name:     filepath.ToSlash(filepath.Base(absPath)),
		Method:   zip.Deflate,
		Modified: info.ModTime(),
	}
	fw, err := zw.CreateHeader(header)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zip header error: %v\n", err)
		return
	}
	f, err := os.Open(absPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zip open error: %v\n", err)
		return
	}
	defer f.Close()
	if _, err := io.Copy(fw, f); err != nil {
		fmt.Fprintf(os.Stderr, "zip copy error: %v\n", err)
	}
}

func (s *Server) handleFSCreateDir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Parent    string `json:"parent"`
		Name      string `json:"name"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, map[string]interface{}{"error": "invalid body"})
		return
	}

	if req.Name == "" {
		jsonResponse(w, map[string]interface{}{"error": "directory name is required"})
		return
	}
	if strings.ContainsAny(req.Name, "/\\") || strings.HasPrefix(req.Name, ".") {
		jsonResponse(w, map[string]interface{}{"error": "invalid directory name"})
		return
	}

	absParent, err := s.resolveFSPath(req.Parent, req.SessionID)
	if err != nil {
		jsonResponse(w, map[string]interface{}{"error": err.Error()})
		return
	}

	// Check parent exists and is a directory
	pInfo, err := os.Stat(absParent)
	if err != nil {
		jsonResponse(w, map[string]interface{}{"error": "parent directory not found"})
		return
	}
	if !pInfo.IsDir() {
		jsonResponse(w, map[string]interface{}{"error": "parent path is not a directory"})
		return
	}

	newDir := filepath.Join(absParent, req.Name)
	absNew, err := filepath.Abs(newDir)
	if err != nil {
		jsonResponse(w, map[string]interface{}{"error": "cannot resolve new directory path"})
		return
	}

	// Ensure the resolved path is still under the parent (prevent traversal)
	if !strings.HasPrefix(absNew, absParent) {
		jsonResponse(w, map[string]interface{}{"error": "directory path escapes parent"})
		return
	}

	if err := os.MkdirAll(absNew, 0755); err != nil {
		jsonResponse(w, map[string]interface{}{"error": "cannot create directory: " + err.Error()})
		return
	}

	jsonResponse(w, map[string]interface{}{
		"path": absNew,
		"name": req.Name,
	})
}

func sanitizeFSPath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return absPath, nil
}

func (s *Server) cleanupSessionWorkDir(workDir string) {
	if workDir == "" {
		return
	}
	baseDir := s.cfg.WorkingDir
	if baseDir == "" {
		baseDir = filepath.Join(s.magicHome, "workspace")
	}
	absWorkDir, err := filepath.Abs(workDir)
	if err != nil {
		return
	}
	absBaseDir, err := filepath.Abs(baseDir)
	if err != nil {
		return
	}
	if !strings.HasPrefix(absWorkDir, absBaseDir+string(filepath.Separator)) && absWorkDir != absBaseDir {
		return
	}
	os.RemoveAll(absWorkDir)
}

func (s *Server) handleFSShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Path    string `json:"path"`
		Seconds int    `json:"seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	absPath, err := sanitizeFSPath(req.Path)
	if err != nil {
		jsonResponse(w, map[string]interface{}{"error": err.Error()})
		return
	}
	// 白名单校验：仅允许在允许的根目录内分享
	allowedRoots := s.getAllowedFSRoots()
	allowed := false
	for _, root := range allowedRoots {
		if IsPathWithin(absPath, root) {
			allowed = true
			break
		}
	}
	if !allowed {
		http.Error(w, "Path not allowed", http.StatusForbidden)
		return
	}
	info, err := os.Stat(absPath)
	if err != nil {
		jsonResponse(w, map[string]interface{}{"error": "path not found"})
		return
	}

	// Default lifetime 1h, clamped to [60s, 7d]
	ttl := time.Duration(req.Seconds) * time.Second
	if ttl <= 0 {
		ttl = time.Hour
	}
	if ttl < 60*time.Second {
		ttl = 60 * time.Second
	}
	if ttl > 7*24*time.Hour {
		ttl = 7 * 24 * time.Hour
	}

	tokenBytes := make([]byte, 24)
	if _, err := io.ReadFull(rand.Reader, tokenBytes); err != nil {
		jsonResponse(w, map[string]interface{}{"error": "failed to generate token"})
		return
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)

	tk := &ShareToken{
		Path:      absPath,
		Name:      filepath.Base(absPath),
		IsDir:     info.IsDir(),
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(ttl),
	}
	s.shareTokensMu.Lock()
	s.shareTokens[token] = tk
	// Best-effort cleanup of expired tokens
	for k, v := range s.shareTokens {
		if time.Now().After(v.ExpiresAt) {
			delete(s.shareTokens, k)
		}
	}
	s.shareTokensMu.Unlock()

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if fwd := r.Header.Get("X-Forwarded-Proto"); fwd != "" {
		scheme = fwd
	}
	host := r.Host
	if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
		host = fwd
	}
	shareURL := fmt.Sprintf("%s://%s/api/fs/shared/%s", scheme, host, token)

	jsonResponse(w, map[string]interface{}{
		"token":      token,
		"url":        shareURL,
		"path":       tk.Path,
		"name":       tk.Name,
		"is_dir":     tk.IsDir,
		"expires_at": tk.ExpiresAt.Unix(),
	})
}

func (s *Server) handleFSDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := r.URL.Query().Get("path")
	sessionID := r.URL.Query().Get("session_id")
	absPath, err := s.resolveFSPath(path, sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	info, err := os.Stat(absPath)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	if info.IsDir() {
		http.Error(w, "path is a directory", http.StatusBadRequest)
		return
	}

	file, err := os.Open(absPath)
	if err != nil {
		http.Error(w, "cannot open file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	filename := filepath.Base(absPath)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))

	io.Copy(w, file)
}

func (s *Server) handleFileUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}

	// Parse multipart form (32MB max memory)
	err := r.ParseMultipartForm(32 << 20)
	if err != nil {
		http.Error(w, "failed to parse form: "+err.Error(), 400)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "failed to get file: "+err.Error(), 400)
		return
	}
	defer file.Close()

	// Create uploads directory
	uploadsDir := filepath.Join(s.magicHome, "uploads")
	os.MkdirAll(uploadsDir, 0755)

	// Generate unique filename
	fileID := uuid.New().String()
	ext := filepath.Ext(header.Filename)
	if ext == "" {
		ext = ".bin"
	}
	filename := fileID + ext
	filepath_ := filepath.Join(uploadsDir, filename)

	// Save file
	out, err := os.Create(filepath_)
	if err != nil {
		http.Error(w, "failed to save file: "+err.Error(), 500)
		return
	}
	defer out.Close()

	_, err = io.Copy(out, file)
	if err != nil {
		http.Error(w, "failed to write file: "+err.Error(), 500)
		return
	}

	// Return file info
	// Use configured URL prefix if available, otherwise use relative path
	var fileURL string
	if s.cfg.Server.UploadURLPrefix != "" {
		fileURL = s.cfg.Server.UploadURLPrefix + "/" + filename
	} else {
		fileURL = "/api/uploads/" + filename
	}
	jsonResponse(w, map[string]interface{}{
		"id":       fileID,
		"name":     header.Filename,
		"filename": filename,
		"url":      fileURL,
		"size":     header.Size,
	})
}

func (s *Server) ensureSessionWorkDir(workDir string) error {
	if workDir == "" {
		return fmt.Errorf("workdir is required")
	}
	return os.MkdirAll(workDir, 0700)
}

func (s *Server) handleFSRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := r.URL.Query().Get("path")
	sessionID := r.URL.Query().Get("session_id")
	absPath, err := s.resolveFSPath(path, sessionID)
	if err != nil {
		jsonResponse(w, map[string]interface{}{"error": err.Error()})
		return
	}

	info, err := os.Stat(absPath)
	if err != nil {
		jsonResponse(w, map[string]interface{}{"error": "file not found"})
		return
	}
	if info.IsDir() {
		jsonResponse(w, map[string]interface{}{"error": "path is a directory"})
		return
	}

	// Size limit: 2MB for preview
	if info.Size() > 2*1024*1024 {
		jsonResponse(w, map[string]interface{}{"error": "file too large for preview (>2MB)"})
		return
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		jsonResponse(w, map[string]interface{}{"error": "cannot read file: " + err.Error()})
		return
	}

	contentType := "text/plain"
	ext := strings.ToLower(filepath.Ext(absPath))
	switch ext {
	case ".json":
		contentType = "application/json"
	case ".html", ".htm":
		contentType = "text/html"
	case ".css":
		contentType = "text/css"
	case ".js":
		contentType = "application/javascript"
	case ".md":
		contentType = "text/markdown"
	case ".xml":
		contentType = "application/xml"
	case ".csv":
		contentType = "text/csv"
	case ".png":
		contentType = "image/png"
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".gif":
		contentType = "image/gif"
	case ".bmp":
		contentType = "image/bmp"
	case ".webp":
		contentType = "image/webp"
	case ".svg":
		contentType = "image/svg+xml"
	}

	w.Header().Set("Content-Type", contentType)
	if strings.HasPrefix(contentType, "text/") || contentType == "application/json" || contentType == "application/javascript" {
		w.Header().Set("Content-Type", contentType+"; charset=utf-8")
	}
	w.Write(data)
}
