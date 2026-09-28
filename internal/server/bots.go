package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/magicwubiao/go-magic/internal/bot"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// validNamePattern matches safe bot names and room IDs: alphanumeric start,
// letters/digits/'-'/'_' up to 64 chars. It guards against path traversal
// via names like "../../x" that would escape the bots/ or rooms/ directory
// in the file-backed store.
var validNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

// maskedEnvValue is the placeholder returned in place of a bot's env values.
// It round-trips: the dashboard never sees the real secret, and a value left
// untouched comes back as this sentinel, which mergeMaskedEnv translates back
// to the stored secret instead of overwriting it.
const maskedEnvValue = "***"

// maskEnv hides env values while keeping the keys visible, so the dashboard can
// still show which variables a bot has without shipping every bot's API keys
// and tokens to any authenticated browser (and into browser caches, logs and
// screenshots). Bots' secrets live in <workdir>/bots/<name>/.env, which stays
// readable to the bot's own tools.
func maskEnv(env map[string]string) map[string]string {
	if len(env) == 0 {
		return nil
	}
	out := make(map[string]string, len(env))
	for k := range env {
		out[k] = maskedEnvValue
	}
	return out
}

// mergeMaskedEnv applies an incoming env map onto the stored one. Values equal
// to maskedEnvValue are "unchanged" placeholders, so the stored secret wins.
// An empty incoming map means the caller removed every variable.
func mergeMaskedEnv(stored, incoming map[string]string) map[string]string {
	if len(incoming) == 0 {
		return nil
	}
	merged := make(map[string]string, len(incoming))
	for k, v := range incoming {
		if v == maskedEnvValue {
			if old, ok := stored[k]; ok {
				merged[k] = old
				continue
			}
		}
		merged[k] = v
	}
	return merged
}

// botToResponse serializes a bot config for the dashboard API.
func botToResponse(cfg *bot.Config, state *bot.RuntimeState) map[string]interface{} {
	resp := map[string]interface{}{
		"name":          cfg.Name,
		"mention_tag":   cfg.MentionTag(),
		"title":         cfg.Title,
		"description":   cfg.Description,
		"system_prompt": cfg.SystemPrompt,
		"model":         cfg.Model,
		"provider":      cfg.Provider,
		"tools":         cfg.Tools,
		"skills":        cfg.Skills,
		"memory":        cfg.Memory,
		"avatar":        cfg.Avatar,
		"env":           maskEnv(cfg.Env),
		"hidden":        cfg.Hidden,
		"active":        cfg.IsActive(),
		"status":        cfg.Status,
		"created_at":    cfg.CreatedAt,
		"updated_at":    cfg.UpdatedAt,
	}
	if state != nil {
		resp["runtime"] = map[string]interface{}{
			"online":          true,
			"session_id":      state.SessionID,
			"queue_depth":     state.QueueDepth,
			"history_length":  state.HistoryLength,
			"active_routines": state.ActiveRoutines,
			"active":          state.Active,
			"status":          state.Status,
			"last_active":     state.LastActiveUnix,
		}
	} else {
		resp["runtime"] = map[string]interface{}{
			"online": false,
		}
	}
	return resp
}

// routineToResponse serializes a routine config.
func routineToResponse(r *bot.RoutineConfig) map[string]interface{} {
	resp := map[string]interface{}{
		"id":          r.ID,
		"name":        r.Name,
		"schedule":    r.Schedule,
		"prompt":      r.Prompt,
		"enabled":     r.Enabled,
		"last_status": r.LastStatus,
		"last_result": r.LastResult,
		"created_at":  r.CreatedAt,
	}
	if r.LastRun != nil {
		resp["last_run"] = *r.LastRun
	}
	return resp
}

