package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/magicwubiao/go-magic/internal/session"
	"github.com/magicwubiao/go-magic/pkg/types"
	"github.com/magicwubiao/go-magic/pkg/utils"
)

func (s *Server) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")

	// Check for messages endpoint
	if strings.HasSuffix(path, "/messages") {
		sessionID := strings.TrimSuffix(path, "/messages")
		s.handleSessionMessages(w, r, sessionID)
		return
	}

	// Check for stream endpoint
	if strings.HasSuffix(path, "/stream") {
		sessionID := strings.TrimSuffix(path, "/stream")
		s.handleSessionStream(w, r, sessionID)
		return
	}

	// Check for running-state probe (mobile background recovery: the browser
	// kills the SSE connection when backgrounded; the frontend polls this to
	// learn whether the turn is still executing server-side)
	if strings.HasSuffix(path, "/running") {
		sessionID := strings.TrimSuffix(path, "/running")
		s.handleSessionRunning(w, r, sessionID)
		return
	}

	// Check for explicit generation cancel (user taps Stop)
	if strings.HasSuffix(path, "/cancel") {
		sessionID := strings.TrimSuffix(path, "/cancel")
		s.handleSessionCancel(w, r, sessionID)
		return
	}

	// Check for reset endpoint
	if strings.HasSuffix(path, "/reset") {
		sessionID := strings.TrimSuffix(path, "/reset")
		s.handleSessionReset(w, r, sessionID)
		return
	}

	// Check for latest-descendant
	if strings.HasSuffix(path, "/latest-descendant") {
		sessionID := strings.TrimSuffix(path, "/latest-descendant")
		jsonResponse(w, map[string]interface{}{
			"requested_session_id": sessionID,
			"session_id":           sessionID,
			"path":                 []string{sessionID},
			"changed":              false,
		})
		return
	}

	// Check for goals endpoint - get goals linked to this session
	if strings.HasSuffix(path, "/goals") {
		sessionID := strings.TrimSuffix(path, "/goals")
		s.handleSessionGoals(w, r, sessionID)
		return
	}

	id := path
	if id == "" {
		http.Error(w, "not found", 404)
		return
	}

	if s.sessionStore == nil {
		http.Error(w, "session store not available", 500)
		return
	}

	dbSession, err := s.sessionStore.LoadSession(context.Background(), id)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}

	switch r.Method {
	case "GET":
		jsonResponse(w, convertDBSessionToAPI(dbSession))
	case "PUT":
		var req struct {
			Name    string  `json:"name"`
			WorkDir *string `json:"work_dir"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		if req.Name != "" {
			if err := s.sessionStore.RenameSession(context.Background(), id, req.Name); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
		}
		if req.WorkDir != nil {
			if dbSession.WorkDirUserSet {
				http.Error(w, "work directory already set by user and cannot be changed", 400)
				return
			}
			if err := s.sessionStore.UpdateWorkDir(context.Background(), id, *req.WorkDir, true); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			// 目录级共享记忆 + 静态规则链：工作目录一旦设置，把已缓存 agent 的
			// 记忆 scope 绑定到该目录归一化键（召回/沉淀落目录桶），并开启从
			// 该目录向上发现规则文件（AGENTS.md 等）的注入。
			s.agentsMu.Lock()
			if a, ok := s.agents[id]; ok && a != nil {
				if (s.cfg != nil && s.cfg.Memory.Enabled) || s.cortexMgr != nil {
					a.SetMemoryScope(normalizeDirScope(*req.WorkDir))
				}
				if s.staticRulesEnabled() {
					a.SetRuleDir(*req.WorkDir)
				}
			}
			s.agentsMu.Unlock()
		}
		jsonResponse(w, map[string]bool{"ok": true})
	case "DELETE":
		var req struct {
			DeleteFiles bool `json:"delete_files"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		s.sessionStore.DeleteSession(context.Background(), id)
		s.agentsMu.Lock()
		delete(s.agents, id)
		s.agentsMu.Unlock()

		if req.DeleteFiles {
			if dbSession.WorkDir != "" && !dbSession.WorkDirUserSet {
				s.cleanupSessionWorkDir(dbSession.WorkDir)
			} else if dbSession.WorkDir != "" {
				// User-set workdir: never remove the directory itself, but
				// still release the materialized attachment copies.
				os.RemoveAll(filepath.Join(dbSession.WorkDir, workdirAttachmentsDir))
			}
			// Always try to clean per-session uploads so deleting a chat also
			// releases disk space used by its attachments.
			s.cleanupSessionUploads(id)
		}

		jsonResponse(w, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *Server) handleSessionReset(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}

	// Reset agent for this session
	s.agentsMu.Lock()
	delete(s.agents, sessionID)
	s.agentsMu.Unlock()

	// Reset session messages in DB
	if s.sessionStore != nil {
		if sess, err := s.sessionStore.LoadSession(context.Background(), sessionID); err == nil {
			sess.Messages = []types.Message{}
			sess.InputTokens = 0
			sess.OutputTokens = 0
			sess.CacheReadTokens = 0
			sess.UpdatedAt = time.Now()
			s.sessionStore.SaveSession(context.Background(), sess)
		}
	}

	jsonResponse(w, map[string]bool{"ok": true})
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		limitStr := r.URL.Query().Get("limit")
		offsetStr := r.URL.Query().Get("offset")

		limit := 20
		if limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
				limit = l
			}
		}
		offset := 0
		if offsetStr != "" {
			if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
				offset = o
			}
		}

		if s.sessionStore == nil {
			jsonResponse(w, map[string]interface{}{"sessions": []Session{}, "total": 0, "limit": limit})
			return
		}

		dbSessions, err := s.sessionStore.ListSessions(context.Background(), "")
		if err != nil {
			jsonResponse(w, map[string]interface{}{"sessions": []Session{}, "total": 0, "limit": limit})
			return
		}

		// Convert to API format
		apiSessions := make([]*Session, 0, len(dbSessions))
		for _, sess := range dbSessions {
			apiSessions = append(apiSessions, convertDBSessionToAPI(sess))
		}

		total := len(apiSessions)
		if offset > total {
			offset = total
		}
		end := offset + limit
		if end > total {
			end = total
		}

		jsonResponse(w, map[string]interface{}{
			"sessions": apiSessions[offset:end],
			"total":    total,
			"limit":    limit,
			"offset":   offset,
		})
	case "POST":
		var req struct {
			Name     string `json:"name"`
			Title    string `json:"title"`
			Model    string `json:"model"`
			Platform string `json:"platform"`
			WorkDir  string `json:"work_dir"`
		}
		// Allow empty body for simple session creation
		if r.ContentLength > 0 {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid request", 400)
				return
			}
		}

		now := time.Now()
		name := req.Name
		if name == "" {
			name = req.Title
		}
		if name == "" {
			name = fmt.Sprintf("Chat %s", now.Format("2006-01-02 15:04"))
		}

		sessionID := uuid.New().String()
		platform := req.Platform
		if platform == "" {
			platform = "web"
		}
		model := req.Model
		if model == "" {
			model = s.cfg.GetCurrentModel()
		}

		workDir := req.WorkDir
		workDirUserSet := false
		if workDir == "" {
			workDir = s.getSessionWorkDir(sessionID, name)
		} else {
			workDirUserSet = true
		}

		if err := s.ensureSessionWorkDir(workDir); err != nil {
			http.Error(w, "failed to create session workdir: "+err.Error(), 500)
			return
		}

		newSession := &session.Session{
			ID:              sessionID,
			Profile:         s.cfg.Profile,
			Platform:        platform,
			Model:           model,
			WorkDir:         workDir,
			WorkDirUserSet:  workDirUserSet,
			Messages:        []types.Message{},
			InputTokens:     0,
			OutputTokens:    0,
			CacheReadTokens: 0,
			CreatedAt:       now,
			UpdatedAt:       now,
		}

		if s.sessionStore != nil {
			if err := s.sessionStore.SaveSession(context.Background(), newSession); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}

		apiSess := convertDBSessionToAPI(newSession)
		if apiSess != nil {
			apiSess.Title = name
			apiSess.Model = req.Model
		}
		jsonResponse(w, apiSess)
	default:
		http.Error(w, "method not allowed", 405)
	}
}

