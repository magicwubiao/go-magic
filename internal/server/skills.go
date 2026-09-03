package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/magicwubiao/go-magic/internal/skills"
	"github.com/magicwubiao/go-magic/pkg/utils"
)

func (s *Server) handleSkills(w http.ResponseWriter, r *http.Request) {
	skills := s.getRealSkills()
	jsonResponse(w, skills)
}

func (s *Server) handleDashboardSkills(w http.ResponseWriter, r *http.Request) {
	skills := s.getRealSkills()
	jsonResponse(w, map[string]interface{}{
		"installed":  skills,
		"available":  []Skill{},
		"categories": []string{"development", "research", "analytics", "automation", "communication"},
	})
}

func (s *Server) handleSkillByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/skills/")

	// Handle toggle - frontend sends {name, enabled}
	if id == "toggle" && r.Method == "PUT" {
		var req struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
			http.Error(w, "invalid request", 400)
			return
		}

		s.disabledSkillsMu.Lock()
		if req.Enabled {
			delete(s.disabledSkills, req.Name)
		} else {
			s.disabledSkills[req.Name] = true
		}
		s.disabledSkillsMu.Unlock()

		// Persist disabled skills to config
		s.mu.Lock()
		if s.cfg != nil {
			disabledList := make([]string, 0)
			s.disabledSkillsMu.Lock()
			for name := range s.disabledSkills {
				disabledList = append(disabledList, name)
			}
			s.disabledSkillsMu.Unlock()
			s.cfg.Skills.Disabled = disabledList
			_ = s.cfg.Save()
		}
		s.mu.Unlock()

		jsonResponse(w, map[string]interface{}{"ok": true, "name": req.Name, "enabled": req.Enabled})
		return
	}

	// Handle browse
	if id == "browse" && r.Method == "GET" {
		jsonResponse(w, s.getRealSkills())
		return
	}

	// Handle versions - 获取技能版本历史
	if strings.HasSuffix(id, "/versions") && r.Method == "GET" {
		skillName := strings.TrimSuffix(id, "/versions")
		if s.skillMgr != nil {
			versions := s.skillMgr.GetVersions(skillName)
			jsonResponse(w, versions)
			return
		}
		jsonResponse(w, []map[string]interface{}{})
		return
	}

	// Handle evolution - 获取技能演化历史
	if strings.HasSuffix(id, "/evolution") && r.Method == "GET" {
		skillName := strings.TrimSuffix(id, "/evolution")
		if s.skillMgr != nil {
			records := s.skillMgr.GetEvolutionRecords(skillName)
			jsonResponse(w, records)
			return
		}
		jsonResponse(w, []map[string]interface{}{})
		return
	}

	// Handle install
	if id == "install" && r.Method == "POST" {
		var req struct {
			URL      string `json:"url"`
			Name     string `json:"name"`
			Content  string `json:"content"`
			Category string `json:"category"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		// If URL is provided, use skill manager to install from URL
		if req.URL != "" {
			if s.skillMgr != nil {
				if err := s.skillMgr.InstallFromURL(req.URL); err != nil {
					http.Error(w, "failed to install skill: "+err.Error(), http.StatusInternalServerError)
					return
				}
				// Reload skills to include the newly installed one
				s.skillMgr.Reload()
				jsonResponse(w, map[string]interface{}{"ok": true, "url": req.URL})
				return
			}
			http.Error(w, "skill manager not available", http.StatusInternalServerError)
			return
		}

		// Legacy: create from name/content
		if req.Name != "" {
			skillsDir := s.getUserSkillsDir()
			skillDir := filepath.Join(skillsDir, req.Name)
			os.MkdirAll(skillDir, 0755)
			if req.Content != "" {
				skillFile := filepath.Join(skillDir, "skill.yaml")
				os.WriteFile(skillFile, []byte(req.Content), 0644)
			}
			// Reload skills
			if s.skillMgr != nil {
				s.skillMgr.Reload()
			}
			jsonResponse(w, map[string]bool{"ok": true})
			return
		}

		http.Error(w, "invalid request: either url or name is required", http.StatusBadRequest)
		return
	}

	// Handle GET - return single skill
	if r.Method == http.MethodGet {
		for _, skill := range s.getRealSkills() {
			if skill.ID == id {
				jsonResponse(w, skill)
				return
			}
		}
		http.Error(w, "not found", 404)
		return
	}

	// Handle PUT - update skill
	if r.Method == http.MethodPut {
		var req struct {
			Name        string   `json:"name"`
			Description string   `json:"description"`
			Category    string   `json:"category"`
			Tags        []string `json:"tags"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}

		// Update through skill manager (updates memory and file)
		if s.skillMgr != nil {
			err := s.skillMgr.UpdateMetadata(id, skills.SkillMeta{
				Name:        req.Name,
				Description: req.Description,
				Tags:        req.Tags,
			})
			if err != nil {
				http.Error(w, "failed to update skill: "+err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			// Fallback: update skill.yaml directly
			// 安全拼接 skillDir：防止 id 含 "../" 穿越到 skillsDir 之外
			skillDir, err := SafeJoin(s.getUserSkillsDir(), id)
			if err != nil {
				http.Error(w, "invalid skill id: "+err.Error(), http.StatusBadRequest)
				return
			}
			skillFile := filepath.Join(skillDir, "skill.yaml")

			// 用 YAML 双引号字符串转义用户输入，防止换行注入额外字段。
			// YAML 双引号字符串中需转义反斜杠和双引号，换行符转为 \n 字面量。
			content := fmt.Sprintf("name: %q\ndescription: %q\ncategory: %q\ntags: %q\n",
				req.Name, req.Description, req.Category, strings.Join(req.Tags, ","))
			if err := os.WriteFile(skillFile, []byte(content), 0644); err != nil {
				http.Error(w, "failed to write skill file: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}

		jsonResponse(w, map[string]interface{}{"ok": true, "id": id})
		return
	}

	// Handle DELETE - delete skill
	if r.Method == http.MethodDelete {
		// First, remove from skill manager (updates memory)
		if s.skillMgr != nil {
			if err := s.skillMgr.Delete(id); err != nil {
				http.Error(w, "failed to delete skill: "+err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			// Fallback: remove from file system directly
			// 安全拼接：防止 id 含 "../" 删除 skillsDir 之外的目录
			skillDir, err := SafeJoin(s.getUserSkillsDir(), id)
			if err != nil {
				http.Error(w, "invalid skill id: "+err.Error(), http.StatusBadRequest)
				return
			}
			if err := os.RemoveAll(skillDir); err != nil {
				http.Error(w, "failed to delete skill directory: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
		jsonResponse(w, map[string]interface{}{"ok": true, "id": id, "deleted": true})
		return
	}

	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleAutoSkillStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.skillMgr == nil {
		jsonResponse(w, map[string]interface{}{"status": "pending", "message": "skill manager not initialized"})
		return
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		// Return status of all auto skills
		statuses := map[string]string{}
		for _, status := range []skills.SkillStatus{
			skills.SkillStatusPending, skills.SkillStatusApproved,
			skills.SkillStatusArchived, skills.SkillStatusRejected,
		} {
			for _, skill := range s.skillMgr.ListAutoSkillsByStatus(status) {
				statuses[skill.Name] = string(status)
			}
		}
		jsonResponse(w, statuses)
		return
	}

	status, err := s.skillMgr.GetSkillStatus(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	jsonResponse(w, map[string]interface{}{"name": name, "status": status})
}

func (s *Server) handleAutoSkillStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.skillMgr == nil {
		jsonResponse(w, map[string]int{"pending": 0, "approved": 0, "archived": 0, "rejected": 0})
		return
	}
	counts := s.skillMgr.GetSkillStatusCounts()
	// Convert map keyed by SkillStatus to string-keyed
	out := map[string]int{}
	for k, v := range counts {
		out[string(k)] = v
	}
	jsonResponse(w, out)
}

func (s *Server) handleSkillUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse multipart form with 32MB max memory
	err := r.ParseMultipartForm(32 << 20)
	if err != nil {
		http.Error(w, "failed to parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Get uploaded file
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "failed to get file: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Get skill name from form or use filename
	skillName := r.FormValue("name")
	if skillName == "" {
		skillName = strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename))
	}

	// Check for relative path (for directory uploads)
	relativePath := r.FormValue("path")

	// If relative path provided, extract folder name from it (use original folder name)
	if relativePath != "" && strings.Contains(relativePath, "/") {
		parts := strings.SplitN(relativePath, "/", 2)
		if parts[0] != "" {
			skillName = parts[0] // Use original folder name
		}
	}

	// 安全校验 skillName：只允许字母、数字、下划线、连字符，拒绝路径分隔符和 ".."
	// 防止 skillName 含 "../" 导致写到 skillsDir 之外
	if err := SanitizeName(skillName); err != nil {
		http.Error(w, "invalid skill name: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Get user skills directory
	skillsDir := s.getUserSkillsDir()
	// SafeJoin 二次防御路径穿越
	skillDir, err := SafeJoin(skillsDir, skillName)
	if err != nil {
		http.Error(w, "invalid skill directory: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Create skill directory
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		http.Error(w, "failed to create skill directory: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Determine file extension and save path
	ext := strings.ToLower(filepath.Ext(header.Filename))
	var destPath string

	switch ext {
	case ".zip":
		// 安全拼接 zip 文件名，防止 header.Filename 含路径
		zipBaseName := filepath.Base(header.Filename)
		zipPath, err := SafeJoin(skillDir, zipBaseName)
		if err != nil {
			http.Error(w, "invalid zip filename: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := saveUploadedFile(file, zipPath); err != nil {
			http.Error(w, "failed to save zip file: "+err.Error(), http.StatusInternalServerError)
			return
		}
		// Extract zip（extractZip 内部已用 SafeJoin 校验每个条目）
		if err := extractZip(zipPath, skillDir); err != nil {
			http.Error(w, "failed to extract zip: "+err.Error(), http.StatusInternalServerError)
			return
		}
		// Remove zip file after extraction
		os.Remove(zipPath)
	default:
		// If relative path provided, preserve directory structure
		if relativePath != "" {
			// Remove the top-level folder from path (it's the skill name)
			parts := strings.SplitN(relativePath, "/", 2)
			var relPath string
			if len(parts) > 1 {
				relPath = parts[1]
			} else {
				relPath = filepath.Base(header.Filename)
			}
			// 安全拼接：防止 parts[1] 含 "../../../" 穿越到 skillDir 之外
			safeDest, err := SafeJoin(skillDir, relPath)
			if err != nil {
				http.Error(w, "invalid file path in upload: "+err.Error(), http.StatusBadRequest)
				return
			}
			destPath = safeDest
		} else {
			// Save as skill.yaml or SKILL.md based on extension
			var relName string
			switch ext {
			case ".md":
				relName = "SKILL.md"
			case ".yaml", ".yml":
				relName = "skill.yaml"
			case ".json":
				relName = "skill.json"
			default:
				// 用 Base 防止 header.Filename 含路径分隔符
				relName = filepath.Base(header.Filename)
			}
			safeDest, err := SafeJoin(skillDir, relName)
			if err != nil {
				http.Error(w, "invalid file path in upload: "+err.Error(), http.StatusBadRequest)
				return
			}
			destPath = safeDest
		}

		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			http.Error(w, "failed to create directory: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Save file
		destFile, err := os.Create(destPath)
		if err != nil {
			http.Error(w, "failed to create file: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer destFile.Close()

		if _, err := destFile.ReadFrom(file); err != nil {
			http.Error(w, "failed to save file: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Reload skills to include the newly uploaded one
	if s.skillMgr != nil {
		s.skillMgr.Reload()
	}

	jsonResponse(w, map[string]interface{}{
		"ok":   true,
		"name": skillName,
		"path": skillDir,
	})
}

func parseSkillMarkdown(data []byte, skill *Skill) {
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Parse YAML front matter in markdown
		if strings.HasPrefix(line, "# ") && skill.Name == skill.ID {
			skill.Name = strings.TrimPrefix(line, "# ")
			skill.Name = strings.TrimSpace(skill.Name)
		}
		if strings.HasPrefix(line, "description:") {
			skill.Description = strings.TrimPrefix(line, "description:")
			skill.Description = strings.TrimSpace(skill.Description)
			skill.Description = strings.Trim(skill.Description, "\"'")
		}
		if strings.HasPrefix(line, "tags:") {
			tags := strings.TrimPrefix(line, "tags:")
			tags = strings.TrimSpace(tags)
			skill.Tags = strings.Split(tags, ",")
		}
	}
	// If no description found, use first non-empty, non-heading line
	if skill.Description == "" {
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "---") {
				skill.Description = utils.Truncate(line, 100)
				break
			}
		}
	}
}

func (s *Server) handleAutoSkillAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.skillMgr == nil {
		http.Error(w, "skill manager not initialized", http.StatusServiceUnavailable)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	name, _ := req["name"].(string)
	action, _ := req["action"].(string)
	if name == "" || action == "" {
		http.Error(w, "missing name or action", http.StatusBadRequest)
		return
	}

	var err2 error
	var message string
	switch action {
	case "approve":
		err2 = s.skillMgr.ApproveAutoSkill(name)
		message = "Skill '" + name + "' has been approved"
	case "reject":
		err2 = s.skillMgr.RejectAutoSkill(name)
		message = "Skill '" + name + "' has been rejected"
	case "archive":
		err2 = s.skillMgr.ArchiveAutoSkill(name)
		message = "Skill '" + name + "' has been archived"
	case "restore":
		err2 = s.skillMgr.RestoreAutoSkill(name)
		message = "Skill '" + name + "' has been restored from archive"
	case "delete":
		err2 = s.skillMgr.DeleteAutoSkill(name)
		message = "Skill '" + name + "' has been permanently deleted"
	default:
		http.Error(w, "unknown action: "+action, http.StatusBadRequest)
		return
	}

	if err2 != nil {
		http.Error(w, err2.Error(), http.StatusBadRequest)
		return
	}

	jsonResponse(w, map[string]interface{}{
		"ok":      true,
		"action":  action,
		"name":    name,
		"message": message,
	})
}

func parseSkillJSON(data []byte, skill *Skill) {
	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return
	}
	if v, ok := obj["name"].(string); ok && v != "" {
		skill.Name = v
	}
	if v, ok := obj["description"].(string); ok {
		skill.Description = v
	}
	if v, ok := obj["category"].(string); ok {
		skill.Category = v
	}
	if v, ok := obj["tags"].([]interface{}); ok {
		tags := make([]string, 0)
		for _, t := range v {
			if s, ok := t.(string); ok {
				tags = append(tags, s)
			}
		}
		skill.Tags = tags
	}
}

func (s *Server) handleSkillHubInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, map[string]interface{}{"ok": false, "error": "method not allowed"})
		return
	}

	var req struct {
		Source   string `json:"source"`
		SourceID string `json:"sourceID"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, map[string]interface{}{"ok": false, "error": "invalid request body"})
		return
	}

	if s.skillMgr != nil {
		err := s.skillMgr.InstallFromHub(skills.HubSource(req.Source), req.SourceID)
		if err != nil {
			fmt.Printf("Hub install error: source=%s sourceID=%s err=%v\n", req.Source, req.SourceID, err)
			jsonResponse(w, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		s.skillMgr.Reload()
		jsonResponse(w, map[string]interface{}{"ok": true})
		return
	}
	jsonResponse(w, map[string]interface{}{"ok": false, "error": "skill manager not available"})
}

func parseSkillYAML(data []byte, skill *Skill) {
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			skill.Name = strings.TrimPrefix(line, "name:")
			skill.Name = strings.TrimSpace(skill.Name)
			skill.Name = strings.Trim(skill.Name, "\"'")
		}
		if strings.HasPrefix(line, "description:") {
			skill.Description = strings.TrimPrefix(line, "description:")
			skill.Description = strings.TrimSpace(skill.Description)
			skill.Description = strings.Trim(skill.Description, "\"'")
		}
		if strings.HasPrefix(line, "tags:") {
			tags := strings.TrimPrefix(line, "tags:")
			tags = strings.TrimSpace(tags)
			skill.Tags = strings.Split(tags, ",")
		}
	}
}

func (s *Server) handleSkillsSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	results := make([]Skill, 0)
	for _, skill := range s.getRealSkills() {
		if strings.Contains(strings.ToLower(skill.Name), strings.ToLower(query)) ||
			strings.Contains(strings.ToLower(skill.Description), strings.ToLower(query)) {
			results = append(results, skill)
		}
	}
	jsonResponse(w, results)
}

func (s *Server) handleSkillHubSearch(w http.ResponseWriter, r *http.Request) {
	keyword := r.URL.Query().Get("q")

	if s.skillMgr != nil {
		skillsList, err := s.skillMgr.SearchHub(keyword, nil)
		if err != nil {
			// Return empty array on error instead of 500, since registries may be unavailable
			jsonResponse(w, []skills.HubSkill{})
			return
		}
		if skillsList == nil {
			skillsList = []skills.HubSkill{}
		}
		jsonResponse(w, skillsList)
		return
	}
	jsonResponse(w, []skills.HubSkill{})
}

func (s *Server) handleSkillsStatistics(w http.ResponseWriter, r *http.Request) {
	stats := []map[string]interface{}{}

	if s.skillMgr != nil {
		allStats := s.skillMgr.GetAllStatistics()
		for _, stat := range allStats {
			stats = append(stats, map[string]interface{}{
				"skill_name":        stat.SkillName,
				"total_invocations": stat.TotalInvocations,
				"success_rate":      stat.SuccessRate,
				"avg_quality":       stat.AvgQuality,
				"avg_duration":      stat.AvgDuration,
				"positive_rate":     stat.PositiveRate,
				"last_used":         stat.LastUsed,
				"trend":             stat.Trend,
			})
		}
	}

	jsonResponse(w, stats)
}
