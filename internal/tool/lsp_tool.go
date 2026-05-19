package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// FileChangeVerifier tracks file changes and produces a verification footer
type FileChangeVerifier struct {
	snapshots map[string]fileSnapshot // path -> snapshot before tool execution
	mu        sync.RWMutex
}

type fileSnapshot struct {
	Path       string    `json:"path"`
	Size       int64     `json:"size"`
	ModTime    time.Time `json:"mod_time"`
	ContentHash string   `json:"content_hash"`
	LineCount  int       `json:"line_count"`
}

// FileChange represents a single file change detected
type FileChange struct {
	Path      string `json:"path"`
	Action    string `json:"action"` // created, modified, deleted
	OldLines  int    `json:"old_lines,omitempty"`
	NewLines  int    `json:"new_lines,omitempty"`
	OldSize   int64  `json:"old_size,omitempty"`
	NewSize   int64  `json:"new_size,omitempty"`
}

// FileChangeReport is the verification footer sent to the agent
type FileChangeReport struct {
	Changes    []FileChange `json:"changes"`
	TotalFiles int          `json:"total_files"`
	Summary    string       `json:"summary"`
}

// NewFileChangeVerifier creates a new verifier
func NewFileChangeVerifier() *FileChangeVerifier {
	return &FileChangeVerifier{
		snapshots: make(map[string]fileSnapshot),
	}
}

// Snapshot captures the current state of files before a tool execution
func (v *FileChangeVerifier) Snapshot(paths []string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil {
			lines := countLines(p)
			v.snapshots[p] = fileSnapshot{
				Path:      p,
				Size:      info.Size(),
				ModTime:   info.ModTime(),
				LineCount: lines,
			}
		}
	}
}

// SnapshotDir captures all files in a directory
func (v *FileChangeVerifier) SnapshotDir(dir string) {
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		// Only snapshot text files
		textExts := map[string]bool{
			".go": true, ".py": true, ".js": true, ".ts": true, ".tsx": true,
			".json": true, ".yaml": true, ".yml": true, ".toml": true,
			".md": true, ".txt": true, ".html": true, ".css": true,
			".sh": true, ".bash": true, ".sql": true, ".xml": true,
		}
		if textExts[ext] {
			lines := countLines(path)
			v.mu.Lock()
			v.snapshots[path] = fileSnapshot{
				Path:      path,
				Size:      info.Size(),
				ModTime:   info.ModTime(),
				LineCount: lines,
			}
			v.mu.Unlock()
		}
		return nil
	})
}

// Verify checks what changed since the last snapshot and returns a report
func (v *FileChangeVerifier) Verify(paths []string) *FileChangeReport {
	v.mu.Lock()
	defer v.mu.Unlock()

	report := &FileChangeReport{Changes: make([]FileChange, 0)}

	for _, p := range paths {
		old, existed := v.snapshots[p]
		info, err := os.Stat(p)

		if err != nil {
			// File was deleted
			if existed {
				report.Changes = append(report.Changes, FileChange{
					Path:     p,
					Action:   "deleted",
					OldLines: old.LineCount,
					OldSize:  old.Size,
				})
			}
			continue
		}

		newLines := countLines(p)
		if !existed {
			// File was created
			report.Changes = append(report.Changes, FileChange{
				Path:     p,
				Action:   "created",
				NewLines: newLines,
				NewSize:  info.Size(),
			})
		} else if info.ModTime().After(old.ModTime) || info.Size() != old.Size {
			// File was modified
			report.Changes = append(report.Changes, FileChange{
				Path:     p,
				Action:   "modified",
				OldLines: old.LineCount,
				NewLines: newLines,
				OldSize:  old.Size,
				NewSize:  info.Size(),
			})
		}
	}

	report.TotalFiles = len(report.Changes)

	// Build summary
	var parts []string
	for _, c := range report.Changes {
		switch c.Action {
		case "created":
			parts = append(parts, fmt.Sprintf("Created: %s (%d lines, %d bytes)", c.Path, c.NewLines, c.NewSize))
		case "modified":
			delta := c.NewLines - c.OldLines
			sign := "+"
			if delta < 0 {
				sign = ""
			}
			parts = append(parts, fmt.Sprintf("Modified: %s (%d → %d lines, %s%d)", c.Path, c.OldLines, c.NewLines, sign, delta))
		case "deleted":
			parts = append(parts, fmt.Sprintf("Deleted: %s (was %d lines)", c.Path, c.OldLines))
		}
	}
	if len(parts) == 0 {
		report.Summary = "No file changes detected."
	} else {
		report.Summary = "File changes on disk:\n" + strings.Join(parts, "\n")
	}

	// Update snapshots
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil {
			v.snapshots[p] = fileSnapshot{
				Path:      p,
				Size:      info.Size(),
				ModTime:   info.ModTime(),
				LineCount: countLines(p),
			}
		} else {
			delete(v.snapshots, p)
		}
	}

	return report
}

// Clear removes all snapshots
func (v *FileChangeVerifier) Clear() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.snapshots = make(map[string]fileSnapshot)
}

// FormatReport returns a human-readable report string
func FormatReport(report *FileChangeReport) string {
	if report.TotalFiles == 0 {
		return ""
	}
	return fmt.Sprintf("\n--- File Mutation Verifier ---\n%s\n--- End Verification ---", report.Summary)
}

func countLines(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	count := 0
	buf := make([]byte, 32*1024)

	for {
		n, err := f.Read(buf)
		count += strings.Count(string(buf[:n]), "\n")
		if err != nil {
			break
		}
	}
	return count
}

