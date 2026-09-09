package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/magicwubiao/go-magic/internal/tool"
)

// clarifyTimeout 是 Web chat 澄清卡片的等待窗口。用户在此期限内未答复则
// Ask 返回错误，clarify 工具报错给模型（模型可自行决定继续或收尾），
// 与 registry 里 clarify 的工具超时（300s=MaxTimeout）保持一致。
const clarifyTimeout = 300 * time.Second

// PendingClarification 是一次等待用户答复的澄清请求（chan 唤醒，同审批模型）。
type PendingClarification struct {
	ID          string
	SessionID   string
	Request     tool.ClarifyRequest
	ans         chan *tool.ClarifyAnswer
	dismiss     chan struct{} // 用户点 ✕ 关闭卡片：closed 一次性取消等待
	dismissOnce sync.Once
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// Ask implements tool.ClarifyBridge：向指定会话发起澄清并阻塞等待用户答复。
//
//   - 该会话无活跃 SSE 澄清通道（bot/群聊/CLI 回合流未注册）→ 返回
//     tool.ErrClarifyUnavailable，clarify 工具回落为普通结构化结果；
//   - 有通道 → 推 clarify_required 卡片事件后挂起，直到用户答复 / ctx 取消 /
//     等待超时。返回的答复会作为 clarify 工具结果回流模型继续原回合。
func (s *Server) Ask(ctx context.Context, sessionID string, req tool.ClarifyRequest) (*tool.ClarifyAnswer, error) {
	s.clarifySSEHandlersMu.Lock()
	push, ok := s.clarifySSEHandlers[sessionID]
	s.clarifySSEHandlersMu.Unlock()
	if !ok || push == nil {
		return nil, tool.ErrClarifyUnavailable
	}

	now := time.Now()
	pc := &PendingClarification{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		Request:   req,
		ans:       make(chan *tool.ClarifyAnswer, 1),
		dismiss:   make(chan struct{}),
		CreatedAt: now,
		ExpiresAt: now.Add(clarifyTimeout),
	}

	s.clarificationsMu.Lock()
	s.clarifications[pc.ID] = pc
	s.clarificationsMu.Unlock()
	defer func() {
		s.clarificationsMu.Lock()
		delete(s.clarifications, pc.ID)
		s.clarificationsMu.Unlock()
	}()

	// 推送澄清卡片事件到该会话的 SSE 流。
	payload := map[string]interface{}{
		"type":         "clarify_required",
		"id":           pc.ID,
		"session_id":   pc.SessionID,
		"question":     req.Question,
		"options":      req.Options,
		"context":      req.Context,
		"multi_select": req.MultiSelect,
		"header":       req.Header,
		"created_at":   pc.CreatedAt.Unix(),
		"expires_at":   pc.ExpiresAt.Unix(),
	}
	if !push(payload) {
		// SSE 流在推送瞬间已死（writeSSE 返回 false）。回合上下文独立于连接，
		// 澄清无人可答，直接取消避免工具挂到超时。
		return nil, fmt.Errorf("clarification channel closed")
	}

	timer := time.NewTimer(time.Until(pc.ExpiresAt))
	defer timer.Stop()

	select {
	case ans := <-pc.ans:
		return ans, nil
	case <-pc.dismiss:
		return nil, fmt.Errorf("clarification dismissed by user")
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, fmt.Errorf("clarification timed out after %s", clarifyTimeout)
	}
}

// resolveClarification 唤醒等待中的澄清请求（幂等，非阻塞）。
func (s *Server) resolveClarification(id string, ans *tool.ClarifyAnswer) error {
	s.clarificationsMu.Lock()
	pc, ok := s.clarifications[id]
	s.clarificationsMu.Unlock()
	if !ok {
		return fmt.Errorf("clarification %s not found", id)
	}
	select {
	case pc.ans <- ans:
	default:
		// 已 resolve（防重复提交）
	}
	return nil
}

// dismissClarification 取消等待中的澄清请求（用户点卡片 ✕）：关闭 dismiss
// 通道唤醒 Ask 并返回"用户已关闭"错误，clarify 工具把该错误报给模型。
// 幂等：重复 dismiss / 已答复后再 dismiss 均安全（Once + 不存在的 id 报错）。
func (s *Server) dismissClarification(id string) error {
	s.clarificationsMu.Lock()
	pc, ok := s.clarifications[id]
	s.clarificationsMu.Unlock()
	if !ok {
		return fmt.Errorf("clarification %s not found", id)
	}
	pc.dismissOnce.Do(func() { close(pc.dismiss) })
	return nil
}

// pendingClarificationInfo 是 GET /api/clarify/pending 的对外结构。
type pendingClarificationInfo struct {
	ID          string   `json:"id"`
	SessionID   string   `json:"session_id"`
	Question    string   `json:"question"`
	Options     []string `json:"options"`
	Context     string   `json:"context"`
	MultiSelect bool     `json:"multi_select"`
	Header      string   `json:"header"`
	CreatedAt   string   `json:"created_at"`
	ExpiresAt   string   `json:"expires_at"`
}

// handleClarifyPending lists pending clarifications for a session
// (frontend restorePendingClarifications fallback).
func (s *Server) handleClarifyPending(w http.ResponseWriter, r *http.Request) {
	sessionFilter := r.URL.Query().Get("session_id")
	s.clarificationsMu.Lock()
	items := make([]pendingClarificationInfo, 0, len(s.clarifications))
	for _, pc := range s.clarifications {
		if sessionFilter != "" && pc.SessionID != sessionFilter {
			continue
		}
		items = append(items, pendingClarificationInfo{
			ID:          pc.ID,
			SessionID:   pc.SessionID,
			Question:    pc.Request.Question,
			Options:     pc.Request.Options,
			Context:     pc.Request.Context,
			MultiSelect: pc.Request.MultiSelect,
			Header:      pc.Request.Header,
			CreatedAt:   pc.CreatedAt.Format(time.RFC3339),
			ExpiresAt:   pc.ExpiresAt.Format(time.RFC3339),
		})
	}
	s.clarificationsMu.Unlock()
	jsonResponse(w, map[string]interface{}{"pending": items, "total": len(items)})
}

// handleClarifyByID answers or dismisses a pending clarification:
//   - POST /api/clarify/{id}/answer  body { "choice": "…", "choices": ["…"], "note": "…" }
//     choice 为单选时的选项文本（或 choices 数组承载多选），note 为用户追加说明。二选一皆可。
//   - POST /api/clarify/{id}/dismiss 无 body：用户关闭卡片，取消等待。
func (s *Server) handleClarifyByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/clarify/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 || r.Method != http.MethodPost {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	id, action := parts[0], parts[1]

	switch action {
	case "answer":
		var req struct {
			Choice  string   `json:"choice"`
			Choices []string `json:"choices"`
			Note    string   `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		choices := req.Choices
		if len(choices) == 0 && strings.TrimSpace(req.Choice) != "" {
			choices = []string{req.Choice}
		}
		note := strings.TrimSpace(req.Note)
		if len(choices) == 0 && note == "" {
			http.Error(w, "choice or note required", http.StatusBadRequest)
			return
		}

		if err := s.resolveClarification(id, &tool.ClarifyAnswer{Choices: choices, Note: note}); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		jsonResponse(w, map[string]bool{"success": true})
	case "dismiss":
		if err := s.dismissClarification(id); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		jsonResponse(w, map[string]bool{"success": true})
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// registerClarifySSEHandler registers an SSE push callback for the given session
// so clarify_required card events are delivered into the chat stream. Returns an
// unregister func (call via defer). Both handleChatStream and handleSessionStream
// use this so clarify cards appear in the Web chat.
func (s *Server) registerClarifySSEHandler(sessionID string, writeSSE func(string) bool) func() {
	s.clarifySSEHandlersMu.Lock()
	s.clarifySSEHandlers[sessionID] = func(payload map[string]interface{}) bool {
		data, err := json.Marshal(payload)
		if err != nil {
			return false
		}
		return writeSSE("data: " + string(data) + "\n\n")
	}
	s.clarifySSEHandlersMu.Unlock()
	return func() {
		s.clarifySSEHandlersMu.Lock()
		delete(s.clarifySSEHandlers, sessionID)
		s.clarifySSEHandlersMu.Unlock()
	}
}

// ensureClarifyBridge 把本 server 注入为 clarify 工具的 Web 通道桥。
// 幂等：重复调用只重新赋值（同一接收者）。
func (s *Server) ensureClarifyBridge() {
	tool.SetClarifyBridge(s)
}
