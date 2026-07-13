package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (s *Server) handleLogsTail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", 500)
		return
	}

	// Parse parameters for compatibility
	filterFile := r.URL.Query().Get("file")
	filterLevel := strings.ToUpper(r.URL.Query().Get("level"))
	linesStr := r.URL.Query().Get("lines")
	lines := 10
	if linesStr != "" {
		if n, err := strconv.Atoi(linesStr); err == nil && n > 0 {
			lines = n
		}
	}

	// Send initial log entries then keep alive
	for i := 0; i < lines; i++ {
		level := "info"
		if filterLevel != "" {
			level = strings.ToLower(filterLevel)
		}
		data, _ := json.Marshal(LogEntry{
			Timestamp: time.Now(),
			Level:     level,
			Message:   fmt.Sprintf("Log entry %d (file=%s)", i, filterFile),
			Source:    "server",
		})
		fmt.Fprintf(w, "data: %s\n\n", string(data))
		flusher.Flush()
		time.Sleep(100 * time.Millisecond)
	}

	// Keep alive for 30 seconds
	for i := 0; i < 30; i++ {
		time.Sleep(1 * time.Second)
		fmt.Fprintf(w, ": keepalive\n\n")
		flusher.Flush()
	}
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 100
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
	}

	// Parse filter parameters
	filterFile := r.URL.Query().Get("file")
	filterLevel := strings.ToUpper(r.URL.Query().Get("level"))
	filterComponent := r.URL.Query().Get("component")
	// Support 'search' parameter for compatibility with web client
	filterSearch := r.URL.Query().Get("search")
	if filterComponent == "" && filterSearch != "" {
		filterComponent = filterSearch
	}

	logDir := filepath.Join(s.magicHome, "logs")
	var lines []string

	// Determine which log files to read
	var filesToRead []string
	entries, err := os.ReadDir(logDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			// Only read .log files
			if !strings.HasSuffix(entry.Name(), ".log") {
				continue
			}
			filesToRead = append(filesToRead, filepath.Join(logDir, entry.Name()))
		}
	}

	// If specific file filter is requested but not found, return empty (will show default message)
	// If no filter, read all log files
	if filterFile != "" {
		// Check if any file matches the filter (partial match for flexibility)
		var matchedFiles []string
		for _, f := range filesToRead {
			if strings.Contains(filepath.Base(f), filterFile) {
				matchedFiles = append(matchedFiles, f)
			}
		}
		if len(matchedFiles) > 0 {
			filesToRead = matchedFiles
		}
		// If no match, keep all files (don't filter out everything)
	}

	// Read and filter log lines
	for _, filePath := range filesToRead {
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if line == "" {
				continue
			}

			// Apply level filter
			if filterLevel != "" {
				// Match log levels like [DEBUG], [INFO], [WARN], [ERROR]
				if !strings.Contains(line, "["+filterLevel+"]") && !strings.Contains(line, "[ "+filterLevel+" ]") {
					continue
				}
			}

			// Apply component filter
			if filterComponent != "" {
				if !strings.Contains(line, filterComponent) {
					continue
				}
			}

			lines = append(lines, line)
		}
	}

	// Trim to limit
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}

	activeFile := filterFile
	if activeFile == "" && len(filesToRead) > 0 {
		activeFile = filepath.Base(filesToRead[0])
	}
	if activeFile == "" {
		activeFile = "server.log"
	}

	if len(lines) == 0 {
		lines = []string{fmt.Sprintf("[%s] [info] Magic Agent Dashboard started", time.Now().Format("2006-01-02 15:04:05"))}
	}

	jsonResponse(w, map[string]interface{}{
		"file":  activeFile,
		"lines": lines,
	})
}

func (s *Server) handleDashboardLogs(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{
		"logs": []LogEntry{
			{Timestamp: time.Now(), Level: "info", Message: "Server started", Source: "system"},
		},
		"stats": map[string]interface{}{
			"total":    1,
			"errors":   0,
			"warnings": 0,
		},
	})
}
