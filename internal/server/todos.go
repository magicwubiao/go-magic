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

// todoUpdateRequest 用指针承载"请求体里到底出现了哪些字段"：
// nil = 该字段没出现（保留原值）；非 nil = 显式提供（空串 = 显式清空）。
// 不能用 string 零值代替 —— 那样无法区分"没传"与"传了空串"。
type todoUpdateRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Priority    *string `json:"priority"`
	Status      *string `json:"status"`
	SessionID   string  `json:"session_id"`
}

// buildTodoUpdateArgs 把请求体映射成 todo 工具的 args。
//
// 工具侧是"**键在 args 里就按值生效**"语义（空串 = 显式清空），所以这里
// 绝不能无条件写入 title/description/priority：那会让任何局部更新都撞上
// "title cannot be empty for update"（改状态 / 优先级 / 描述全部 500），
// 并把未提供的 description / priority 静默清空（数据丢失）。
func buildTodoUpdateArgs(id, sessionID string, req todoUpdateRequest) map[string]interface{} {
	args := map[string]interface{}{
		"action":     "update",
		"id":         id,
		"session_id": sessionID,
	}
	if req.Title != nil {
		args["title"] = *req.Title
	}
	if req.Description != nil {
		args["description"] = *req.Description
	}
	if req.Priority != nil {
		args["priority"] = *req.Priority
	}
	// 空 status 无意义（工具会忽略），不写 key 以免与"显式清空"混淆。
	if req.Status != nil && *req.Status != "" {
		args["status"] = *req.Status
	}
	return args
}

func (s *Server) updateTodo(w http.ResponseWriter, r *http.Request, id string) {
	var req todoUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	sessionID := pickSessionIDFromRequest(r, req.SessionID)

	todoTool := tool.GetTodoTool()
	args := buildTodoUpdateArgs(id, sessionID, req)

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
