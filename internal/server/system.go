package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/internal/slash"
)

// versionCheck cache state (lives for the lifetime of the process).
var (
	versionCheckCache     map[string]interface{}
	versionCheckCacheTime time.Time
	versionCheckMu        sync.Mutex
)

// versionCheckHTTPClient carries a hard 10s timeout as a backstop in case the
// per-request context is missing or its deadline is not respected.
var versionCheckHTTPClient = &http.Client{Timeout: 10 * time.Second}

func (s *Server) handleUsageMonthly(w http.ResponseWriter, r *http.Request) {
	if s.usageMgr == nil {
		jsonResponse(w, []map[string]interface{}{})
		return
	}
	// Build last N months of aggregated data (front-end shows table)
	now := time.Now()
	result := make([]map[string]interface{}, 0, 12)

	// Go through all available daily stats, aggregated by month
	type monthTotals struct {
		sessions int
		input    int
		output   int
		cost     float64
	}
	months := make(map[string]*monthTotals)

	// Single bulk fetch over the last 12 months instead of 365 daily lock acquisitions.
	start := now.AddDate(0, 0, -365)
	dailyStats := s.usageMgr.GetDailyStatsRange(start, now)
	for dateStr, stats := range dailyStats {
		t, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}
		monthStr := t.Format("2006-01")
		if _, ok := months[monthStr]; !ok {
			months[monthStr] = &monthTotals{}
		}
		months[monthStr].sessions += stats.TotalRequests
		months[monthStr].input += stats.TotalInput
		months[monthStr].output += stats.TotalOutput
		months[monthStr].cost += stats.TotalCost
	}

	// Sort month keys (descending)
	keys := make([]string, 0, len(months))
	for k := range months {
		keys = append(keys, k)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(keys)))

	for _, month := range keys {
		m := months[month]
		totalTokens := m.input + m.output
		result = append(result, map[string]interface{}{
			"month":          month,
			"total_sessions": m.sessions,
			"total_messages": m.sessions,
			"total_tokens":   totalTokens,
			"total_cost":     m.cost,
		})
	}

	jsonResponse(w, result)
}

func (s *Server) handleUsageDaily(w http.ResponseWriter, r *http.Request) {
	if s.usageMgr == nil {
		jsonResponse(w, []map[string]interface{}{})
		return
	}
	days := 30
	if d := r.URL.Query().Get("days"); d != "" {
		if n, err := strconv.Atoi(d); err == nil && n > 0 && n <= 365 {
			days = n
		}
	}
	now := time.Now()
	start := now.AddDate(0, 0, -days+1)
	// Single bulk fetch instead of N individual lock acquisitions.
	dailyStats := s.usageMgr.GetDailyStatsRange(start, now)
	result := make([]map[string]interface{}, 0, days)
	for i := 0; i < days; i++ {
		date := now.AddDate(0, 0, -i).Format("2006-01-02")
		stats, ok := dailyStats[date]
		if !ok {
			result = append(result, map[string]interface{}{
				"date":          date,
				"sessions":      0,
				"messages":      0,
				"input_tokens":  0,
				"output_tokens": 0,
				"total_tokens":  0,
				"cost":          0.0,
			})
			continue
		}
		result = append(result, map[string]interface{}{
			"date":          stats.Date,
			"sessions":      stats.TotalRequests,
			"messages":      stats.TotalRequests,
			"input_tokens":  stats.TotalInput,
			"output_tokens": stats.TotalOutput,
			"total_tokens":  stats.TotalInput + stats.TotalOutput,
			"cost":          stats.TotalCost,
		})
	}
	jsonResponse(w, result)
}

func (s *Server) handleSystemHealth(w http.ResponseWriter, r *http.Request) {
	llmStatus := "not_configured"
	if s.provider != nil {
		llmStatus = "connected"
	}
	dbStatus := "disconnected"
	if s.sessionStore != nil {
		dbStatus = "connected"
	}

	jsonResponse(w, map[string]interface{}{
		"status": "healthy",
		"checks": map[string]string{
			"server":   "ok",
			"database": dbStatus,
			"llm":      llmStatus,
		},
	})
}