// SessionDirGroup groups sessions that share a working directory the user
// explicitly set at least once (chat 页面"按工作目录查看会话"的弹层数据源)。
type SessionDirGroup struct {
	Dir      string     `json:"dir"`
	Sessions []*Session `json:"sessions"`
}

// handleSessionsDirGroups aggregates web sessions whose working directory was
// user-set, grouped by directory (newest activity first within each group).
// 只统计用户显式设置过工作目录的会话，网关/TUI 等平台的会话不参与分组。
func (s *Server) handleSessionsDirGroups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	empty := map[string]interface{}{"groups": []SessionDirGroup{}, "total_sessions": 0}
	if s.sessionStore == nil {
		jsonResponse(w, empty)
		return
	}

	dbSessions, err := s.sessionStore.ListSessionsByUserWorkDir(r.Context())
	if err != nil {
		jsonResponse(w, empty)
		return
	}

	// 按规范化路径聚合（清理尾部分隔符等），组内按最近活动倒序
	byDir := map[string][]*Session{}
	total := 0
	for _, dbSess := range dbSessions {
		apiSess := convertDBSessionToAPI(dbSess)
		if apiSess == nil || strings.TrimSpace(apiSess.WorkDir) == "" {
			continue
		}
		key := filepath.Clean(apiSess.WorkDir)
		byDir[key] = append(byDir[key], apiSess)
		total++
	}

	groups := make([]SessionDirGroup, 0, len(byDir))
	for dir, list := range byDir {
		sort.Slice(list, func(i, j int) bool { return list[i].LastActive > list[j].LastActive })
		groups = append(groups, SessionDirGroup{Dir: dir, Sessions: list})
	}
	// 组间按组内最近一条会话的活动时间排序（最新在前）
	sort.Slice(groups, func(i, j int) bool {
		ai, bj := groups[i].Sessions, groups[j].Sessions
		if len(ai) == 0 {
			return false
		}
		if len(bj) == 0 {
			return true
		}
		return ai[0].LastActive > bj[0].LastActive
	})

	jsonResponse(w, map[string]interface{}{
		"groups":         groups,
		"total_sessions": total,
	})
}

