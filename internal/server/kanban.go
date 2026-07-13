package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/kanban"
)

func (s *Server) handleKanbanSplit(w http.ResponseWriter, r *http.Request, taskID string) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	if s.kanbanMgr == nil || s.provider == nil {
		http.Error(w, "kanban or provider not initialized", 500)
		return
	}
	subtasks, err := s.kanbanMgr.SplitTask(r.Context(), taskID, s.provider)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	result := make([]interface{}, 0, len(subtasks))
	for _, st := range subtasks {
		result = append(result, map[string]interface{}{
			"id":              st.ID,
			"title":           st.Title,
			"description":     st.Body,
			"status":          st.Status,
			"priority":        priorityToString(st.Priority),
			"estimated_hours": st.EstimatedHours,
		})
	}
	jsonResponse(w, result)
}

func (s *Server) handleKanbanTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		var req struct {
			Title          string  `json:"title"`
			Description    string  `json:"description"`
			Priority       string  `json:"priority"`
			DueDate        string  `json:"due_date"`
			EstimatedHours float64 `json:"estimated_hours"`
			GoalID         string  `json:"goal_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}

		if s.kanbanMgr == nil {
			http.Error(w, "kanban not initialized", 500)
			return
		}

		priority := priorityFromString(req.Priority)

		task, err := s.kanbanMgr.CreateTask(req.Title, req.Description, "", kanban.WithPriority(priority))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		// Apply optional fields
		updates := make(map[string]interface{})
		if req.DueDate != "" {
			updates["due_date"] = req.DueDate
		}
		if req.EstimatedHours > 0 {
			updates["estimated_hours"] = req.EstimatedHours
		}
		if req.GoalID != "" {
			updates["goal_id"] = req.GoalID
		}
		if len(updates) > 0 {
			task, _ = s.kanbanMgr.UpdateTask(task.ID, updates)
		}

		jsonResponse(w, map[string]interface{}{
			"id":              task.ID,
			"title":           task.Title,
			"description":     task.Body,
			"status":          task.Status,
			"priority":        priorityToString(task.Priority),
			"tags":            task.Skills,
			"created_at":      task.CreatedAt.Unix(),
			"updated_at":      task.UpdatedAt.Unix(),
			"due_date":        task.DueDate,
			"estimated_hours": task.EstimatedHours,
			"goal_id":         task.GoalID,
		})
		return
	}

	if r.Method == "GET" {
		if s.kanbanMgr == nil {
			jsonResponse(w, []interface{}{})
			return
		}
		board, _ := s.kanbanMgr.GetBoard("")
		tasks := make([]interface{}, 0)
		for _, taskList := range board {
			for _, task := range taskList {
				tasks = append(tasks, s.taskToJSON(task))
			}
		}
		jsonResponse(w, tasks)
		return
	}

	http.Error(w, "method not allowed", 405)
}

func (s *Server) handleKanbanBlock(w http.ResponseWriter, r *http.Request, taskID string) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	task, err := s.kanbanMgr.BlockTask(taskID, req.Reason)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	jsonResponse(w, s.taskToJSON(task))
}

func priorityToString(p int) string {
	switch p {
	case 3:
		return "critical"
	case 2:
		return "high"
	case 1:
		return "medium"
	default:
		return "low"
	}
}

func (s *Server) handleKanbanChildren(w http.ResponseWriter, r *http.Request, taskID string) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", 405)
		return
	}
	children, err := s.kanbanMgr.GetChildren(taskID)
	if err != nil {
		jsonResponse(w, []interface{}{})
		return
	}
	result := make([]map[string]interface{}, 0, len(children))
	for _, t := range children {
		result = append(result, map[string]interface{}{
			"id":              t.ID,
			"title":           t.Title,
			"status":          t.Status,
			"priority":        t.Priority,
			"due_date":        t.DueDate,
			"estimated_hours": t.EstimatedHours,
		})
	}
	jsonResponse(w, result)
}

func priorityFromString(s string) int {
	switch s {
	case "critical":
		return 3
	case "high":
		return 2
	case "medium":
		return 1
	default:
		return 0
	}
}

func (s *Server) handleKanbanTaskByIDOrSubroute(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/kanban/tasks/")
	parts := strings.SplitN(path, "/", 2)

	// If there's a sub-route (e.g. <id>/comments, <id>/children, <id>/triage, <id>/block, <id>/move)
	if len(parts) == 2 && parts[1] != "" {
		id := parts[0]
		subroute := parts[1]
		switch subroute {
		case "move":
			s.handleKanbanTaskMove(w, r, id)
		case "comments":
			s.handleKanbanComments(w, r, id)
		case "children":
			s.handleKanbanChildren(w, r, id)
		case "triage":
			s.handleKanbanTriage(w, r, id)
		case "block":
			s.handleKanbanBlock(w, r, id)
		case "split":
			s.handleKanbanSplit(w, r, id)
		default:
			http.Error(w, "not found", 404)
		}
		return
	}

	// Otherwise it's a task ID only — delegate to the original handler
	s.handleKanbanTaskByID(w, r)
}

func (s *Server) handleKanbanComments(w http.ResponseWriter, r *http.Request, taskID string) {
	switch r.Method {
	case "GET":
		comments, err := s.kanbanMgr.ListComments(taskID)
		if err != nil {
			jsonResponse(w, []interface{}{})
			return
		}
		result := make([]map[string]interface{}, 0, len(comments))
		for _, c := range comments {
			result = append(result, map[string]interface{}{
				"id":         c.ID,
				"task_id":    c.TaskID,
				"author":     c.Author,
				"body":       c.Body,
				"created_at": c.CreatedAt.Unix(),
			})
		}
		jsonResponse(w, result)
	case "POST":
		var req struct {
			Author string `json:"author"`
			Body   string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		comment, err := s.kanbanMgr.AddComment(taskID, req.Author, req.Body)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		jsonResponse(w, map[string]interface{}{
			"id":         comment.ID,
			"task_id":    comment.TaskID,
			"author":     comment.Author,
			"body":       comment.Body,
			"created_at": comment.CreatedAt.Unix(),
		})
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *Server) handleKanbanBoard(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", 405)
		return
	}

	if s.kanbanMgr == nil {
		http.Error(w, "kanban manager not initialized", 503)
		return
	}

	board, err := s.kanbanMgr.GetBoard("")
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get board: %v", err), 500)
		return
	}

	tasks := make([]interface{}, 0)
	for _, taskList := range board {
		for _, task := range taskList {
			tasks = append(tasks, s.taskToJSON(task))
		}
	}

	columns := []map[string]interface{}{
		{"key": "triage", "title": "Triage", "count": 0},
		{"key": "todo", "title": "To Do", "count": 0},
		{"key": "ready", "title": "Ready", "count": 0},
		{"key": "running", "title": "Running", "count": 0},
		{"key": "blocked", "title": "Blocked", "count": 0},
		{"key": "done", "title": "Done", "count": 0},
	}

	// Count tasks per status
	for status, taskList := range board {
		for _, col := range columns {
			if string(status) == col["key"] {
				col["count"] = len(taskList)
				break
			}
		}
	}

	jsonResponse(w, map[string]interface{}{"tasks": tasks, "columns": columns})
}

func (s *Server) handleKanbanTaskMove(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}

	if s.kanbanMgr == nil {
		http.Error(w, "kanban not initialized", 500)
		return
	}

	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", 400)
		return
	}

	updates := map[string]interface{}{
		"status": req.Status,
	}

	task, err := s.kanbanMgr.UpdateTask(id, updates)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	jsonResponse(w, s.taskToJSON(task))
}

func (s *Server) handleKanbanTriage(w http.ResponseWriter, r *http.Request, taskID string) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	if s.provider == nil {
		http.Error(w, "provider not available", 500)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	task, err := s.kanbanMgr.TriageTask(ctx, taskID, s.provider)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	jsonResponse(w, map[string]interface{}{
		"id":              task.ID,
		"title":           task.Title,
		"description":     task.Body,
		"status":          task.Status,
		"priority":        priorityToString(task.Priority),
		"due_date":        task.DueDate,
		"estimated_hours": task.EstimatedHours,
	})
}

func (s *Server) handleKanbanTaskByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/kanban/tasks/")
	// Handle /move suffix
	if strings.HasSuffix(id, "/move") {
		id = strings.TrimSuffix(id, "/move")
		s.handleKanbanTaskMove(w, r, id)
		return
	}

	if id == "" {
		http.Error(w, "not found", 404)
		return
	}

	if s.kanbanMgr == nil {
		http.Error(w, "kanban not initialized", 500)
		return
	}

	task, err := s.kanbanMgr.GetTask(id)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}

	switch r.Method {
	case "GET":
		jsonResponse(w, s.taskToJSON(task))
	case "PUT":
		var req struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			Priority    string `json:"priority"`
			Status      string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}

		updates := make(map[string]interface{})
		if req.Title != "" {
			updates["title"] = req.Title
		}
		if req.Description != "" {
			updates["body"] = req.Description
		}
		if req.Priority != "" {
			priority := 1
			if req.Priority == "high" {
				priority = 2
			} else if req.Priority == "low" {
				priority = 0
			}
			updates["priority"] = priority
		}
		if req.Status != "" {
			updates["status"] = req.Status
		}

		updatedTask, err := s.kanbanMgr.UpdateTask(id, updates)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		jsonResponse(w, s.taskToJSON(updatedTask))
	case "DELETE":
		if err := s.kanbanMgr.DeleteTask(id); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		jsonResponse(w, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", 405)
	}
}
