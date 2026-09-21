package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// fsServePrefix 是静态预览的挂载点，与服务端 mux 注册保持一处定义。
//
// 地址形态：/api/fs/serve/<sig>/<subpath>
//
//   - <sig>     自包含签名凭据，覆盖「被托管目录 + 过期时间」（见 signFSServeDir）。
//   - <subpath> 该目录内的相对路径，空表示目录根（返回 index.html）。
//
// 为什么凭据必须放在**路径**里，而不是 query / header / cookie：
//
//  1. 相对资源引用按 RFC 3986 §5.3 只做路径合并（merge），query 会被整段丢弃。
//     index.html 里的 href="assets/style.css" 解析成 /api/fs/serve/assets/style.css，
//     原先挂在 query 上的 ?path=&token= 必然全部丢失，于是每个 css/js/图片都 401。
//  2. 子资源请求带不上 Authorization header（iframe 里的 img/link/script 无法自定义 header）。
//  3. cookie 在 <iframe sandbox="allow-scripts">（刻意不带 allow-same-origin）下
//     不会随子资源请求发送——已用真实浏览器实测确认：
//     allow-same-origin 时子资源收到 cookie，去掉后 cookie 为空、请求变回 401。
//     而正是「去掉 allow-same-origin」这一步阻断了被预览 HTML 读取
//     window.parent.document 与 localStorage.auth_token，是必须保留的隔离。
//
// 路径段是唯一同时满足「被子资源自动继承」与「不依赖任何浏览器凭据」的位置。
const fsServePrefix = "/api/fs/serve"

// fsServeSigTTL 是预览签名的有效期。预览页是长驻 iframe，太短会在使用中途
// 失效；而签名泄漏的影响面仅限于被签名的那个目录（不含其他 API），因此给一天。
const fsServeSigTTL = fsTicketStreamTTL

var errFSServeSig = errors.New("invalid or expired preview signature")

// signFSServeDir 为目录签发预览凭据。
//
// 预览凭据就是一张 scope=serve、path=托管目录的票据，因此这里直接复用通用
// 票据签发（见 fs_ticket.go）：全站只有一条 HMAC 签名路径，不存在第二套
// 需要单独审计的密码学实现。
func (s *Server) signFSServeDir(dir string, exp time.Time) (string, error) {
	return s.signFSTicket(fsTicket{Scope: fsScopeServe, Path: dir, Exp: exp})
}

// parseFSServeSig 校验凭据并还原被托管目录。
func (s *Server) parseFSServeSig(sig string, now time.Time) (string, error) {
	tk, err := s.parseFSTicket(sig, now)
	if err != nil {
		return "", errFSServeSig
	}
	// 作用域必须严格匹配。否则一张 read/download 票据就能被塞进
	// /api/fs/serve/<sig>/ 的路径位，把「读一个文件」偷换成「托管整个目录」——
	// 相邻作用域之间正是最容易被忽略的提权缝隙。
	if tk.Scope != fsScopeServe || tk.Path == "" {
		return "", errFSServeSig
	}
	return tk.Path, nil
}