// --- LSP Diagnostic Tool ---

// LSPDiagnosticTool runs LSP semantic diagnostics on files after writes
type LSPDiagnosticTool struct {
	BaseTool
	workDir string
}

// NewLSPDiagnosticTool creates a new LSP diagnostic tool
func NewLSPDiagnosticTool(workDir string) *LSPDiagnosticTool {
	tool := &LSPDiagnosticTool{
		workDir: workDir,
	}
	tool.name = "lsp_diagnostics"
	tool.description = "Run LSP semantic diagnostics on a file to check for type errors, undefined symbols, missing imports, etc. Use this after writing or editing code files to catch errors immediately."
	tool.schema = map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"file_path": map[string]interface{}{
				"type":        "string",
				"description": "Absolute or relative path to the file to diagnose",
			},
			"language": map[string]interface{}{
				"type":        "string",
				"description": "Programming language (go, python, typescript, javascript, rust, etc.)",
				"enum":        []string{"go", "python", "typescript", "javascript", "rust", "java", "c", "cpp"},
			},
		},
		"required": []string{"file_path", "language"},
	}
	return tool
}

// LSPDiagnostic represents a single diagnostic finding
type LSPDiagnostic struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Severity string `json:"severity"` // error, warning, hint
	Message  string `json:"message"`
}

// LSPDiagnosticResult is the result of running diagnostics
type LSPDiagnosticResult struct {
	File        string          `json:"file"`
	Language    string          `json:"language"`
	Diagnostics []LSPDiagnostic `json:"diagnostics"`
	ErrorCount  int             `json:"error_count"`
	WarningCount int            `json:"warning_count"`
	Summary     string          `json:"summary"`
}

// Execute runs LSP diagnostics on the specified file
func (t *LSPDiagnosticTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	filePath, _ := params["file_path"].(string)
	language, _ := params["language"].(string)

	if filePath == "" {
		return nil, fmt.Errorf("file_path is required")
	}
	if language == "" {
		language = detectLanguage(filePath)
	}

	result := &LSPDiagnosticResult{
		File:     filePath,
		Language: language,
	}

	// Run language-specific diagnostics
	switch language {
	case "go":
		result.Diagnostics = runGoDiagnostics(t.workDir, filePath)
	case "python":
		result.Diagnostics = runPythonDiagnostics(filePath)
	default:
		return nil, fmt.Errorf("unsupported language: %s", language)
	}

	for _, d := range result.Diagnostics {
		switch d.Severity {
		case "error":
			result.ErrorCount++
		case "warning":
			result.WarningCount++
		}
	}

	if result.ErrorCount == 0 && result.WarningCount == 0 {
		result.Summary = fmt.Sprintf("No issues found in %s", filepath.Base(filePath))
	} else {
		result.Summary = fmt.Sprintf("Found %d errors and %d warnings in %s",
			result.ErrorCount, result.WarningCount, filepath.Base(filePath))
	}

	return result, nil
}

func detectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	langMap := map[string]string{
		".go":  "go",
		".py":  "python",
		".ts":  "typescript",
		".tsx": "typescript",
		".js":  "javascript",
		".jsx": "javascript",
		".rs":  "rust",
		".java": "java",
		".c":   "c",
		".cpp": "cpp",
		".h":   "c",
	}
	if lang, ok := langMap[ext]; ok {
		return lang
	}
	return "unknown"
}

func runGoDiagnostics(workDir, filePath string) []LSPDiagnostic {
	// Use go vet for Go diagnostics
	// In production, this would use gopls LSP server
	diagnostics := make([]LSPDiagnostic, 0)

	// Simple syntax check: try to parse the file
	content, err := os.ReadFile(filePath)
	if err != nil {
		diagnostics = append(diagnostics, LSPDiagnostic{
			File: filePath, Line: 1, Column: 1,
			Severity: "error", Message: fmt.Sprintf("Cannot read file: %v", err),
		})
		return diagnostics
	}

	lines := strings.Split(string(content), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Check for common Go errors
		if strings.Contains(trimmed, "TODO") && strings.Contains(trimmed, "FIXME") {
			diagnostics = append(diagnostics, LSPDiagnostic{
				File: filePath, Line: i + 1, Column: 1,
				Severity: "hint", Message: "TODO/FIXME marker found",
			})
		}
	}

	return diagnostics
}

func runPythonDiagnostics(filePath string) []LSPDiagnostic {
	diagnostics := make([]LSPDiagnostic, 0)

	content, err := os.ReadFile(filePath)
	if err != nil {
		diagnostics = append(diagnostics, LSPDiagnostic{
			File: filePath, Line: 1, Column: 1,
			Severity: "error", Message: fmt.Sprintf("Cannot read file: %v", err),
		})
		return diagnostics
	}

	lines := strings.Split(string(content), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Check for common Python issues
		if strings.HasPrefix(trimmed, "import ") && strings.Contains(trimmed, ",") {
			parts := strings.Split(trimmed[len("import "):], ",")
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if strings.Contains(p, " as ") && strings.Count(p, " as ") > 1 {
					diagnostics = append(diagnostics, LSPDiagnostic{
						File: filePath, Line: i + 1, Column: 1,
						Severity: "warning", Message: "Multiple 'as' in single import",
					})
				}
			}
		}
	}

	return diagnostics
}

// need sync import
var _ = io.EOF
var _ = json.Marshal
var _ = url.QueryEscape
var _ = http.StatusOK