// handleBots routes /api/bots (list, create) requests.
func (s *Server) handleBots(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleBotsList(w, r)
	case http.MethodPost:
		s.handleBotsCreate(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleBotByID routes /api/bots/{name}[/...] requests.
func (s *Server) handleBotByID(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/bots/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	name := ""
	if len(parts) > 0 {
		name = parts[0]
	}
	if name == "" {
		http.Error(w, "bot name is required", http.StatusBadRequest)
		return
	}
	if !validNamePattern.MatchString(name) {
		http.Error(w, "invalid bot name", http.StatusBadRequest)
		return
	}

	switch {
	case len(parts) == 1:
		switch r.Method {
		case http.MethodGet:
			s.handleBotGet(w, r, name)
		case http.MethodPut, http.MethodPatch:
			s.handleBotUpdate(w, r, name)
		case http.MethodDelete:
			s.handleBotDelete(w, r, name)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case len(parts) == 2 && parts[1] == "routines":
		s.handleBotRoutines(w, r, name)
	case len(parts) == 3 && parts[1] == "routines":
		s.handleBotRoutineByID(w, r, name, parts[2])
	case len(parts) == 4 && parts[1] == "routines" && parts[3] == "run":
		s.handleBotRoutineRun(w, r, name, parts[2])
	case len(parts) == 2 && parts[1] == "chat":
		s.handleBotChat(w, r, name)
	case len(parts) == 3 && parts[1] == "chat" && parts[2] == "stream":
		s.handleBotChatStream(w, r, name)
	case len(parts) == 2 && parts[1] == "running" && r.Method == http.MethodGet:
		s.handleBotRunning(w, r, name)
	case len(parts) == 2 && parts[1] == "cancel" && r.Method == http.MethodPost:
		s.handleBotCancel(w, r, name)
	case len(parts) == 2 && parts[1] == "guide" && r.Method == http.MethodPost:
		s.handleBotGuide(w, r, name)
	case len(parts) == 2 && parts[1] == "messages" && r.Method == http.MethodDelete:
		// Must be matched before the generic "messages" case below, otherwise
		// DELETE falls through to handleBotMessages which rejects non-GET.
		s.handleBotClearMessages(w, r, name)
	case len(parts) == 2 && parts[1] == "messages":
		s.handleBotMessages(w, r, name)
	case len(parts) == 2 && parts[1] == "clone" && r.Method == http.MethodPost:
		s.handleBotClone(w, r, name)
	case len(parts) == 2 && parts[1] == "activate" && r.Method == http.MethodPost:
		s.handleBotSetActive(w, r, name, true)
	case len(parts) == 2 && parts[1] == "deactivate" && r.Method == http.MethodPost:
		s.handleBotSetActive(w, r, name, false)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// requireBotManager resolves the shared bot manager or fails the request.
func (s *Server) requireBotManager(w http.ResponseWriter) *bot.Manager {
	mgr := s.botMgr()
	if mgr == nil {
		http.Error(w, "bot mode is not running", http.StatusServiceUnavailable)
		return nil
	}
	return mgr
}

// handleBotsList GET /api/bots
func (s *Server) handleBotsList(w http.ResponseWriter, r *http.Request) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}
	configs := mgr.List()
	result := make([]map[string]interface{}, 0, len(configs))
	for _, cfg := range configs {
		state := mgr.RuntimeStatus(cfg.Name)
		result = append(result, botToResponse(cfg, &state))
	}
	jsonResponse(w, result)
}

// handleBotsCreate POST /api/bots
func (s *Server) handleBotsCreate(w http.ResponseWriter, r *http.Request) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}

	var req struct {
		Name         string            `json:"name"`
		Title        string            `json:"title"`
		Description  string            `json:"description"`
		SystemPrompt string            `json:"system_prompt"`
		Model        string            `json:"model"`
		Provider     string            `json:"provider"`
		Tools        []string          `json:"tools"`
		Skills       []string          `json:"skills"`
		Memory       string            `json:"memory"`
		Avatar       string            `json:"avatar"`
		Env          map[string]string `json:"env"`
		Hidden       bool              `json:"hidden"`
		Start        *bool             `json:"start,omitempty"` // reserved; bots are online immediately when mode is on
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	cfg := &bot.Config{
		Name:         req.Name,
		Title:        req.Title,
		Description:  req.Description,
		SystemPrompt: req.SystemPrompt,
		Model:        req.Model,
		Provider:     req.Provider,
		Tools:        req.Tools,
		Skills:       req.Skills,
		Memory:       req.Memory,
		Avatar:       req.Avatar,
		Env:          req.Env,
		Hidden:       req.Hidden,
	}
	if err := mgr.CreateBot(cfg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	state := mgr.RuntimeStatus(cfg.Name)
	w.WriteHeader(http.StatusCreated)
	jsonResponse(w, botToResponse(cfg, &state))
}

// handleBotGet GET /api/bots/{name}
func (s *Server) handleBotGet(w http.ResponseWriter, r *http.Request, name string) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}
	cfg, err := mgr.GetBot(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	state := mgr.RuntimeStatus(name)
	jsonResponse(w, botToResponse(cfg, &state))
}

// handleBotUpdate PUT/PATCH /api/bots/{name}
func (s *Server) handleBotUpdate(w http.ResponseWriter, r *http.Request, name string) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}

	var req struct {
		Title        *string           `json:"title,omitempty"`
		Description  *string           `json:"description,omitempty"`
		SystemPrompt *string           `json:"system_prompt,omitempty"`
		Model        *string           `json:"model,omitempty"`
		Provider     *string           `json:"provider,omitempty"`
		Tools        []string          `json:"tools,omitempty"`
		Skills       []string          `json:"skills,omitempty"`
		Memory       *string           `json:"memory,omitempty"`
		Avatar       *string           `json:"avatar,omitempty"`
		Env          map[string]string `json:"env,omitempty"`
		Hidden       *bool             `json:"hidden,omitempty"`
		ClearTools   *bool             `json:"clear_tools,omitempty"`
		ClearSkills  *bool             `json:"clear_skills,omitempty"`
		ClearEnv     *bool             `json:"clear_env,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	cfg, err := mgr.UpdateBot(name, func(c *bot.Config) {
		if req.Title != nil {
			c.Title = *req.Title
		}
		if req.Description != nil {
			c.Description = *req.Description
		}
		if req.SystemPrompt != nil {
			c.SystemPrompt = *req.SystemPrompt
		}
		if req.Model != nil {
			c.Model = *req.Model
		}
		if req.Provider != nil {
			c.Provider = *req.Provider
		}
		if req.Tools != nil {
			c.Tools = req.Tools
		} else if req.ClearTools != nil && *req.ClearTools {
			c.Tools = nil
		}
		if req.Skills != nil {
			c.Skills = req.Skills
		} else if req.ClearSkills != nil && *req.ClearSkills {
			c.Skills = nil
		}
		if req.Memory != nil {
			c.Memory = *req.Memory
		}
		if req.Avatar != nil {
			c.Avatar = *req.Avatar
		}
		if req.Env != nil {
			// Values the caller did not touch come back as maskedEnvValue
			// (botToResponse never exposes the plaintext), so keep the stored
			// secret for those keys instead of persisting the mask itself.
			c.Env = mergeMaskedEnv(c.Env, req.Env)
		} else if req.ClearEnv != nil && *req.ClearEnv {
			c.Env = nil
		}
		if req.Hidden != nil {
			c.Hidden = *req.Hidden
		}
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	state := mgr.RuntimeStatus(name)
	jsonResponse(w, botToResponse(cfg, &state))
}

// handleBotDelete DELETE /api/bots/{name}
func (s *Server) handleBotDelete(w http.ResponseWriter, r *http.Request, name string) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}
	if err := mgr.DeleteBot(name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// The chat session is gone with the bot — its upload bucket is now an
	// orphan the GC will keep forever (it deliberately errs on the side of
	// keeping bot buckets). Remove it explicitly, mirroring session deletion
	// with delete_files=true.
	s.cleanupSessionUploads(botUploadBucket(name))
	jsonResponse(w, map[string]interface{}{"deleted": name})
}

// handleBotRoutines GET/POST /api/bots/{name}/routines
func (s *Server) handleBotRoutines(w http.ResponseWriter, r *http.Request, name string) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}

	switch r.Method {
	case http.MethodGet:
		routines, err := mgr.ListRoutines(name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		result := make([]map[string]interface{}, 0, len(routines))
		for _, rt := range routines {
			result = append(result, routineToResponse(rt))
		}
		jsonResponse(w, result)
	case http.MethodPost:
		var req struct {
			Name     string `json:"name"`
			Schedule string `json:"schedule"`
			Prompt   string `json:"prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		rt := &bot.RoutineConfig{
			Name:     req.Name,
			Schedule: req.Schedule,
			Prompt:   req.Prompt,
		}
		if rt.ID == "" || rt.ID == "auto" {
			rt.ID = fmt.Sprintf("web_%s", uuid.New().String()[:8])
		}
		if err := mgr.AddRoutine(name, rt); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
		jsonResponse(w, routineToResponse(rt))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleBotRoutineByID routes /api/bots/{name}/routines/{id}: DELETE removes,
// PATCH applies partial updates (schedule/prompt/enabled/name).
func (s *Server) handleBotRoutineByID(w http.ResponseWriter, r *http.Request, name, idOrName string) {
	switch r.Method {
	case http.MethodDelete:
		mgr := s.requireBotManager(w)
		if mgr == nil {
			return
		}
		if err := mgr.RemoveRoutine(name, idOrName); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		jsonResponse(w, map[string]interface{}{"deleted": idOrName})
	case http.MethodPatch, http.MethodPut:
		mgr := s.requireBotManager(w)
		if mgr == nil {
			return
		}

		var req struct {
			Name     *string `json:"name,omitempty"`
			Schedule *string `json:"schedule,omitempty"`
			Prompt   *string `json:"prompt,omitempty"`
			Enabled  *bool   `json:"enabled,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.Name == nil && req.Schedule == nil && req.Prompt == nil && req.Enabled == nil {
			http.Error(w, "no fields to update", http.StatusBadRequest)
			return
		}

		rt, err := mgr.UpdateRoutine(name, idOrName, func(r *bot.RoutineConfig) {
			if req.Name != nil {
				r.Name = *req.Name
			}
			if req.Schedule != nil {
				r.Schedule = strings.TrimSpace(*req.Schedule)
			}
			if req.Prompt != nil {
				r.Prompt = *req.Prompt
			}
			if req.Enabled != nil {
				r.Enabled = *req.Enabled
			}
		})
		if err != nil {
			status := http.StatusBadRequest
			if strings.Contains(err.Error(), "not found") {
				status = http.StatusNotFound
			}
			http.Error(w, err.Error(), status)
			return
		}
		jsonResponse(w, routineToResponse(rt))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// botUploadBucket 是 bot 聊天附件在上传目录里的桶名。bot 会话存在独立的
// bots.db 里，桶名直接取 canonical session id 的清洗形态（清洗规则与
// uploadsDirFor 一致），前端上传附件时也带同名 session_id，两侧必然对上。
func botUploadBucket(botName string) string {
	sid := bot.CanonicalSessionID(strings.ToLower(botName))
	return fileNameSafeRe.ReplaceAllString(sid, "_")
}

// parseBotChatPayload 解析 bot 聊天请求体（/chat 与 /chat/stream 共用），
// 复用 sessions 的 parseChatPayload（files/images/imageUrls/imageNames 与
// 会话聊天完全同一请求形状），并兼容旧的 {"message": "..."} 字段。
//
// 返回：
//   - text：用户输入的纯文本（content 与 message 两个字段的合并结果）；
//   - parts：模型可见的 content parts（文本作为首个 text 部件——带部件的
//     消息在出站转换时只发 parts，纯文本 input 会被丢弃）；
//   - persisted：落盘用的引用形态 parts（base64 已换成上传引用，不含
//     物化摘要——那是当回合的执行提示，不是用户说的话）；
//   - errMsg：非空表示应回给客户端的 4xx 文案。
func (s *Server) parseBotChatPayload(r *http.Request, botName string) (text string, parts, persisted []types.ContentPart, errMsg string) {
	const maxBotChatBodyBytes = 16 << 20
	raw, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, maxBotChatBodyBytes))
	if err != nil {
		return "", nil, nil, "failed to read request body: " + err.Error()
	}

	// Back-compat: the original bot chat contract was {"message": "..."}.
	// parseChatPayload only knows the sessions shape, so the legacy field is
	// decoded separately and merged below.
	var legacy struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &legacy)

	// parseChatPayload re-reads the body; hand the saved bytes back.
	r.Body = io.NopCloser(bytes.NewReader(raw))

	bucket := botUploadBucket(botName)
	parsed, perr := s.parseChatPayload(r, bucket)
	if parsed == nil {
		return "", nil, nil, perr
	}

	text = strings.TrimSpace(parsed.content)
	if text == "" {
		text = strings.TrimSpace(legacy.Message)
	}
	if text == "" && len(parsed.contentParts) == 0 {
		return "", nil, nil, "message is required"
	}

	// Materialize uploaded attachments into the bot's isolated workdir so its
	// sandboxed file tools can read them (canonical copies under
	// <magicHome>/uploads stay untouched — GC/audit source of truth). Done
	// before enqueueing: the uploads GC must not reclaim files a queued turn
	// still needs.
	var materializeSummary string
	if len(parsed.pendingMaterialize) > 0 {
		if mgr := s.botMgr(); mgr != nil {
			if workDir := mgr.BotWorkDir(botName); workDir != "" {
				materializeSummary = materializeUploads(parsed.pendingMaterialize, workDir)
			}
		}
		if materializeSummary == "" {
			// 工作目录不可用：至少把服务器规范路径告诉模型（与 sessions 同）。
			lines := make([]string, 0, len(parsed.pendingMaterialize))
			for _, it := range parsed.pendingMaterialize {
				lines = append(lines, fmt.Sprintf("- %s → %s", it.Name, it.Src))
			}
			materializeSummary = "附件已保存在以下服务器路径（工作目录暂不可用，如需读取请告知用户）：\n" + strings.Join(lines, "\n")
		}
	}

	if text != "" {
		parts = append(parts, types.ContentPart{Type: "text", Text: text})
	}
	parts = append(parts, parsed.contentParts...)
	if materializeSummary != "" {
		parts = append(parts, types.ContentPart{Type: "text", Text: materializeSummary})
	}

	persisted = persistedContentParts(parsed.contentParts, parsed.imageURLRefs, parsed.imageNames, s.uploadDisplayName)
	if text != "" {
		persisted = append([]types.ContentPart{{Type: "text", Text: text}}, persisted...)
	}
	return text, parts, persisted, ""
}

// handleBotChat POST /api/bots/{name}/chat — synchronous send-and-wait turn.
func (s *Server) handleBotChat(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}

	text, parts, persisted, errMsg := s.parseBotChatPayload(r, name)
	if errMsg != "" {
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	var reply string
	var err error
	if len(parts) > 0 {
		reply, err = mgr.SendToBotWithMedia(name, text, parts, persisted)
	} else {
		reply, err = mgr.SendToBot(name, text)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, map[string]interface{}{
		"id":        uuid.New().String(),
		"role":      "assistant",
		"content":   reply,
		"timestamp": time.Now().UnixMilli(),
	})
}

// handleBotMessages GET /api/bots/{name}/messages — canonical chat history.
func (s *Server) handleBotMessages(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), readTimeout10s())
	defer cancel()

	sess, err := mgr.Sessions().LoadSession(ctx, bot.CanonicalSessionID(strings.ToLower(name)))
	if err != nil || sess == nil {
		jsonResponse(w, []interface{}{})
		return
	}
	type chatMsg struct {
		id        string
		role      string
		from      string
		content   string
		parts     []types.ContentPart
		timestamp int64
	}

	var merged []chatMsg
	var pendingAssistants []chatMsg

	flushPending := func() {
		if len(pendingAssistants) == 0 {
			return
		}
		if len(pendingAssistants) == 1 {
			merged = append(merged, pendingAssistants[0])
		} else {
			var sb strings.Builder
			var lastTS int64
			for _, pa := range pendingAssistants {
				if sb.Len() > 0 {
					sb.WriteString("\n\n")
				}
				sb.WriteString(pa.content)
				if pa.timestamp > lastTS {
					lastTS = pa.timestamp
				}
			}
			merged = append(merged, chatMsg{
				id:        pendingAssistants[0].id,
				role:      "assistant",
				from:      pendingAssistants[0].from,
				content:   sb.String(),
				timestamp: lastTS,
			})
		}
		pendingAssistants = nil
	}

	for i, msg := range sess.Messages {
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}
		ts := int64(0)
		if !msg.Timestamp.IsZero() {
			ts = msg.Timestamp.UnixMilli()
		} else if len(sess.Messages) > 1 {
			ratio := float64(i) / float64(len(sess.Messages)-1)
			startMs := sess.CreatedAt.UnixMilli()
			endMs := sess.UpdatedAt.UnixMilli()
			if endMs <= startMs {
				endMs = startMs + int64(len(sess.Messages)-1)*1000
			}
			ts = startMs + int64(float64(endMs-startMs)*ratio)
		} else {
			ts = sess.UpdatedAt.UnixMilli()
		}
		entry := chatMsg{
			id:        fmt.Sprintf("%s-%d", sess.ID, i),
			role:      msg.Role,
			from:      msg.From,
			content:   msg.Content,
			parts:     msg.ContentParts,
			timestamp: ts,
		}
		if msg.Role == "user" {
			flushPending()
			merged = append(merged, entry)
		} else {
			pendingAssistants = append(pendingAssistants, entry)
		}
	}
	flushPending()

	result := make([]map[string]interface{}, 0, len(merged))
	for _, m := range merged {
		entry := map[string]interface{}{
			"id":        m.id,
			"role":      m.role,
			"from":      m.from,
			"content":   m.content,
			"timestamp": m.timestamp,
		}
		// Multimodal user messages: the persisted parts are the lightweight
		// ref form (file parts with /api/uploads URLs, no inline base64).
		// Expose name/url so the dashboard can render attachment thumbnails
		// after a reload, and synthesize the bubble text from text parts when
		// the agent history stored the message as parts-only (Content empty —
		// the outbound convention for multimodal messages).
		if len(m.parts) > 0 {
			attachments := make([]map[string]string, 0, len(m.parts))
			textBits := make([]string, 0, len(m.parts))
			for _, p := range m.parts {
				switch {
				case p.Type == "text" && strings.TrimSpace(p.Text) != "":
					textBits = append(textBits, p.Text)
				case p.Type == "file" && p.File != nil && p.File.URL != "":
					attachments = append(attachments, map[string]string{
						"name": p.File.Name,
						"url":  p.File.URL,
						"mime": p.File.MimeType,
					})
				case p.Type == "image_url" && p.ImageURL != nil &&
					p.ImageURL.URL != "" && !strings.HasPrefix(p.ImageURL.URL, "data:"):
					// Legacy inline-image persistence (pre-ref form): URL is a
					// plain link, no readable name.
					attachments = append(attachments, map[string]string{
						"name": "",
						"url":  p.ImageURL.URL,
						"mime": "",
					})
				}
			}
			if len(attachments) > 0 {
				entry["attachments"] = attachments
			}
			if strings.TrimSpace(m.content) == "" && len(textBits) > 0 {
				entry["content"] = strings.Join(textBits, "\n")
			}
		}
		result = append(result, entry)
	}
	jsonResponse(w, result)
}

// handleBotChatStream POST /api/bots/{name}/chat/stream — SSE variant of
// handleBotChat. The turn still runs serialized on the bot's queue; assistant
// deltas are pushed as {"delta": "..."} events, ending with {"done": true}.
func (s *Server) handleBotChatStream(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}

	text, parts, persisted, errMsg := s.parseBotChatPayload(r, name)
	if errMsg != "" {
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	writeSSE := func(payload string) {
		fmt.Fprint(w, "data: "+payload+"\n\n")
		flusher.Flush()
	}
	writeJSONEvent := func(v interface{}) {
		if data, err := json.Marshal(v); err == nil {
			writeSSE(string(data))
		}
	}

	// Headers out immediately so proxies/clients see a live stream.
	writeSSE(`{"type":"connected"}`)

	var reply string
	var err error
	// onToolEvent forwards structured tool_start / tool_result activity to the
	// SSE client as {"tool":{...}} events so the web UI can render live tool
	// activity while the bot works (same parser the sessions chat uses).
	onToolEvent := func(evt map[string]interface{}) {
		writeJSONEvent(map[string]interface{}{"tool": evt})
	}
	if len(parts) > 0 {
		reply, err = mgr.SendToBotStreamWithMedia(name, text, parts, persisted, func(content string, done bool) {
			if done || content == "" {
				return
			}
			writeJSONEvent(map[string]string{"delta": content})
		}, onToolEvent)
	} else {
		reply, err = mgr.SendToBotStreamEvents(name, text, func(content string, done bool) {
			if done || content == "" {
				return
			}
			writeJSONEvent(map[string]string{"delta": content})
		}, onToolEvent)
	}

	if err != nil {
		msg := err.Error()
		writeJSONEvent(map[string]string{"error": msg})
	} else if strings.TrimSpace(reply) != "" {
		// Ensure the client has the full final text even if some deltas were
		// coalesced by intermediate proxies.
		writeJSONEvent(map[string]interface{}{"final": reply})
	}
	writeSSE(`{"done":true}`)
}

// handleBotRunning GET /api/bots/{name}/running — probe whether the bot has
// a turn in flight or queued. Clients whose SSE stream died (mobile browsers
// kill idle streams when backgrounded) poll this until it reports false,
// then fetch the final history.
func (s *Server) handleBotRunning(w http.ResponseWriter, r *http.Request, name string) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}
	jsonResponse(w, map[string]interface{}{"running": mgr.IsBusy(name)})
}

// handleBotCancel POST /api/bots/{name}/cancel — explicitly cancel the bot's
// in-flight turn. With the turn decoupled from the SSE connection, dropping
// the stream no longer stops the server; clients call this to stop.
func (s *Server) handleBotCancel(w http.ResponseWriter, r *http.Request, name string) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	jsonResponse(w, map[string]interface{}{"canceled": mgr.CancelTurn(name)})
}

// handleBotGuide POST /api/bots/{name}/guide — inject a steering message into
// the bot's in-flight turn (agent guide mechanism). The model sees it before
// its next LLM call within the same turn; generation is not interrupted.
// When no turn is running the response is {"injected": false} and the client
// falls back to a regular send.
func (s *Server) handleBotGuide(w http.ResponseWriter, r *http.Request, name string) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		http.Error(w, "text is required", http.StatusBadRequest)
		return
	}
	injected, err := mgr.GuideBot(name, req.Text)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	jsonResponse(w, map[string]interface{}{"injected": injected})
}

// handleBotClearMessages DELETE /api/bots/{name}/messages — wipe the bot's
// canonical chat history (disk + live agent memory).
func (s *Server) handleBotClearMessages(w http.ResponseWriter, r *http.Request, name string) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}
	if err := mgr.ClearHistory(name); err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	jsonResponse(w, map[string]interface{}{"cleared": true})
}

// handleBotClone POST /api/bots/{name}/clone — duplicate a bot's full profile
// (persona, model pin, tools/skills allowlists, memory, avatar, env) under a
// new name with empty chat history and no routines.
func (s *Server) handleBotClone(w http.ResponseWriter, r *http.Request, name string) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		http.Error(w, "new bot name is required", http.StatusBadRequest)
		return
	}

	cfg, err := mgr.CloneBot(name, strings.TrimSpace(req.Name))
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	state := mgr.RuntimeStatus(cfg.Name)
	w.WriteHeader(http.StatusCreated)
	jsonResponse(w, botToResponse(cfg, &state))
}

// handleBotSetActive POST /api/bots/{name}/activate|deactivate — pause or
// resume a bot without deleting it.
func (s *Server) handleBotSetActive(w http.ResponseWriter, r *http.Request, name string, active bool) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}
	cfg, err := mgr.SetBotActive(name, active)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	state := mgr.RuntimeStatus(cfg.Name)
	jsonResponse(w, botToResponse(cfg, &state))
}

// handleBotRoutineRun POST /api/bots/{name}/routines/{id}/run — trigger an
// immediate one-off routine execution outside its cron schedule.
func (s *Server) handleBotRoutineRun(w http.ResponseWriter, r *http.Request, name, idOrName string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}
	if err := mgr.RunRoutineNow(name, idOrName); err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		} else if strings.Contains(err.Error(), "is disabled") || strings.Contains(err.Error(), "not running") {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	jsonResponse(w, map[string]interface{}{"triggered": idOrName})
}
