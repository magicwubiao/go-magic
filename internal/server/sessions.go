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
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/internal/session"
	"github.com/magicwubiao/go-magic/internal/tool"
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

		if req.DeleteFiles && dbSession.WorkDir != "" && !dbSession.WorkDirUserSet {
			s.cleanupSessionWorkDir(dbSession.WorkDir)
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

func (s *Server) handleSessionStream(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", 405)
		return
	}

	if s.provider == nil {
		http.Error(w, "LLM provider not configured. Please add a provider in Models page.", 400)
		return
	}

	content := r.URL.Query().Get("content")

	// Parse images from query parameter (JSON array of base64 data URLs)
	var contentParts []types.ContentPart
	imagesJSON := r.URL.Query().Get("images")
	if imagesJSON != "" {
		var imageURLs []string
		if err := json.Unmarshal([]byte(imagesJSON), &imageURLs); err == nil {
			for _, imgURL := range imageURLs {
				if imgURL != "" {
					contentParts = append(contentParts, types.ContentPart{
						Type:     "image_url",
						ImageURL: &types.MediaURL{URL: imgURL},
					})
				}
			}
		}
	}

	// Parse files from query parameter (JSON array of {name, filename, url, data})
	filesJSON := r.URL.Query().Get("files")
	if filesJSON != "" {
		var files []struct {
			Name     string `json:"name"`
			Filename string `json:"filename"`
			URL      string `json:"url"`
			Data     string `json:"data"` // base64 data URL from frontend
		}
		if err := json.Unmarshal([]byte(filesJSON), &files); err == nil {
			for _, f := range files {
				var dataURL string

				// Priority 1: Use base64 data directly from frontend (most reliable)
				if f.Data != "" {
					dataURL = f.Data
				} else {
					// Priority 2: Fetch file content from URL
					var data []byte
					var err error

					if f.URL != "" {
						// Extract path part (remove query parameters if any)
						fileURLPath := f.URL
						if idx := strings.IndexAny(fileURLPath, "?"); idx != -1 {
							fileURLPath = fileURLPath[:idx]
						}

						if strings.HasPrefix(fileURLPath, "/api/uploads/") {
							// Local file - read from filesystem
							uploadsDir := filepath.Join(s.magicHome, "uploads")
							filePath := filepath.Join(uploadsDir, f.Filename)
							data, err = os.ReadFile(filePath)
						} else {
							// External URL - fetch content
							req, _ := http.NewRequest("GET", f.URL, nil)
							if token := r.URL.Query().Get("token"); token != "" {
								req.Header.Set("Authorization", "Bearer "+token)
							}
							resp, fetchErr := http.DefaultClient.Do(req)
							if fetchErr == nil {
								defer resp.Body.Close()
								data, err = io.ReadAll(resp.Body)
							} else {
								err = fetchErr
							}
						}
					} else if f.Filename != "" {
						// Fallback: try to read from filesystem using filename
						uploadsDir := filepath.Join(s.magicHome, "uploads")
						filePath := filepath.Join(uploadsDir, f.Filename)
						data, err = os.ReadFile(filePath)
					}

					if err == nil && data != nil {
						// Determine MIME type from extension
						mimeType := "application/octet-stream"
						ext := strings.ToLower(filepath.Ext(f.Name))
						switch ext {
						case ".txt", ".md", ".json", ".yaml", ".yml", ".csv", ".xml", ".html", ".htm", ".js", ".ts", ".go", ".py", ".java", ".c", ".cpp", ".h", ".rs", ".rb", ".php", ".sh", ".css", ".sql", ".log":
							mimeType = "text/plain"
						case ".png":
							mimeType = "image/png"
						case ".jpg", ".jpeg":
							mimeType = "image/jpeg"
						case ".gif":
							mimeType = "image/gif"
						case ".webp":
							mimeType = "image/webp"
						case ".svg":
							mimeType = "image/svg+xml"
						case ".pdf":
							mimeType = "application/pdf"
						case ".doc":
							mimeType = "application/msword"
						case ".docx":
							mimeType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
						case ".xls", ".xlsx":
							mimeType = "application/vnd.ms-excel"
						case ".ppt", ".pptx":
							mimeType = "application/vnd.ms-powerpoint"
						case ".zip":
							mimeType = "application/zip"
						}

						base64Data := base64.StdEncoding.EncodeToString(data)
						dataURL = fmt.Sprintf("data:%s;base64,%s", mimeType, base64Data)
					}
				}

				if dataURL != "" {
					contentParts = append(contentParts, types.ContentPart{
						Type: "file",
						File: &types.FileInfo{
							Name:     f.Name,
							URL:      f.URL,
							Contents: dataURL,
						},
					})
				}
			}
		}
	}

	// Validate that we have at least content or media to send
	if content == "" && len(contentParts) == 0 {
		http.Error(w, "content or media required", 400)
		return
	}

	aiAgent := s.getOrCreateAgent(sessionID)
	if aiAgent == nil {
		http.Error(w, "LLM provider not configured. Please set up a provider in Settings.", http.StatusServiceUnavailable)
		return
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

	// Flush headers immediately so the client/proxy knows the stream is alive.
	writeSSE("data: {\"type\":\"connected\"}\n\n")

	// 注册本 session 的审批 SSE 推送回调：当 ApprovalHook 创建 pending 时，
	// 立即向当前 SSE 流推送 approval_required 事件，前端在对话流内渲染审批卡片。
	defer s.registerApprovalSSEHandler(sessionID, writeSSE)()

	// Use r.Context() directly so we detect client disconnects immediately.
	// Use a generous timeout for long-running tasks.
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()
	defer sseW.Close()

	// Inject the session's working directory into the context so file and
	// command tools resolve relative paths against it, then save the user
	// message to the session.
	if s.sessionStore != nil {
		if sess, err := s.sessionStore.LoadSession(context.Background(), sessionID); err == nil {
			ctx = tool.WithWorkDir(ctx, sess.WorkDir)
			ctx = tool.WithWorkDirUserSet(ctx, sess.WorkDirUserSet)

			sess.Messages = append(sess.Messages, types.Message{
				Role:         "user",
				Content:      content,
				ContentParts: contentParts,
				Timestamp:    time.Now(),
			})
			sess.UpdatedAt = time.Now()
			s.sessionStore.SaveSession(context.Background(), sess)
		}
	}

	// Start heartbeat goroutine to keep connection alive during long tool executions
	heartbeatDone := make(chan struct{})
	safeGo(func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if !writeSSE("data: {\"type\":\"ping\"}\n\n") {
					cancel()
					return
				}
			case <-heartbeatDone:
				return
			case <-ctx.Done():
				return
			}
		}
	})

	// Check if provider supports streaming
	_, supportsStream := s.provider.(provider.StreamingToolCaller)
	if !supportsStream {
		// Fallback: non-streaming response sent as single chunk
		var resp string
		var err error
		if len(contentParts) > 0 {
			resp, err = aiAgent.RunConversationWithMedia(ctx, content, contentParts)
		} else {
			resp, err = aiAgent.RunConversation(ctx, content)
		}
		close(heartbeatDone)
		if err != nil {
			data, _ := json.Marshal(map[string]string{"error": err.Error()})
			writeSSE("data: " + string(data) + "\n\n")
			return
		}

		// Send as delta chunks
		words := strings.Split(resp, "")
		clientGone := false
		for _, word := range words {
			select {
			case <-ctx.Done():
				// 客户端断开时回复已完整生成，仍要落库，避免整轮丢失
				clientGone = true
			default:
			}
			if clientGone {
				break
			}
			data, _ := json.Marshal(map[string]string{"delta": word})
			if !writeSSE("data: " + string(data) + "\n\n") {
				clientGone = true
				break
			}
		}

		// Save assistant message（即使发送中途断开也保存完整回复）
		if s.sessionStore != nil && strings.TrimSpace(resp) != "" {
			if sess, err := s.sessionStore.LoadSession(context.Background(), sessionID); err == nil {
				sess.Messages = append(sess.Messages, types.Message{
					Role:      "assistant",
					Content:   resp,
					Timestamp: time.Now(),
				})
				inputTokens, outputTokens, cacheTokens := aiAgent.GetTokenStats()
				sess.InputTokens += inputTokens
				sess.OutputTokens += outputTokens
				sess.CacheReadTokens += cacheTokens
				sess.UpdatedAt = time.Now()
				s.sessionStore.SaveSession(context.Background(), sess)
			}
		}

		// Record usage statistics
		s.recordUsage(aiAgent, sessionID)

		doneData, _ := json.Marshal(map[string]bool{"done": true})
		writeSSE("data: " + string(doneData) + "\n\n")
		return
	}

	// Real streaming
	var fullResponse strings.Builder
	var streamErr error
	streamHandler := func(chunk string, done bool) {
		if done {
			return
		}
		if chunk == "" {
			return
		}

		select {
		case <-ctx.Done():
			return
		default:
		}

		// Parse tool markers and emit structured events for web frontend
		// >>>TOOL_START|toolName|args<<<
		if strings.Contains(chunk, ">>>TOOL_START|") {
			re := regexp.MustCompile(`>>>TOOL_START\|([^|]+)\|(.*)<<<`)
			m := re.FindStringSubmatch(chunk)
			if m != nil {
				toolName := m[1]
				toolArgs := m[2]
				if len(toolArgs) > 200 {
					toolArgs = toolArgs[:200] + "..."
				}
				eventData, _ := json.Marshal(map[string]interface{}{
					"type": "tool_start",
					"name": toolName,
					"args": toolArgs,
				})
				writeSSE("data: " + string(eventData) + "\n\n")
			}
			return
		}

		// >>>TOOL_RESULT_START|toolName|success|duration<<<content>>>TOOL_RESULT_END<<<
		if strings.Contains(chunk, ">>>TOOL_RESULT_START|") {
			re := regexp.MustCompile(`>>>TOOL_RESULT_START\|([^|]+)\|([^|]+)\|([^<]+)<<<`)
			endRe := regexp.MustCompile(`>>>TOOL_RESULT_END<<<`)
			startMatch := re.FindStringSubmatchIndex(chunk)
			endMatch := endRe.FindStringIndex(chunk)
			if startMatch != nil && endMatch != nil {
				submatch := re.FindStringSubmatch(chunk[startMatch[0]:startMatch[1]])
				if len(submatch) >= 4 {
					toolName := submatch[1]
					toolSuccess := submatch[2] == "true"
					toolDuration := submatch[3]
					toolContent := chunk[startMatch[1]:endMatch[0]]
					// Truncate tool content for display
					if len(toolContent) > 500 {
						toolContent = toolContent[:500] + "..."
					}
					eventData, _ := json.Marshal(map[string]interface{}{
						"type":     "tool_result",
						"name":     toolName,
						"success":  toolSuccess,
						"duration": toolDuration,
						"content":  strings.TrimSpace(toolContent),
					})
					writeSSE("data: " + string(eventData) + "\n\n")
				}
			}
			return
		}

		// Skip other internal markers
		if strings.Contains(chunk, ">>>TURN_START<<<") {
			return
		}

		fullResponse.WriteString(chunk)
		data, _ := json.Marshal(map[string]string{"delta": chunk})
		writeSSE("data: " + string(data) + "\n\n")
	}
	if len(contentParts) > 0 {
		streamErr = aiAgent.RunConversationStreamWithMedia(ctx, content, contentParts, streamHandler)
	} else {
		streamErr = aiAgent.RunConversationStream(ctx, content, streamHandler)
	}

	close(heartbeatDone)

	if streamErr != nil {
		data, _ := json.Marshal(map[string]string{"error": streamErr.Error()})
		writeSSE("data: " + string(data) + "\n\n")
	}

	// Save assistant message。
	// 空内容保护：中断（用户点停止、超时等）且没有任何已生成内容时，
	// 不追加空 assistant 消息 —— 否则下次打开会话会出现空白回答气泡；
	// 用户消息已在流开始前保存，此处跳过即可保持历史干净。
	if assistantText := strings.TrimSpace(fullResponse.String()); s.sessionStore != nil && assistantText != "" {
		if sess, err := s.sessionStore.LoadSession(context.Background(), sessionID); err == nil {
			sess.Messages = append(sess.Messages, types.Message{
				Role:      "assistant",
				Content:   fullResponse.String(),
				Timestamp: time.Now(),
			})
			inputTokens, outputTokens, cacheTokens := aiAgent.GetTokenStats()
			sess.InputTokens += inputTokens
			sess.OutputTokens += outputTokens
			sess.CacheReadTokens += cacheTokens
			sess.UpdatedAt = time.Now()
			s.sessionStore.SaveSession(context.Background(), sess)
		}
	}

	// Record usage statistics
	s.recordUsage(aiAgent, sessionID)

	doneData, _ := json.Marshal(map[string]bool{"done": true})
	writeSSE("data: " + string(doneData) + "\n\n")
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
		if s.provider == nil {
			http.Error(w, "provider not configured", 400)
			return
		}

		var req struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Content == "" {
			http.Error(w, "invalid request: content required", 400)
			return
		}

		aiAgent := s.getOrCreateAgent(sessionID)
		if aiAgent == nil {
			http.Error(w, "LLM provider not configured. Please set up a provider in Settings.", http.StatusServiceUnavailable)
			return
		}

		ctx := context.Background()
		respContent, err := aiAgent.RunConversation(ctx, req.Content)
		if err != nil {
			http.Error(w, fmt.Sprintf("agent error: %v", err), 500)
			return
		}

		// Save to session store
		if s.sessionStore != nil {
			if sess, err := s.sessionStore.LoadSession(context.Background(), sessionID); err == nil {
				sess.Messages = append(sess.Messages, types.Message{
					Role:      "user",
					Content:   req.Content,
					Timestamp: time.Now(),
				})
				sess.Messages = append(sess.Messages, types.Message{
					Role:      "assistant",
					Content:   respContent,
					Timestamp: time.Now(),
				})
				inputTokens, outputTokens, cacheTokens := aiAgent.GetTokenStats()
				sess.InputTokens += inputTokens
				sess.OutputTokens += outputTokens
				sess.CacheReadTokens += cacheTokens
				sess.UpdatedAt = time.Now()
				s.sessionStore.SaveSession(context.Background(), sess)
			}
		}

		jsonResponse(w, map[string]interface{}{
			"id":        fmt.Sprintf("msg_%d", time.Now().UnixNano()),
			"role":      "assistant",
			"content":   respContent,
			"timestamp": time.Now().Unix(),
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