// chatPayload 是提交一条聊天消息的公共请求体（/stream 与 /messages 共用）。
type chatPayload struct {
	Content   string   `json:"content"`
	Images    []string `json:"images"`
	ImageURLs []string `json:"imageUrls"` // uploaded /api/uploads/ path per image (same order) — used as the persisted reference
	// 每张图片的原始文件名（与 images 同序）。图片走多模态通道，不在 files
	// 里，落库时要靠它把「缩略图旁边显示什么名字」记下来；缺省时后端会回查
	// uploads 元数据兜底（见 uploadDisplayName）。
	ImageNames []string `json:"imageNames"`
	Files      []struct {
		Name     string `json:"name"`
		Filename string `json:"filename"`
		URL      string `json:"url"`
		Data     string `json:"data"` // legacy base64 data URL — kept for back-compat only
	} `json:"files"`
}

// parsedChatPayload 是 chatPayload 解析后的结果：模型可见的 content parts、
// 落库用的引用、以及需要在执行前物化到工作目录的附件。
type parsedChatPayload struct {
	content            string
	contentParts       []types.ContentPart
	imageURLRefs       []string
	imageNames         []string
	pendingMaterialize []uploadToMaterialize
}

// parseChatPayload 解析请求体并构建 content parts。
//
// 这段逻辑原本内联在 handleSessionStream 里；/messages 的 POST 也需要同样的
// 处理（它同样是"提交一条消息"，只是不等结果），因此抽出来共用，
// 避免两个入口对附件/图片的处理出现分叉。
// errMsg 非空表示是调用方应回给客户端的 4xx 校验错误。
func (s *Server) parseChatPayload(r *http.Request, sessionID string) (*parsedChatPayload, string) {
	var payload chatPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return nil, "failed to decode payload: " + err.Error()
	}

	out := &parsedChatPayload{content: payload.Content}

	// Parse images from JSON body field. Inline base64 payloads are capped —
	// they ride in the request body, get embedded into the agent history for
	// every subsequent turn, and would otherwise let a handful of large
	// screenshots blow past the 16 MiB body limit and the model's context.
	const (
		maxImagesPerMessage  = 8
		maxImagePayloadBytes = 12 << 20 // 12 MiB of base64 payload in total
	)
	if len(payload.Images) > 0 {
		if len(payload.Images) > maxImagesPerMessage {
			return nil, fmt.Sprintf("too many images: %d attached, max %d per message", len(payload.Images), maxImagesPerMessage)
		}
		totalImageBytes := 0
		for i, imgURL := range payload.Images {
			if imgURL == "" {
				continue
			}
			if idx := strings.Index(imgURL, ","); idx >= 0 {
				totalImageBytes += len(imgURL) - idx - 1
			} else {
				totalImageBytes += len(imgURL)
			}
			if totalImageBytes > maxImagePayloadBytes {
				return nil, "images too large: total inline payload exceeds 12 MiB — please attach fewer or smaller images"
			}
			ref := ""
			if i < len(payload.ImageURLs) {
				ref = payload.ImageURLs[i]
			}
			name := ""
			if i < len(payload.ImageNames) {
				name = payload.ImageNames[i]
			}
			out.contentParts = append(out.contentParts, types.ContentPart{
				Type:     "image_url",
				ImageURL: &types.MediaURL{URL: imgURL},
			})
			out.imageURLRefs = append(out.imageURLRefs, ref)
			out.imageNames = append(out.imageNames, name)
		}
	}

	// Parse files from JSON body field. Priority for resolving content:
	//   1. Filename-based lookup on local uploads (most common path — file is
	//      already on disk from /api/upload).
	//   2. Direct data URL provided by the client (legacy fallback; not used
	//      by the current frontend).
	//   3. Fetch external URL (when uploaded somewhere else).
	// All payloads live in the request body — the URL stays clean.
	uploadsDir := s.uploadsRoot()
	// 需要在回合开始前物化到会话工作目录的附件（agent 的文件工具被限制在
	// 工作目录内，够不到 <magicHome>/uploads 根目录）。物化在入队前完成，
	// 保证排队时间再长也不受上传目录清理影响。
	for _, f := range payload.Files {
		var dataURL string
		handled := false // set when we already emitted a content part for this file

		switch {
		case f.Filename != "":
			// Resolve relative path "/api/uploads/<session>/<file>" into a
			// real file by stripping the prefix.
			localName := f.Filename
			if f.URL != "" {
				idx := strings.IndexAny(f.URL, "?")
				clean := f.URL
				if idx >= 0 {
					clean = clean[:idx]
				}
				if strings.HasPrefix(clean, "/api/uploads/") {
					localName = strings.TrimPrefix(clean, "/api/uploads/")
				}
			}
			localName = filepath.Base(localName)
			// search the per-session bucket first, then _shared, then flat root
			candidates := []string{
				filepath.Join(uploadsDir, sessionID, localName),
				filepath.Join(uploadsDir, "_shared", localName),
				filepath.Join(uploadsDir, localName),
			}
			for _, candidate := range candidates {
				data, err := os.ReadFile(candidate)
				if err == nil {
					// Remember for workdir materialization: the agent's file
					// tools are sandboxed to the workdir and cannot reach the
					// uploads root, so we copy the file over at stream time.
					out.pendingMaterialize = append(out.pendingMaterialize, uploadToMaterialize{Name: f.Name, Src: candidate})
					// Zip-based Office documents (xlsx/docx) are unreadable
					// to the LLM as raw bytes — extract their text here so
					// the model gets real content instead of hunting the
					// filesystem for a file its tools cannot reach.
					if parsed, ok := parseOfficeText(f.Name, data); ok {
						payloadText := fmt.Sprintf("（以下内容从附件 %s 自动提取）\n\n%s", f.Name, parsed)
						out.contentParts = append(out.contentParts, types.ContentPart{
							Type: "file",
							File: &types.FileInfo{
								Name:     f.Name,
								MimeType: "text/plain",
								URL:      f.URL,
								Contents: "data:text/plain;base64," + base64.StdEncoding.EncodeToString([]byte(payloadText)),
							},
						})
						handled = true
						break
					}
					mimeType := mimeFromFilename(f.Name)
					// Extensionless or unknown files (LICENSE, Makefile, .bin
					// fallbacks...) stay at octet-stream here. Sniff the real
					// content and override so convertFilePart doesn't file
					// them under "binary — not readable" and drop the payload.
					if mimeType == "application/octet-stream" && len(data) > 0 {
						sniffed := http.DetectContentType(data[:min(len(data), 512)])
						if i := strings.Index(sniffed, ";"); i >= 0 {
							sniffed = strings.TrimSpace(sniffed[:i])
						}
						switch {
						case strings.HasPrefix(sniffed, "text/"):
							// Normalize: isText() matches on exact map keys,
							// and only "text/plain"/"text/html" are listed.
							if sniffed == "text/html" || sniffed == "text/plain" {
								mimeType = sniffed
							} else {
								mimeType = "text/plain"
							}
						case sniffed != "application/octet-stream":
							// image/*, application/pdf etc. — trust the sniff.
							mimeType = sniffed
						}
					}
					base64Data := base64.StdEncoding.EncodeToString(data)
					dataURL = fmt.Sprintf("data:%s;base64,%s", mimeType, base64Data)
					break
				}
			}
		case f.Data != "":
			// Legacy fallback: client supplied a data URL directly.
			dataURL = f.Data
		case f.URL != "":
			// External URL — fetch via the default client. We deliberately
			// do NOT forward the request's Authorization header here, since
			// it's our own dashboard token and the external service wouldn't
			// understand it.
			resp, err := http.Get(f.URL)
			if err == nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
					base64Data := base64.StdEncoding.EncodeToString(data)
					dataURL = fmt.Sprintf("data:application/octet-stream;base64,%s", base64Data)
				}
			}
		}

		if handled {
			continue
		}

		if dataURL != "" {
			// Derive the final mime (dataURL prefix wins — it reflects both
			// extension and sniff) and carry size/mime into FileInfo so
			// provider/convert doesn't re-guess from the filename alone.
			finalMime := "application/octet-stream"
			if strings.HasPrefix(dataURL, "data:") {
				if rest := dataURL[5:]; strings.Contains(rest, ";") {
					finalMime = rest[:strings.Index(rest, ";")]
				}
			}
			out.contentParts = append(out.contentParts, types.ContentPart{
				Type: "file",
				File: &types.FileInfo{
					Name:     f.Name,
					MimeType: finalMime,
					URL:      f.URL,
					Contents: dataURL,
				},
			})
		}
	}

	return out, ""
}

