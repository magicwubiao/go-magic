package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/magicwubiao/go-magic/internal/tool"
)

func (s *Server) handleTodos(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.listTodos(w, r)
		return
	}
	if r.Method == http.MethodPost {
		s.createTodo(w, r)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleTodoByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/todos/")
	if id == "" {
		http.Error(w, "todo id is required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.getTodo(w, r, id)
	case http.MethodPut, http.MethodPatch:
		s.updateTodo(w, r, id)
	case http.MethodDelete:
		s.deleteTodo(w, r, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func pickSessionIDFromRequest(r *http.Request, fallback string) string {
	if r == nil {
		return fallback
	}
	if v := r.URL.Query().Get("session_id"); v != "" {
		return v
	}
	if v := r.URL.Query().Get("filter_session"); v != "" {
		return v
	}
	return fallback
}

func (s *Server) listTodos(w http.ResponseWriter, r *http.Request) {
	todoTool := tool.GetTodoTool()
	filterStatus := r.URL.Query().Get("filter_status")
	filterPriority := r.URL.Query().Get("filter_priority")
	filterSession := pickSessionIDFromRequest(r, "")
	sortMode := r.URL.Query().Get("sort")

	// 注意：始终把 session_id 写进 args（空串也写）。
	// TodoTool.listTodos 会以 "key 是否存在于 args" 为界区分两种语义：
	//   存在 session_id/filter_session → 严格按值过滤（空串=只看全局未归属）
	//   完全不带 session 相关 key → 返回全部（仅 LLM 层"未感知会话"场景兜底）
	args := map[string]interface{}{
		"action":     "list",
		"session_id": filterSession,
	}
	if filterStatus != "" {
		args["filter_status"] = filterStatus
	}
	if filterPriority != "" {
		args["filter_priority"] = filterPriority
	}
	if sortMode != "" {
		args["sort"] = sortMode
	}

	result, err := todoTool.Execute(r.Context(), args)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, result)
}

func (s *Server) getTodo(w http.ResponseWriter, r *http.Request, id string) {
	todoTool := tool.GetTodoTool()
	filterSession := pickSessionIDFromRequest(r, "")
	// 始终传 session_id（空串=只看全局 bucket），与 listTodos 一致。
	args := map[string]interface{}{
		"action":        "list",
		"filter_status": "",
		"session_id":    filterSession,
	}
	result, err := todoTool.Execute(r.Context(), args)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp, ok := result.(map[string]interface{})
	if !ok {
		http.Error(w, "invalid response", http.StatusInternalServerError)
		return
	}

	todos, _ := resp["todos"].([]map[string]interface{})
	for _, t := range todos {
		if tid, _ := t["id"].(string); tid == id {
			jsonResponse(w, t)
			return
		}
	}
	http.Error(w, "todo not found", http.StatusNotFound)
}

func (s *Server) createTodo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Priority    string `json:"priority"`
		SessionID   string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}

	sessionID := pickSessionIDFromRequest(r, req.SessionID)

	todoTool := tool.GetTodoTool()
	args := map[string]interface{}{
		"action":     "create",
		"title":      req.Title,
		"session_id": sessionID,
	}
	if req.Description != "" {
		args["description"] = req.Description
	}
	if req.Priority != "" {
		args["priority"] = req.Priority
	}

	result, err := todoTool.Execute(r.Context(), args)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, result)
}

func (s *Server) updateTodo(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Priority    string `json:"priority"`
		Status      string `json:"status"`
		SessionID   string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	sessionID := pickSessionIDFromRequest(r, req.SessionID)

	todoTool := tool.GetTodoTool()
	args := map[string]interface{}{
		"action":     "update",
		"id":         id,
		"session_id": sessionID,
	}
	// 这里必须显式带 key（哪怕为空串），TodoTool 用 "key present in args" 语义
	args["title"] = req.Title
	args["description"] = req.Description
	args["priority"] = req.Priority
	if req.Status != "" {
		args["status"] = req.Status
	}

	result, err := todoTool.Execute(r.Context(), args)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, result)
}

func (s *Server) deleteTodo(w http.ResponseWriter, r *http.Request, id string) {
	sessionID := pickSessionIDFromRequest(r, "")

	todoTool := tool.GetTodoTool()
	args := map[string]interface{}{
		"action":     "delete",
		"id":         id,
		"session_id": sessionID,
	}
	result, err := todoTool.Execute(r.Context(), args)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, result)
}
