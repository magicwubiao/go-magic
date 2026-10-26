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
	"github.com/magicwubiao/go-magic/internal/provider"
)

func cronJobToResponse(job *cron.Job, locale string) map[string]interface{} {
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
		"working_dir":      job.WorkingDir,
		"schedule":         job.Schedule,
		"schedule_display": describeSchedule(job.Schedule, locale),
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

	// 子资源路由：自然语言解析、未来执行时间预览（易用性接口，
	// 必须先于"剩余路径当 jobID"处理）
	if path == "parse" {
		s.handleCronParse(w, r)
		return
	}
	if path == "next-runs" {
		s.handleCronNextRuns(w, r)
		return
	}

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
		jsonResponse(w, cronJobToResponse(job, localeFromQuery(r)))
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
		jsonResponse(w, cronJobToResponse(job, localeFromQuery(r)))
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
		jsonResponse(w, cronJobToResponse(job, localeFromQuery(r)))
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
			// Pointer so an explicit empty string clears the directory.
			WorkingDir *string `json:"working_dir,omitempty"`
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
		if req.WorkingDir != nil {
			job.WorkingDir = strings.TrimSpace(*req.WorkingDir)
		}
		if err := s.cronMgr.Update(job); err != nil {
			http.Error(w, fmt.Sprintf("failed to update job: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, cronJobToResponse(job, localeFromQuery(r)))
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
			result = append(result, cronJobToResponse(job, localeFromQuery(r)))
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
			WorkingDir  string   `json:"working_dir"`
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
			WorkingDir:  strings.TrimSpace(req.WorkingDir),
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

		jsonResponse(w, cronJobToResponse(job, localeFromQuery(r)))
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// localeFromQuery 从请求里取界面语言（?locale=zh / en），缺省 en。
// schedule_display 属于展示文案，跟随界面语言而不是硬编码英文。
func localeFromQuery(r *http.Request) string {
	l := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("locale")))
	if strings.HasPrefix(l, "zh") {
		return "zh"
	}
	return "en"
}

func describeSchedule(schedule string, locale string) string {
	if locale == "zh" {
		return describeScheduleZH(schedule)
	}
	return describeScheduleEN(schedule)
}

func describeScheduleEN(schedule string) string {
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

// describeScheduleZH 输出中文版调度描述。能识别的常见模式给自然说法，
// 其余给出结构化描述，保证中文界面下不再混排英文。
func describeScheduleZH(schedule string) string {
	parts := strings.Fields(schedule)
	if len(parts) != 5 {
		return schedule
	}

	min, hour, dom, mon, dow := parts[0], parts[1], parts[2], parts[3], parts[4]

	// 整点/固定时刻的常见模式
	if dom == "*" && mon == "*" {
		switch {
		case min == "*" && hour == "*" && dow == "*":
			return "每分钟"
		case strings.HasPrefix(min, "*/") && hour == "*" && dow == "*":
			return fmt.Sprintf("每 %s 分钟", min[2:])
		case min == "0" && hour == "*" && dow == "*":
			return "每小时"
		case strings.HasPrefix(hour, "*/") && min == "0" && dow == "*":
			return fmt.Sprintf("每 %s 小时", hour[2:])
		case dow == "1-5" && min == "0":
			return fmt.Sprintf("工作日每天 %s:00", hour)
		case dow == "*" && min == "0":
			return fmt.Sprintf("每天 %s:00", hour)
		case dow == "*" && dom == "*":
			return fmt.Sprintf("每天 %s:%s", hour, min)
		case dow == "1-5":
			return fmt.Sprintf("工作日 %s:%s", hour, min)
		}
	}
	if min == "0" && hour == "8" && dom == "*" && mon == "*" && dow == "*" {
		return "每天 08:00"
	}
	if dom == "1" && mon == "*" && dow == "*" {
		return fmt.Sprintf("每月 1 号 %s:%s", hour, min)
	}

	// 星期字段展开（支持 0-6 / 1,3,5 / 1-5）
	if dom == "*" && mon == "*" && dow != "*" {
		if names := dowNamesZH(dow); names != "" {
			return fmt.Sprintf("每%s %s:%s", names, hour, min)
		}
	}

	return fmt.Sprintf("cron: 分=%s 时=%s 日=%s 月=%s 周=%s", min, hour, dom, mon, dow)
}

// dowNamesZH 把 day-of-week 字段转成"一到五"这类中文短语；无法解析返回 ""。
func dowNamesZH(dow string) string {
	if dow == "*" {
		return "天"
	}
	mapping := map[string]string{
		"0": "日", "7": "日", "1": "一", "2": "二", "3": "三",
		"4": "四", "5": "五", "6": "六",
	}
	var out []string
	for _, part := range strings.Split(dow, ",") {
		if rng := strings.SplitN(part, "-", 2); len(rng) == 2 {
			a, ok1 := mapping[rng[0]]
			b, ok2 := mapping[rng[1]]
			if ok1 && ok2 {
				out = append(out, a+"到"+b)
				continue
			}
			return ""
		}
		name, ok := mapping[part]
		if !ok {
			return ""
		}
		out = append(out, name)
	}
	return strings.Join(out, "、")
}

// handleCronNextRuns 返回一个 cron 表达式未来 N 次执行时间，用于创建/编辑
// 表单里的实时预览："这个表达式到底什么时候跑"。同时承担真正的表达式校验
// （此前前端只数字段个数，`99 99 * * *` 也被当成合法）。
func (s *Server) handleCronNextRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	schedule := strings.TrimSpace(r.URL.Query().Get("schedule"))
	if schedule == "" {
		http.Error(w, "schedule query parameter is required", http.StatusBadRequest)
		return
	}

	count := 5
	if c := r.URL.Query().Get("count"); c != "" {
		if n, err := strconv.Atoi(c); err == nil && n > 0 && n <= 20 {
			count = n
		}
	}

	times, err := cron.GetNextRuns(schedule, count)
	if err != nil {
		jsonResponse(w, map[string]interface{}{
			"valid": false,
			"error": err.Error(),
		})
		return
	}

	nextRuns := make([]string, 0, len(times))
	for _, t := range times {
		nextRuns = append(nextRuns, t.Format("2006-01-02 15:04:05"))
	}
	jsonResponse(w, map[string]interface{}{
		"valid":     true,
		"next_runs": nextRuns,
		"display":   describeSchedule(schedule, localeFromQuery(r)),
	})
}

// handleCronParse 用 LLM 把自然语言（"每天早上8点提醒我喝水"）解析成
// 定时任务三要素：cron 表达式、任务名、执行 prompt。解析结果只是回填
// 表单，仍会走 next-runs 校验与用户确认后才会保存。
func (s *Server) handleCronParse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.provider == nil {
		http.Error(w, "no LLM provider configured", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		http.Error(w, "text is required", http.StatusBadRequest)
		return
	}

	systemPrompt := `You convert a user's natural-language request into a scheduled job spec.
Reply with ONLY a JSON object (no markdown, no explanations) with these keys:
- "schedule": 5-field cron expression (minute hour day-of-month month day-of-week), local time. Examples: "0 8 * * *" daily at 08:00, "30 21 * * 1-5" weekdays at 21:30, "*/10 * * * *" every 10 minutes. Use * for day-of-month/month unless the user names a specific date.
- "name": short job name in the user's language (max ~20 chars).
- "prompt": the instruction the agent should execute on each run, in the user's language, self-contained and repeatable.
If the request is not schedulable as a recurring task, reply {"error": "reason"}.`

	messages := []provider.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: req.Text},
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	resp, err := s.provider.Chat(ctx, messages)
	if err != nil {
		http.Error(w, fmt.Sprintf("LLM parse failed: %v", err), http.StatusBadGateway)
		return
	}

	content := strings.TrimSpace(resp.Content)
	// 容错：剥掉可能出现的 markdown 代码块包裹
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	// 有些模型会在 JSON 前后补说明，取第一个 { 到最后一个 } 之间
	if i := strings.Index(content, "{"); i >= 0 {
		if j := strings.LastIndex(content, "}"); j > i {
			content = content[i : j+1]
		}
	}

	var parsed struct {
		Schedule string `json:"schedule"`
		Name     string `json:"name"`
		Prompt   string `json:"prompt"`
		Error    string `json:"error"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		http.Error(w, "failed to parse LLM response as JSON", http.StatusBadGateway)
		return
	}
	if parsed.Error != "" {
		http.Error(w, parsed.Error, http.StatusBadRequest)
		return
	}
	if parsed.Schedule == "" {
		http.Error(w, "LLM returned no schedule", http.StatusBadGateway)
		return
	}
	if err := cron.ValidateSchedule(parsed.Schedule); err != nil {
		http.Error(w, fmt.Sprintf("LLM produced invalid cron expression %q: %v", parsed.Schedule, err), http.StatusBadGateway)
		return
	}

	result := map[string]interface{}{
		"schedule":         parsed.Schedule,
		"name":             parsed.Name,
		"prompt":           parsed.Prompt,
		"schedule_display": describeSchedule(parsed.Schedule, localeFromQuery(r)),
	}
	if nextRuns, err := cron.GetNextRuns(parsed.Schedule, 3); err == nil {
		times := make([]string, 0, len(nextRuns))
		for _, t := range nextRuns {
			times = append(times, t.Format("2006-01-02 15:04:05"))
		}
		result["next_runs"] = times
	}
	jsonResponse(w, result)
}