// handleSessionStream POST /api/sessions/{id}/stream
//
// 语义（改造后）：
//   - 普通模式：把消息放入会话队列，然后保持 SSE 连接监听该会话的事件总线，
//     转发回合事件直到本回合结束（done）。回合由队列 worker 串行执行，
//     一个会话永远只有一个回合在跑，后续消息排队等待。
//   - 附着模式（?attach=1）：不提交消息，只把连接挂到事件总线，用于断线
//     （手机切后台）或回合进行中打开页面时续接实时输出。
//
// 之所以不再在 handler 里同步跑 agent：handler 的生命周期受 HTTP 连接
// 约束，而回合的生命周期只受超时与用户停止约束。排队功能要求"提交"与
// "执行"解耦——提交请求可以立刻返回，回合在后台按 FIFO 串行执行。
func (s *Server) handleSessionStream(w http.ResponseWriter, r *http.Request, sessionID string) {
	// Only POST is allowed. The frontend previously used GET with everything
	// stuffed into query params (token, base64 file contents, etc.) which leaks
	// secrets via browser history, Referer, reverse-proxy access logs, and
	// hits URL-length limits. POST keeps the payload in the body and lets us
	// authenticate via the Authorization header instead of the URL.
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.provider == nil {
		http.Error(w, "LLM provider not configured. Please add a provider in Models page.", 400)
		return
	}

	// Cap body size to a reasonable limit. 16 MiB is well above any realistic
	// chat payload but stops a malicious caller from streaming 10 GB into us.
	const maxStreamBodyBytes = 16 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxStreamBodyBytes)
	defer r.Body.Close()

	// 附着模式：只把连接挂到会话的事件总线上，不发送新消息（见下方说明）。
	attach := r.URL.Query().Get("attach") == "1"

	// Validate that we have at least content or media to send. 附着模式下
	// 不带消息，因此该校验只对普通发送生效。
	if !attach {
		if s.provider == nil {
			http.Error(w, "LLM provider not configured. Please set up a provider in Settings.", http.StatusServiceUnavailable)
			return
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Disable server-wide WriteTimeout for SSE streams (long-running connections)
	rc := http.NewResponseController(w)
	rc.SetWriteDeadline(time.Time{})

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", 500)
		return
	}

	sseW := newSSEWriter(w, flusher)
	writeSSE := sseW.Write

	// ------------------------------------------------------------------
	// 附着模式（?attach=1）：不发送任何新消息，只是把当前 SSE 连接挂到该
	// 会话的事件总线上，用于断线（手机切后台）或回合进行中打开页面时续接
	// 实时输出。没有回合在跑时不发 done，连接由 keepAlive 维持，直到客户端
	// 主动断开——前端若在附着期间发消息会关闭旧连接并以普通模式重开。
	// ------------------------------------------------------------------
	if attach {
		writeSSE("data: {\"type\":\"connected\"}\n\n")

		queue := s.sessionQueueFor(sessionID)
		snk := queue.addSink()
		defer func() {
			snk.cancel()
			queue.closeSink(snk)
		}()

		streamCtx, streamCancel := context.WithCancel(context.Background())
		defer streamCancel()
		startSSEClientWatch(r.Context(), streamCancel)

		if queue.snapshot().running {
			// 回合正在进行：只挂监听，实时事件照常转发（不发 done——
			// 本连接是附着者，回合结果由回合自身的结束信号负责）。
			s.forwardTurnEvents(streamCtx, writeSSE, snk)
			return
		}

		// 空闲：报一个 started=false，让前端立刻知道"这一刻没有回合在跑"，
		// 随后保持心跳（若用户在此期间发消息，worker 会推 turn_started）。
		writeSSE("data: {\"type\":\"stream_started\",\"started\":false}\n\n")
		s.keepAlive(streamCtx, writeSSE, queue)
		return
	}

	parsed, errMsg := s.parseChatPayload(r, sessionID)
	if parsed == nil {
		writeSSE("data: " + sseErrorPayload(fmt.Errorf("%s", errMsg)) + "\n\n")
		return
	}
	if parsed.content == "" && len(parsed.contentParts) == 0 {
		writeSSE("data: " + sseErrorPayload(fmt.Errorf("content or media required")) + "\n\n")
		return
	}
	defer sseW.Close()

	aiAgent := s.getOrCreateAgent(sessionID)
	if aiAgent == nil {
		writeSSE("data: " + sseErrorPayload(errProviderNotConfigured{}) + "\n\n")
		return
	}

	// Inject the session's working directory into the turn run context.
	// 解析阶段就固定下来：排队消息可能在很久以后才被执行，期间用户可能改了
	// 会话的工作目录——一条消息的工作目录必须与发送它的那一刻一致。
	turnRun := &turnRunCtx{fileOps: NewTurnFileOpTracker()}
	if s.sessionStore != nil {
		if sess, err := s.sessionStore.LoadSession(context.Background(), sessionID); err == nil {
			turnRun.workDir = sess.WorkDir
			turnRun.workDirUserSet = sess.WorkDirUserSet
		}
	}

	// Materialize uploaded attachments into the session workdir so the model's
	// file tools (sandboxed to the workdir) can read them. The canonical copies
	// under <magicHome>/uploads stay untouched — they remain the source of
	// truth for GC and audit. 必须在入队前完成：排队项若携带上传引用，
	// 文件可能在真正执行前被清理，届时再物化就晚了。
	var materializeSummary string
	if len(parsed.pendingMaterialize) > 0 {
		materializeSummary = materializeUploads(parsed.pendingMaterialize, turnRun.workDir)
		if materializeSummary == "" {
			// 工作目录不可用（尚未创建？）：至少把服务器的规范路径告诉模型。
			lines := make([]string, 0, len(parsed.pendingMaterialize))
			for _, it := range parsed.pendingMaterialize {
				lines = append(lines, fmt.Sprintf("- %s → %s", it.Name, it.Src))
			}
			materializeSummary = "附件已保存在以下服务器路径（工作目录暂不可用，如需读取请告知用户）：\n" + strings.Join(lines, "\n")
		}
	}

	// 落库用的 content parts 在此处（入队前）算好：队列项里保留的是轻量引用，
	// 而 persistedContentParts 需要把 inline 载荷换成上传路径引用。
	persistedParts := persistedContentParts(parsed.contentParts, parsed.imageURLRefs, parsed.imageNames, s.uploadDisplayName)

	item, dup := s.enqueueChatTurn(sessionID, parsed.content, parsed.contentParts, persistedParts, turnRun, materializeSummary)
	if item == nil {
		writeSSE("data: " + sseErrorPayload(fmt.Errorf("message not accepted, please retry")) + "\n\n")
		return
	}

	writeSSE("data: {\"type\":\"connected\"}\n\n")

	queue := s.sessionQueueFor(sessionID)
	snk := queue.addSink()
	defer func() {
		snk.cancel()
		queue.closeSink(snk)
	}()

	// 排队位置：入队后立刻回给前端，避免"点了发送但界面没反应"。
	if !dup {
		writeSSE(sseQueuedPayload(item, queue))
	}

	streamCtx, streamCancel := context.WithCancel(context.Background())
	defer streamCancel()
	startSSEClientWatch(r.Context(), streamCancel)

	s.forwardTurnEvents(streamCtx, writeSSE, snk)
}