func (s *Server) handleSystemStats(w http.ResponseWriter, r *http.Request) {
	sessions := 0
	messages := 0
	if s.sessionStore != nil {
		if list, err := s.sessionStore.ListSessions(context.Background(), ""); err == nil {
			sessions = len(list)
			for _, sess := range list {
				messages += len(sess.Messages)
			}
		}
	}

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	uptimeSeconds := int(time.Since(s.startTime).Seconds())

	jsonResponse(w, map[string]interface{}{
		"sessions":     sessions,
		"messages":     messages,
		"uptime":       uptimeSeconds,
		"memory_usage": memStats.Alloc,
		"goroutines":   runtime.NumGoroutine(),
	})
}

func (s *Server) handleUsageInsights(w http.ResponseWriter, r *http.Request) {
	if s.usageMgr == nil {
		jsonResponse(w, map[string]interface{}{
			"total_sessions":         0,
			"total_messages":         0,
			"total_input_tokens":     0,
			"total_output_tokens":    0,
			"total_cost":             0.0,
			"avg_cost_per_session":   0.0,
			"avg_cost_per_message":   0.0,
			"avg_tokens_per_message": 0,
			"most_used_model":        "",
			"most_active_hour":       0,
			"most_active_day":        "",
		})
		return
	}
	days := 30
	if d := r.URL.Query().Get("days"); d != "" {
		if n, err := strconv.Atoi(d); err == nil && n > 0 && n <= 365 {
			days = n
		}
	}
	insights, err := s.usageMgr.GetInsights(days)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// Map backend Insights to frontend UsageInsight fields
	totalSessions := insights.TotalRequests
	totalCost := insights.TotalCost
	var mostUsedModel string
	if len(insights.TopModels) > 0 {
		mostUsedModel = insights.TopModels[0].Model
	}

	result := map[string]interface{}{
		"total_sessions":         totalSessions,
		"total_messages":         totalSessions,
		"total_input_tokens":     0, // Not tracked separately in insights
		"total_output_tokens":    0,
		"total_cost":             totalCost,
		"avg_cost_per_session":   insights.AvgCostPerReq,
		"avg_cost_per_message":   insights.AvgCostPerReq,
		"avg_tokens_per_message": int(insights.AvgTokensPerReq),
		"most_used_model":        mostUsedModel,
		"top_models":             insights.TopModels,
		"most_active_hour":       0,
		"most_active_day":        "",
	}

	// Also include raw token breakdown from the manager itself if possible
	// For now, try to get a more accurate input/output split using daily totals
	// over the same period using the dailyStats data.
	totalIn, totalOut := s.usageMgr.EstimateTokenSplit(days)
	result["total_input_tokens"] = totalIn
	result["total_output_tokens"] = totalOut
	result["total_tokens"] = totalIn + totalOut

	jsonResponse(w, result)
}

func (s *Server) handleUsageProviders(w http.ResponseWriter, r *http.Request) {
	if s.usageMgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "usage manager not available"})
		return
	}
	days := 30
	if d := r.URL.Query().Get("days"); d != "" {
		if v, err := strconv.Atoi(d); err == nil {
			days = v
		}
	}
	providers, err := s.usageMgr.GetProviderBreakdown(days)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	jsonResponse(w, providers)
}

func (s *Server) handleUsageHourly(w http.ResponseWriter, r *http.Request) {
	if s.usageMgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "usage manager not available"})
		return
	}
	days := 30
	if d := r.URL.Query().Get("days"); d != "" {
		if v, err := strconv.Atoi(d); err == nil {
			days = v
		}
	}
	hourly, err := s.usageMgr.GetHourlyBreakdown(days)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	jsonResponse(w, hourly)
}