// handleFSServeSign 用登录凭据换取一张票据（POST /api/fs/sign）。
//
// 走 requireAuth：这是唯一需要登录凭据的时刻，前端用 fetch + Authorization
// 头调用，因此凭据不必出现在任何 URL 里。返回地址里带的是作用域受限、有硬性
// 过期时间的票据，可以安全地交给 <img src> / <a href> / EventSource 使用。
//
// scope 决定票据用途，缺省为 serve——向后兼容：老前端不带 scope，语义正是
// 「给我一个静态预览地址」。
func (s *Server) handleFSServeSign(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Path      string `json:"path"`
		SessionID string `json:"session_id"`
		Scope     string `json:"scope"`
		Hidden    bool   `json:"hidden"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	scope := strings.TrimSpace(req.Scope)
	if scope == "" {
		scope = fsScopeServe
	}

	// events 票据不绑定任何文件，因此不需要 path/session。
	if scope == fsScopeEvents {
		sig, err := s.signFSTicket(fsTicket{
			Scope: fsScopeEvents,
			Exp:   time.Now().Add(fsTicketStreamTTL),
		})
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonResponse(w, map[string]interface{}{
			"url":        "/api/events?sig=" + url.QueryEscape(sig),
			"expires_in": int(fsTicketStreamTTL.Seconds()),
		})
		return
	}

	// uploads 票据的 path 是上传根内的相对路径，不走工作区 resolveFSPath：
	// 会话工作区边界对上传目录没有意义，上传内容的边界是上传根。
	if scope == fsScopeUploads {
		rel, err := normalizeUploadTicketPath(req.Path)
		if err != nil {
			jsonError(w, http.StatusBadRequest, "invalid upload path")
			return
		}
		abs, err := s.resolveUploadTicketPath(rel)
		if err != nil {
			jsonError(w, http.StatusNotFound, "attachment not found")
			return
		}
		if fi, statErr := os.Stat(abs); statErr != nil || fi.IsDir() {
			jsonError(w, http.StatusNotFound, "attachment not found")
			return
		}
		u, err := s.signTicketURL(fsTicket{
			Scope: fsScopeUploads,
			Path:  rel,
			Exp:   time.Now().Add(fsTicketActionTTL),
		})
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonResponse(w, map[string]interface{}{
			"url":        u,
			"expires_in": int(fsTicketActionTTL.Seconds()),
		})
		return
	}

	absPath, err := s.resolveFSPath(req.Path, req.SessionID)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := os.Stat(absPath)
	if err != nil {
		jsonError(w, http.StatusNotFound, "resource not found")
		return
	}

	switch scope {
	case fsScopeRead, fsScopeDownload, fsScopeZip:
		// zip 可以打包目录；read/download 面对目录没有意义，提前拒绝，
		// 免得签出一张使用时必然 400 的票据。
		if info.IsDir() && scope != fsScopeZip {
			jsonError(w, http.StatusBadRequest, "path is a directory")
			return
		}
		opts := ""
		if scope == fsScopeZip && req.Hidden {
			opts = "h"
		}
		u, err := s.signTicketURL(fsTicket{
			Scope: scope,
			Path:  absPath,
			Opts:  opts,
			Exp:   time.Now().Add(fsTicketActionTTL),
		})
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonResponse(w, map[string]interface{}{
			"url":        u,
			"expires_in": int(fsTicketActionTTL.Seconds()),
		})

	case fsScopeServe:
		// 目录 → 托管该目录，入口是它自己的 index.html。
		// 文件 → 托管它所在的目录，入口就是该文件名。
		// 两种情况下 index.html 里的相对引用都能映射回原始位置。
		dir, entry := absPath, ""
		if !info.IsDir() {
			dir, entry = filepath.Dir(absPath), filepath.Base(absPath)
			if entry == "index.html" {
				// 入口落在目录根上，浏览器的 base 直接是目录，语义更干净
				entry = ""
			}
		}

		// 目录没有 index.html 时提前给出可读的错误，而不是让 iframe 里收到一个
		// 403（iframe 会把它当普通文档 @load，前端只能看到一片空白）。
		if info.IsDir() {
			if idx, statErr := os.Stat(filepath.Join(dir, "index.html")); statErr != nil || idx.IsDir() {
				jsonError(w, http.StatusBadRequest, "directory has no index.html to preview")
				return
			}
		}

		exp := time.Now().Add(fsServeSigTTL)
		sig, err := s.signFSServeDir(dir, exp)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonResponse(w, map[string]interface{}{
			"url":        fsServePrefix + "/" + sig + "/" + url.PathEscape(entry),
			"expires_in": int(fsServeSigTTL.Seconds()),
		})

	default:
		jsonError(w, http.StatusBadRequest, "unknown ticket scope: "+scope)
	}
}

// handleFSServe 提供静态网页预览（GET/HEAD /api/fs/serve/<sig>/<subpath>）。
//
// 刻意不套 requireAuth：凭据在路径里，且必须能被 iframe 内的相对子资源请求
// 继承。这里既不认 Authorization header，也不认 ?token=，避免留下未经签名的
// 旁路——签名是这条链路上唯一的准入条件。
func (s *Server) handleFSServe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, fsServePrefix+"/")
	sig, sub, _ := strings.Cut(rest, "/")

	dir, err := s.parseFSServeSig(sig, time.Now())
	if err != nil {
		http.Error(w, "invalid or expired preview link", http.StatusForbidden)
		return
	}

	target, err := safeJoinFSServe(dir, sub)
	if err != nil {
		http.Error(w, "invalid preview path", http.StatusForbidden)
		return
	}

	info, err := os.Stat(target)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// 目录列举必须关闭：无论请求的是托管根还是一个「恰好存在」的子目录，
	// 只有目录内确实有 index.html 时才继续，否则 403。
	// 这里绝不能再回落到 http.FileServer —— 它在缺少 index.html 时会
	// 直接生成目录列表（实测会列出 .env 等所有文件名）。
	if info.IsDir() {
		idx := filepath.Join(target, "index.html")
		idxInfo, statErr := os.Stat(idx)
		if statErr != nil || idxInfo.IsDir() {
			http.Error(w, "directory listing is disabled", http.StatusForbidden)
			return
		}
		target, info = idx, idxInfo
	}

	serveFSServeFile(w, r, target, info)
}

// safeJoinFSServe 把 subpath 解析到 dir 之内，拒绝一切越界尝试。
//
// 被拒绝的情况：显式 ".." 路径段、NUL 字符、以及指向 dir 之外的符号链接。
func safeJoinFSServe(dir, sub string) (string, error) {
	sub = strings.TrimPrefix(sub, "/")
	if sub == "" {
		return dir, nil
	}
	if strings.ContainsRune(sub, 0) {
		return "", errors.New("invalid path")
	}
	// 显式拒绝 ".." 段：path.Clean 会把它夹在根上（"../x" → "/x"），那样虽然
	// 安全，却会静默地把请求指向另一个文件，不如直接报错来得可预期。
	for _, seg := range strings.Split(sub, "/") {
		if seg == ".." {
			return "", errors.New("path escapes preview root")
		}
	}

	joined := filepath.Join(dir, filepath.FromSlash(path.Clean("/"+sub)))
	resolved, err := filepath.EvalSymlinks(joined)
	if err != nil {
		// 不存在（或断链）：交给调用方按 404 处理
		return joined, nil
	}
	if !fsServeWithin(dir, resolved) {
		return "", errors.New("path escapes preview root")
	}
	return resolved, nil
}

// fsServeWithin 判断 target 是否位于 root 之内（先解析 root 自身的符号链接，
// 避免 /tmp 之类本身就是软链的路径被误判）。
func fsServeWithin(root, target string) bool {
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// serveFSServeFile 输出单个文件。
//
// 用 http.ServeContent 而不是 http.ServeFile：ServeFile 会对以 /index.html
// 结尾的请求发 301 跳到 "./"（白跑一次往返），并且自己带了一套目录处理逻辑，
// 而这里的目录解析（含「无 index.html 则 403」）必须完全由我们掌控。
// ServeContent 同时保留了 Range / 条件请求支持，视频拖拽进度条依赖它。
func serveFSServeFile(w http.ResponseWriter, r *http.Request, name string, info os.FileInfo) {
	f, err := os.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	http.ServeContent(w, r, filepath.Base(name), info.ModTime(), f)
}