// enqueueChatTurn 把一条消息放入会话队列并确保 worker 在跑。重复提交
// （同 session 同内容、队列里已存在完全相同的待执行项）会被合并，避免
// 弱网重试在前端留下两条一模一样的排队气泡。
// 返回 nil 表示队列正在回收（调用方应让客户端重试）。
func (s *Server) enqueueChatTurn(sessionID, content string, contentParts []types.ContentPart, persistedParts []types.ContentPart, run *turnRunCtx, materializeSummary string) (*queuedTurn, bool) {
	// 物化摘要先并入 content parts，再统一剥离 inline 载荷——保持与改造前
	// 完全一致的模型输入顺序（用户文本 → 图片/文件 → 物化摘要）。
	if materializeSummary != "" {
		contentParts = append(contentParts, types.ContentPart{Type: "text", Text: materializeSummary})
	}
	// 所有需要字节的步骤（物化、Office 文本抽取）都已在入队前完成，此处把
	// inline base64 降级为轻量引用：排队项可能等上几分钟，带着几十 MB 的
	// base64 排队既占内存又会随会话落库膨胀。
	if slimmed := stripInlineMediaParts(contentParts); slimmed != nil {
		contentParts = slimmed
	}

	item := &queuedTurn{
		id:             uuid.NewString(),
		content:        content,
		contentParts:   contentParts,
		persistedParts: persistedParts,
		createdAt:      time.Now(),
		run:            run,
	}

	s.chatQueuesMu.Lock()
	queue := s.chatQueues[sessionID]
	if queue == nil {
		queue = newSessionQueue()
		s.chatQueues[sessionID] = queue
	}
	// workerLive 在 chatQueuesMu 下判定，保证两个人同时发消息时只会有一个
	// worker 被拉起——否则同一个 *agent.Agent 会被并行使用。
	spawnWorker := !queue.workerLive
	if spawnWorker {
		queue.workerLive = true
	}
	s.chatQueuesMu.Unlock()

	if dupItem := queue.findDuplicate(content); dupItem != nil {
		return dupItem, true
	}
	queue.enqueue(item)

	if spawnWorker {
		safeGo(func() { s.runQueue(sessionID, queue) })
	}
	return item, false
}