func compareVersions(v1, v2 string) int {
	// Handle "dev" or empty versions
	if v1 == "dev" || v1 == "" {
		return -1
	}
	if v2 == "dev" || v2 == "" {
		return 1
	}

	parts1 := strings.Split(v1, ".")
	parts2 := strings.Split(v2, ".")

	for i := 0; i < len(parts1) && i < len(parts2); i++ {
		n1, _ := strconv.Atoi(parts1[i])
		n2, _ := strconv.Atoi(parts2[i])
		if n1 > n2 {
			return 1
		}
		if n1 < n2 {
			return -1
		}
	}

	if len(parts1) > len(parts2) {
		return 1
	}
	if len(parts1) < len(parts2) {
		return -1
	}
	return 0
}

func (s *Server) handleUsageSessions(w http.ResponseWriter, r *http.Request) {
	if s.usageMgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "usage manager not available"})
		return
	}
	date := r.URL.Query().Get("date")
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	sessions, err := s.usageMgr.GetSessionStats(date)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	jsonResponse(w, sessions)
}

func (s *Server) handleUsageTopSessions(w http.ResponseWriter, r *http.Request) {
	if s.usageMgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "usage manager not available"})
		return
	}
	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil {
			limit = v
		}
	}
	sessions, err := s.usageMgr.GetTopSessions(limit)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	jsonResponse(w, sessions)
}

func (s *Server) handleSystemVersion(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{
		"version":    s.version,
		"commit":     s.commit,
		"build_date": s.buildDate,
		"platform":   runtime.GOOS,
		"arch":       runtime.GOARCH,
	})
}

