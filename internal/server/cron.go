package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/magicwubiao/go-magic/internal/cron"
)

func cronJobToResponse(job *cron.Job) map[string]interface{} {
	state := "inactive"
	if job.Enabled {
		state = "active"
	}
	if job.LastStatus == "running" {
		state = "running"
	}

	resp := map[string]interface{}{
		"id":               job.ID,
		"name":             job.Name,
		"description":      job.Description,
		"prompt":           job.Prompt,
		"script":           job.Script,
		"schedule":         job.Schedule,
		"schedule_display": describeSchedule(job.Schedule),
		"enabled":          job.Enabled,
		"no_agent":         job.NoAgent,
		"state":            state,
		"last_status":      job.LastStatus,
		"last_error":       job.LastError,
		"run_count":        job.RunCount,
	}

	if job.LastRun != nil {
		resp["last_run_at"] = job.LastRun.Format(time.RFC3339)
	}
	if job.NextRun != nil {
		resp["next_run_at"] = job.NextRun.Format(time.RFC3339)
	}

	return resp
}

func (s *Server) handleCronJobByID(w http.ResponseWriter, r *http.Request) {
	if s.cronMgr == nil {
		http.Error(w, "cron manager not available", http.StatusServiceUnavailable)
		return
	}

	// Support both /api/cron/{id} and /api/cron/jobs/{id}
	path := r.URL.Path
	path = strings.TrimPrefix(path, "/api/cron/jobs/")
	path = strings.TrimPrefix(path, "/api/cron/")
	jobID := strings.TrimSuffix(path, "/pause")
	jobID = strings.TrimSuffix(jobID, "/resume")
	jobID = strings.TrimSuffix(jobID, "/trigger")
	jobID = strings.TrimSuffix(jobID, "/run")
	jobID = strings.TrimSuffix(jobID, "/logs")

	if strings.HasSuffix(path, "/logs") {
		limit := 50
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 {
				limit = n
			}
		}
		logs := s.cronMgr.GetLogs(jobID, limit)
		jsonResponse(w, logs)
		return
	}

	if strings.HasSuffix(path, "/pause") {
		job := s.findCronJobByID(jobID)
		if job == nil {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}
		job.Enabled = false
		if err := s.cronMgr.Update(job); err != nil {
			http.Error(w, fmt.Sprintf("failed to pause job: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, cronJobToResponse(job))
		return
	}
	if strings.HasSuffix(path, "/resume") {
		job := s.findCronJobByID(jobID)
		if job == nil {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}
		job.Enabled = true
		// Recalculate next run
		if nextRun, err := cron.GetNextRun(job.Schedule); err == nil {
			job.NextRun = nextRun
		}
		if err := s.cronMgr.Update(job); err != nil {
			http.Error(w, fmt.Sprintf("failed to resume job: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, cronJobToResponse(job))
		return
	}
	if strings.HasSuffix(path, "/trigger") || strings.HasSuffix(path, "/run") {
		job := s.findCronJobByID(jobID)
		if job == nil {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}
		go safeGo(func() {
			_ = s.cronMgr.RunJob(context.Background(), job)
		})
		jsonResponse(w, map[string]bool{"ok": true})
		return
	}
	if r.Method == http.MethodGet {
		job := s.findCronJobByID(jobID)
		if job == nil {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}
		jsonResponse(w, cronJobToResponse(job))
		return
	}
	if r.Method == http.MethodPut {
		job := s.findCronJobByID(jobID)
		if job == nil {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}
		var req struct {
			Name        string   `json:"name"`
			Description string   `json:"description"`
			Prompt      string   `json:"prompt"`
			Schedule    string   `json:"schedule"`
			Script      string   `json:"script"`
			NoAgent     *bool    `json:"no_agent,omitempty"`
			Enabled     *bool    `json:"enabled,omitempty"`
			Skills      []string `json:"skills"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.Name != "" {
			job.Name = req.Name
		}
		if req.Description != "" {
			job.Description = req.Description
		}
		if req.Prompt != "" {
			job.Prompt = req.Prompt
		}
		if req.Schedule != "" {
			// Validate new schedule
			if err := cron.ValidateSchedule(req.Schedule); err != nil {
				http.Error(w, fmt.Sprintf("invalid cron expression: %v", err), http.StatusBadRequest)
				return
			}
			job.Schedule = req.Schedule
			if nextRun, err := cron.GetNextRun(req.Schedule); err == nil {
				job.NextRun = nextRun
			}
		}
		if req.Script != "" {
			job.Script = req.Script
		}
		if req.NoAgent != nil {
			job.NoAgent = *req.NoAgent
		}
		if req.Enabled != nil {
			job.Enabled = *req.Enabled
		}
		if req.Skills != nil {
			job.Skills = req.Skills
		}
		if err := s.cronMgr.Update(job); err != nil {
			http.Error(w, fmt.Sprintf("failed to update job: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, cronJobToResponse(job))
		return
	}
	if r.Method == http.MethodDelete {
		if err := s.cronMgr.Remove(jobID); err != nil {
			http.Error(w, fmt.Sprintf("failed to delete job: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]bool{"ok": true})
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleCronJobs(w http.ResponseWriter, r *http.Request) {
	if s.cronMgr == nil {
		http.Error(w, "cron manager not available", http.StatusServiceUnavailable)
		return
	}

	if r.Method == http.MethodGet {
		jobs := s.cronMgr.List()
		result := make([]interface{}, 0, len(jobs))
		for _, job := range jobs {
			result = append(result, cronJobToResponse(job))
		}
		jsonResponse(w, result)
		return
	}
	if r.Method == http.MethodPost {
		var req struct {
			Name        string   `json:"name"`
			Description string   `json:"description"`
			Prompt      string   `json:"prompt"`
			Schedule    string   `json:"schedule"`
			Script      string   `json:"script"`
			NoAgent     bool     `json:"no_agent"`
			Skills      []string `json:"skills"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if req.Name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}
		if req.Schedule == "" {
			http.Error(w, "schedule is required", http.StatusBadRequest)
			return
		}

		// Validate cron expression
		if err := cron.ValidateSchedule(req.Schedule); err != nil {
			http.Error(w, fmt.Sprintf("invalid cron expression: %v", err), http.StatusBadRequest)
			return
		}

		job := &cron.Job{
			ID:          uuid.New().String(),
			Name:        req.Name,
			Description: req.Description,
			Prompt:      req.Prompt,
			Schedule:    req.Schedule,
			Script:      req.Script,
			NoAgent:     req.NoAgent,
			Skills:      req.Skills,
			Enabled:     true,
		}

		// Calculate next run time
		if nextRun, err := cron.GetNextRun(req.Schedule); err == nil {
			job.NextRun = nextRun
		}

		if err := s.cronMgr.Add(job); err != nil {
			http.Error(w, fmt.Sprintf("failed to create job: %v", err), http.StatusInternalServerError)
			return
		}

		jsonResponse(w, cronJobToResponse(job))
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func describeSchedule(schedule string) string {
	parts := strings.Fields(schedule)
	if len(parts) != 5 {
		return schedule
	}

	min, hour, dom, mon, dow := parts[0], parts[1], parts[2], parts[3], parts[4]

	// Common patterns
	if min == "0" && hour == "8" && dom == "*" && mon == "*" && dow == "*" {
		return "Every day at 08:00"
	}
	if min == "0" && hour == "9" && dom == "*" && mon == "*" && dow == "1-5" {
		return "Every weekday at 09:00"
	}
	if min == "0" && hour == "0" && dom == "*" && mon == "*" && dow == "*" {
		return "Every day at midnight"
	}
	if min == "*" && hour == "*" && dom == "*" && mon == "*" && dow == "*" {
		return "Every minute"
	}
	if min == "0" && hour == "*" && dom == "*" && mon == "*" && dow == "*" {
		return "Every hour"
	}
	if min == "0" && hour == "*" && dom == "*" && mon == "*" && dow == "1-5" {
		return "Every hour on weekdays"
	}
	if min == "30" && hour == "*" && dom == "1" && mon == "*" && dow == "*" {
		return "Monthly on the 1st at 00:30"
	}

	// Generic description
	return fmt.Sprintf("At %s:%s on day-of-month %s, month %s, day-of-week %s", hour, min, dom, mon, dow)
}