// findDuplicate 返回队列中内容完全相同的待执行项（仅比对文本，命中即视为
// 同一消息的重发）。
func (q *sessionQueue) findDuplicate(content string) *queuedTurn {
	if content == "" {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, it := range q.items {
		if it.content == content {
			return it
		}
	}
	return nil
}

// forwardTurnEvents 把 sink 上的回合事件转发到这条 SSE 连接，直到回合结束
// （done/error）或客户端断开。回合结束后仅结束本次请求，不是"停止回合"。
func (s *Server) forwardTurnEvents(ctx context.Context, writeSSE func(string) bool, snk *turnSink) {
	for {
		select {
		case ev, ok := <-snk.evch:
			if !ok {
				return
			}
			if ev.err != nil {
				writeSSE("data: " + sseErrorPayload(ev.err) + "\n\n")
			}
			if ev.data != "" && !writeSSE(ev.data) {
				return
			}
			if ev.done {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// startSSEClientWatch 监视客户端是否断开。回合与连接已解耦：断开只意味着
// "没人看这条流了"，不取消回合；这里只是让读事件的循环尽快退出，避免
// handler goroutine 与 SSE 连接泄漏。
func startSSEClientWatch(rctx context.Context, cancel context.CancelFunc) {
	safeGo(func() {
		<-rctx.Done()
		cancel()
	})
}

// ============================================================================
// SSE payload helpers
// ============================================================================

func sseErrorPayload(err error) string {
	b, _ := json.Marshal(map[string]string{"error": err.Error()})
	return string(b)
}

func sseQueuedPayload(item *queuedTurn, queue *sessionQueue) string {
	b, _ := json.Marshal(map[string]interface{}{
		"type":       "queued",
		"id":         item.id,
		"content":    shortenQueuedContent(item.content),
		"position":   queue.positionOf(item.id),
		"created_at": item.createdAt.Unix(),
	})
	return "data: " + string(b) + "\n\n"
}

// positionOf 返回队列项的位置（1 起）；0 表示已经不在队列中（可能已开始执行）。
func (q *sessionQueue) positionOf(id string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, it := range q.items {
		if it.id == id {
			return i + 1
		}
	}
	return 0
}

// ============================================================================
// Attachment persistence helpers
// ============================================================================

// mimeFromFilename 按扩展名给出 MIME，未知一律 application/octet-stream，由
// 调用方决定是否再按内容嗅探修正。附件分类（文本 / 图片 / 二进制）依赖它。
func mimeFromFilename(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".txt", ".md", ".json", ".yaml", ".yml", ".csv", ".xml", ".html", ".htm", ".js", ".ts", ".go", ".py", ".java", ".c", ".cpp", ".h", ".rs", ".rb", ".php", ".sh", ".css", ".sql", ".log":
		return "text/plain"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".pdf":
		return "application/pdf"
	case ".doc":
		return "application/msword"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".xls", ".xlsx":
		return "application/vnd.ms-excel"
	case ".ppt", ".pptx":
		return "application/vnd.ms-powerpoint"
	case ".zip":
		return "application/zip"
	}
	return "application/octet-stream"
}

// imageMimeForRef 推断图片附件的 MIME：优先取 data URL 前缀（本轮请求里的图片
// 就是 data URL），落库后只剩引用路径时退回扩展名。两者都判不出时返回
// "image/*"——前端只要 startsWith("image/") 就能决定是否渲染缩略图。
func imageMimeForRef(dataURL, ref string) string {
	if strings.HasPrefix(dataURL, "data:") {
		rest := strings.TrimPrefix(dataURL, "data:")
		if i := strings.Index(rest, ";"); i > 0 {
			if m := strings.ToLower(rest[:i]); strings.HasPrefix(m, "image/") {
				return m
			}
		}
	}
	if m := mimeFromFilename(ref); strings.HasPrefix(m, "image/") {
		return m
	}
	return "image/*"
}

// persistedContentParts 把本轮内容部件转成可落库的形态：剥掉内联 base64（本轮
// 已消费完毕，几百 KB~几 MB 的 base64 存进会话库会让每次会话读取都变慢），
// 其余元信息保留。
//
// 图片刻意落成 file 部件而不是 image_url：MediaURL 没有名字字段，图片一旦只
// 剩一个 uuid 引用，回放时前端认不出它，气泡里就只剩 [文件] 占位。file 部件
// 能同时带上原始名、MIME 与引用路径，前端据此渲染缩略图 + 文件名。
// resolveName 在客户端没给名字时回查 uploads 元数据兜底（可为 nil）。
func persistedContentParts(parts []types.ContentPart, imageURLRefs, imageNames []string, resolveName func(string) string) []types.ContentPart {
	out := make([]types.ContentPart, 0, len(parts))
	imgIdx := 0
	for _, part := range parts {
		switch {
		case part.Type == "file" && part.File != nil:
			out = append(out, types.ContentPart{
				Type: "file",
				File: &types.FileInfo{
					Name: part.File.Name,
					URL:  part.File.URL,
					// MimeType kept — cheap and lets the API layer classify
					// the attachment after a session reload.
					MimeType: part.File.MimeType,
					Contents: "", // not persisted
				},
			})
		case part.Type == "image_url" && part.ImageURL != nil:
			ref, name := "", ""
			if imgIdx < len(imageURLRefs) {
				ref = imageURLRefs[imgIdx]
			}
			if imgIdx < len(imageNames) {
				name = imageNames[imgIdx]
			}
			imgIdx++
			if name == "" && resolveName != nil {
				name = resolveName(ref)
			}
			out = append(out, types.ContentPart{
				Type: "file",
				File: &types.FileInfo{
					Name:     name,
					MimeType: imageMimeForRef(part.ImageURL.URL, ref),
					URL:      ref,
				},
			})
		default:
			out = append(out, part)
		}
	}
	return out
}

// ============================================================================
// Stream lifecycle registry
//
// 回合与客户端连接解耦后（见 handleSessionStream），server 需要记录每个
// session 的队列状态，供 /running 探测（含排队深度）、/cancel 显式停止使用。
// 状态即 chatqueue.go 里的 sessionQueue：改造前这里是一个只有 cancel func
// 的全局 map，既不区分"在跑"与"排队"，也无法回答"还有几条在等"。
// ============================================================================

// handleSessionRunning GET /api/sessions/{id}/running — 前端在连接被手机
// 浏览器切后台杀掉后轮询此端点：running=true 表示回合仍在服务端执行，
// 继续等待；false 表示回合已结束，拉取 /messages 恢复完整回复。
// 同时返回队列信息，前端据此渲染"排队中"的消息并支持刷新后恢复。
func (s *Server) handleSessionRunning(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	snap := s.queueInfoFor(sessionID)
	queued := snap.items
	if queued == nil {
		queued = []queuedTurnInfo{}
	}
	jsonResponse(w, map[string]interface{}{
		"session_id":  sessionID,
		"running":     snap.running,
		"active_id":   snap.activeID,
		"queue_depth": len(queued),
		"queued":      queued,
	})
}

// handleSessionCancel POST /api/sessions/{id}/cancel — 用户点"停止"时由前端
// 调用。连接解耦后，前端 abort 本地 fetch 不再能取消服务端回合，必须显式取消。
// 语义是「停止这一切」：既取消正在执行的回合，也丢弃全部排队消息——前端在
// 点停止时会同时清掉本地的排队气泡，服务端若保留队列，刷新后那些消息会
// 重新冒出来执行，与用户的意图相反。
func (s *Server) handleSessionCancel(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	res := cancelSignal{}
	if q := s.lookupSessionQueue(sessionID); q != nil {
		res = q.cancelAll()
	}
	jsonResponse(w, map[string]interface{}{
		"session_id":  sessionID,
		"cancelled":   res.active,
		"dropped":     res.pending,
		"queue_depth": 0,
	})
}

func (s *Server) handleSessionMessages(w http.ResponseWriter, r *http.Request, sessionID string) {
	switch r.Method {
	case "GET":
		if s.sessionStore == nil {
			jsonResponse(w, map[string]interface{}{"session_id": sessionID, "messages": []map[string]interface{}{}})
			return
		}

		dbSession, err := s.sessionStore.LoadSession(context.Background(), sessionID)
		if err != nil {
			http.Error(w, "not found", 404)
			return
		}

		messages := convertDBMessagesToAPI(sessionID, dbSession.Messages)
		jsonResponse(w, map[string]interface{}{
			"session_id": sessionID,
			"messages":   messages,
		})
	case "POST":
		// POST /api/sessions/{id}/messages —— 非流式提交入口。
		//
		// 改造后语义变为"入队"：消息进入会话队列后立即返回，由队列 worker
		// 串行执行，结果通过 SSE 流（/stream）或会话消息接口异步获取。这样
		// 客户端在一个回合进行中提交第二条消息时不会被阻塞或丢弃。
		// 注意这不再是"同步等结果"的接口——需要实时输出的调用方应使用 /stream。
		if s.provider == nil {
			http.Error(w, "provider not configured", 400)
			return
		}

		const maxBodyBytes = 16 << 20
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		defer r.Body.Close()

		// 与 /stream 共用同一套解析：图片上限、Office 文本抽取、附件物化
		// 等处理必须完全一致，否则两条入口会出现行为分叉。
		parsed, errMsg := s.parseChatPayload(r, sessionID)
		if parsed == nil {
			http.Error(w, errMsg, 400)
			return
		}

		run := &turnRunCtx{fileOps: NewTurnFileOpTracker()}
		if s.sessionStore != nil {
			if sess, err := s.sessionStore.LoadSession(context.Background(), sessionID); err == nil {
				run.workDir = sess.WorkDir
				run.workDirUserSet = sess.WorkDirUserSet
			}
		}

		var materializeSummary string
		if len(parsed.pendingMaterialize) > 0 {
			materializeSummary = materializeUploads(parsed.pendingMaterialize, run.workDir)
			if materializeSummary == "" {
				lines := make([]string, 0, len(parsed.pendingMaterialize))
				for _, it := range parsed.pendingMaterialize {
					lines = append(lines, fmt.Sprintf("- %s → %s", it.Name, it.Src))
				}
				materializeSummary = "附件已保存在以下服务器路径（工作目录暂不可用，如需读取请告知用户）：\n" + strings.Join(lines, "\n")
			}
		}

		persistedParts := persistedContentParts(parsed.contentParts, parsed.imageURLRefs, parsed.imageNames, s.uploadDisplayName)
		item, dup := s.enqueueChatTurn(sessionID, parsed.content, parsed.contentParts, persistedParts, run, materializeSummary)
		if item == nil {
			http.Error(w, "message not accepted, please retry", http.StatusServiceUnavailable)
			return
		}

		jsonResponse(w, map[string]interface{}{
			"id":        item.id,
			"role":      "user",
			"content":   parsed.content,
			"queued":    !dup,
			"duplicate": dup,
			"timestamp": item.createdAt.Unix(),
		})
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *Server) handleSessionGoals(w http.ResponseWriter, r *http.Request, sessionID string) {
	if s.goalMgr == nil {
		jsonResponse(w, map[string]interface{}{"session_id": sessionID, "goals": []map[string]interface{}{}})
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}

	ctx := r.Context()
	goals, err := s.goalMgr.GetGoalsBySession(ctx, sessionID)
	if err != nil {
		jsonResponse(w, map[string]interface{}{"session_id": sessionID, "goals": []map[string]interface{}{}})
		return
	}

	// Convert to simple format for display
	result := []map[string]interface{}{}
	for _, g := range goals {
		result = append(result, map[string]interface{}{
			"id":       g.ID,
			"title":    g.Title,
			"status":   g.Status,
			"progress": g.Progress,
		})
	}

	jsonResponse(w, map[string]interface{}{
		"session_id": sessionID,
		"goals":      result,
	})
}

func (s *Server) handleSessionSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", 405)
		return
	}

	query := r.URL.Query().Get("q")
	results := []map[string]interface{}{}

	if s.sessionStore == nil {
		jsonResponse(w, map[string]interface{}{"results": results})
		return
	}

	dbSessions, err := s.sessionStore.ListSessions(context.Background(), "")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	for _, sess := range dbSessions {
		// Search in messages content
		for _, m := range sess.Messages {
			if strings.Contains(strings.ToLower(m.Content), strings.ToLower(query)) {
				results = append(results, map[string]interface{}{
					"session_id":      sess.ID,
					"snippet":         utils.Truncate(m.Content, 200),
					"role":            m.Role,
					"source":          sess.Platform,
					"model":           s.cfg.GetCurrentModel(),
					"session_started": sess.CreatedAt.Unix(),
				})
				break // One match per session
			}
		}
	}

	jsonResponse(w, map[string]interface{}{"results": results})
}