func (s *Server) handleUsageBudget(w http.ResponseWriter, r *http.Request) {
	if s.usageMgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "usage manager not available"})
		return
	}
	switch r.Method {
	case "GET":
		budget := s.usageMgr.GetBudget()
		jsonResponse(w, budget)
	case "PUT":
		var req struct {
			Limit          float64 `json:"limit"`
			AlertThreshold float64 `json:"alert_threshold"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", 400)
			return
		}
		if err := s.usageMgr.SetBudget(req.Limit, req.AlertThreshold); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		jsonResponse(w, map[string]interface{}{"status": "ok"})
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *Server) handleUsageWeekly(w http.ResponseWriter, r *http.Request) {
	if s.usageMgr == nil {
		jsonResponse(w, map[string]interface{}{
			"sessions":      0,
			"messages":      0,
			"input_tokens":  0,
			"output_tokens": 0,
			"total_tokens":  0,
			"cost":          0.0,
		})
		return
	}
	stats, err := s.usageMgr.GetWeeklyStats()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	jsonResponse(w, map[string]interface{}{
		"sessions":      stats.TotalRequests,
		"messages":      stats.TotalRequests,
		"input_tokens":  stats.TotalInput,
		"output_tokens": stats.TotalOutput,
		"total_tokens":  stats.TotalInput + stats.TotalOutput,
		"cost":          stats.TotalCost,
	})
}

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	jsonResponse(w, map[string]interface{}{
		"version":      s.version,
		"platform":     runtime.GOOS,
		"arch":         runtime.GOARCH,
		"go_version":   runtime.Version(),
		"memory_usage": memStats.Alloc,
		"goroutines":   runtime.NumGoroutine(),
	})
}

func (s *Server) handlePreferenceFeedback(profileName, key string, accurate bool) {
	// In production, this should update Cortex UserProfile
	// For now, just log the feedback
	fmt.Printf("Preference feedback: profile=%s, key=%s, accurate=%v\n", profileName, key, accurate)
}

func (s *Server) handleCommands(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	manager := slash.NewManager()
	commands := manager.List()

	jsonResponse(w, commands)
}

func (s *Server) handleUsageToday(w http.ResponseWriter, r *http.Request) {
	if s.usageMgr == nil {
		jsonResponse(w, map[string]interface{}{
			"sessions":          0,
			"messages":          0,
			"input_tokens":      0,
			"output_tokens":     0,
			"total_tokens":      0,
			"cost":              0.0,
			"avg_response_time": 0,
			"top_models":        []interface{}{},
		})
		return
	}
	stats, err := s.usageMgr.GetTodayStats()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// Normalize to frontend-friendly field names
	// TotalRequests represents both sessions and messages in our usage-tracking model
	result := map[string]interface{}{
		"sessions":          stats.TotalRequests,
		"messages":          stats.TotalRequests,
		"input_tokens":      stats.TotalInput,
		"output_tokens":     stats.TotalOutput,
		"total_tokens":      stats.TotalInput + stats.TotalOutput,
		"cost":              stats.TotalCost,
		"avg_response_time": 0,
		"top_models":        []interface{}{},
	}
	jsonResponse(w, result)
}

func (s *Server) handleVersionCheck(w http.ResponseWriter, r *http.Request) {
	// Serve cached result if still fresh (avoids hammering GitHub API).
	versionCheckMu.Lock()
	if versionCheckCache != nil && time.Since(versionCheckCacheTime) < time.Hour {
		cached := versionCheckCache
		versionCheckMu.Unlock()
		jsonResponse(w, cached)
		return
	}
	versionCheckMu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	owner := "magicwubiao"
	repo := "go-magic"
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		http.Error(w, "failed to create request", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "go-magic/"+s.version)

	resp, err := versionCheckHTTPClient.Do(req)
	if err != nil {
		http.Error(w, "failed to check for updates: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, "github api error", resp.StatusCode)
		return
	}

	var release struct {
		TagName     string    `json:"tag_name"`
		Name        string    `json:"name"`
		Body        string    `json:"body"`
		Prerelease  bool      `json:"prerelease"`
		PublishedAt time.Time `json:"published_at"`
		HTMLURL     string    `json:"html_url"`
		Assets      []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
			Size               int64  `json:"size"`
		} `json:"assets"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		http.Error(w, "failed to parse response", http.StatusInternalServerError)
		return
	}

	// Compare versions
	latestVersion := strings.TrimPrefix(release.TagName, "v")
	currentVersion := strings.TrimPrefix(s.version, "v")
	hasUpdate := compareVersions(latestVersion, currentVersion) > 0

	// Find appropriate asset for current platform
	var downloadURL string
	var assetSize int64
	platform := runtime.GOOS
	arch := runtime.GOARCH

	// Match actual release asset names: go-magic-windows-amd64.exe, go-magic-linux-amd64, etc.
	var patterns []string
	if platform == "windows" {
		patterns = []string{
			fmt.Sprintf("go-magic-%s-%s.exe", platform, arch),
			fmt.Sprintf("magic-%s-%s.exe", platform, arch),
		}
	} else {
		patterns = []string{
			fmt.Sprintf("go-magic-%s-%s", platform, arch),
			fmt.Sprintf("magic-%s-%s", platform, arch),
		}
	}

	for _, asset := range release.Assets {
		for _, pattern := range patterns {
			if asset.Name == pattern {
				downloadURL = asset.BrowserDownloadURL
				assetSize = asset.Size
				break
			}
		}
		if downloadURL != "" {
			break
		}
	}

	// Fallback: substring match
	if downloadURL == "" {
		expectedSub := fmt.Sprintf("%s-%s", platform, arch)
		for _, asset := range release.Assets {
			if strings.Contains(asset.Name, expectedSub) {
				downloadURL = asset.BrowserDownloadURL
				assetSize = asset.Size
				break
			}
		}
	}

	result := map[string]interface{}{
		"current_version": currentVersion,
		"latest_version":  latestVersion,
		"has_update":      hasUpdate,
		"release_name":    release.Name,
		"release_notes":   release.Body,
		"published_at":    release.PublishedAt,
		"html_url":        release.HTMLURL,
		"download_url":    downloadURL,
		"asset_size":      assetSize,
		"prerelease":      release.Prerelease,
	}

	// Cache for 1 hour so subsequent requests don't re-hit GitHub.
	versionCheckMu.Lock()
	versionCheckCache = result
	versionCheckCacheTime = time.Now()
	versionCheckMu.Unlock()

	jsonResponse(w, result)
}

func (s *Server) handleCommandExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Command   string `json:"command"`
		SessionID string `json:"session_id,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Command == "" {
		http.Error(w, "Command is required", http.StatusBadRequest)
		return
	}

	manager := slash.NewManager()
	result, err := manager.Execute(r.Context(), req.Command)

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	jsonResponse(w, map[string]interface{}{
		"success": true,
		"result":  result,
	})
}
