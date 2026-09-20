package tool

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/utils"
)

// FileChangeVerifier tracks file changes and produces a verification footer
type FileChangeVerifier struct {
	snapshots map[string]fileSnapshot // path -> snapshot before tool execution
	mu        sync.RWMutex
}

type fileSnapshot struct {
	Path        string    `json:"path"`
	Size        int64     `json:"size"`
	ModTime     time.Time `json:"mod_time"`
	ContentHash string    `json:"content_hash"`
	LineCount   int       `json:"line_count"`
}

// FileChange represents a single file change detected
type FileChange struct {
	Path     string `json:"path"`
	Action   string `json:"action"` // created, modified, deleted
	OldLines int    `json:"old_lines,omitempty"`
	NewLines int    `json:"new_lines,omitempty"`
	OldSize  int64  `json:"old_size,omitempty"`
	NewSize  int64  `json:"new_size,omitempty"`
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
	tool.description = "Run semantic/syntax diagnostics on one source file to catch errors right after writing or editing it. Reliable for go, python, javascript and typescript. Returns only findings for the requested file; unsupported languages return an explicit error instead of a false all-clear."
	tool.schema = map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"file_path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the file to diagnose (relative to the working directory or absolute)",
			},
			"language": map[string]interface{}{
				"type":        "string",
				"description": "Optional; auto-detected from the file extension when omitted. Only languages with a working checker are listed — other languages are rejected with an error rather than silently reported as clean.",
				"enum":        []string{"go", "python", "typescript", "javascript"},
			},
		},
		"required": []string{"file_path"},
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
	File         string          `json:"file"`
	Language     string          `json:"language"`
	Checker      string          `json:"checker,omitempty"`
	Diagnostics  []LSPDiagnostic `json:"diagnostics"`
	ErrorCount   int             `json:"error_count"`
	WarningCount int             `json:"warning_count"`
	Summary      string          `json:"summary"`
	Note         string          `json:"note,omitempty"`
}

// supportedDiagnosticLanguages is both the dispatch domain and the text used in
// the "unsupported language" error, so the two cannot drift apart.
var supportedDiagnosticLanguages = []string{"go", "python", "typescript", "javascript"}

// Execute runs LSP diagnostics on the specified file
func (t *LSPDiagnosticTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	rawPath, _ := params["file_path"].(string)
	language, _ := params["language"].(string)

	if strings.TrimSpace(rawPath) == "" {
		return nil, fmt.Errorf("file_path is required")
	}

	// Resolve before touching the file system. The previous implementation read
	// the raw argument, so a relative path was resolved against the server
	// process CWD — and a failed read became a *diagnostic* ("Cannot read
	// file: ..."), which made a path mistake look like a code error the agent
	// then tried to "fix". Fail loudly instead.
	filePath, err := t.resolveFile(ctx, rawPath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat %q (resolved to %q): %w", rawPath, filePath, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%q is a directory; lsp_diagnostics checks a single file (use the `lint` tool for a project)",
			filePath)
	}

	if language == "" {
		language = detectLanguage(filePath)
	}
	language = normalizeLanguage(language)

	result := &LSPDiagnosticResult{
		File:     filePath,
		Language: language,
	}

	var note string
	switch language {
	case "go":
		result.Diagnostics, result.Checker, note, err = runGoDiagnostics(ctx, filePath)
	case "python":
		result.Diagnostics, result.Checker, note, err = runPythonDiagnostics(filePath)
	case "javascript":
		result.Diagnostics, result.Checker, note, err = runJSDiagnostics(filePath)
	case "typescript":
		var projectTotal int
		result.Diagnostics, projectTotal, result.Checker, note, err = runTSProjectCheck(ctx, filePath)
		if err == nil && projectTotal > len(result.Diagnostics) {
			note += fmt.Sprintf("; %d more diagnostic(s) exist elsewhere in the project",
				projectTotal-len(result.Diagnostics))
		}
	default:
		// An unimplemented language used to be answered with
		// "unsupported language: javascript" while the schema still advertised
		// javascript/rust/java/c/cpp as valid inputs. The schema now lists only
		// what is implemented, and this error stays explicit so the agent can
		// switch tools instead of retrying.
		return nil, fmt.Errorf(
			"unsupported language %q: this build runs diagnostics for %s only (use the `lint` tool or execute_command for anything else)",
			language, strings.Join(supportedDiagnosticLanguages, ", "))
	}
	if err != nil {
		return nil, err
	}
	result.Note = note

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

// resolveFile resolves a caller-supplied path the way the file tools do.
//
// The constructor-time workDir is only a fallback: it is a snapshot taken when
// the registry was built, and the live session workdir (injected per turn) is
// the authoritative one.
func (t *LSPDiagnosticTool) resolveFile(ctx context.Context, p string) (string, error) {
	if WorkDirFromContext(ctx) == "" && t.workDir != "" {
		ctx = WithWorkDir(ctx, t.workDir)
	}
	return resolvePath(ctx, p)
}

// normalizeLanguage folds the aliases a model may pass into canonical names.
func normalizeLanguage(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "go", "golang":
		return "go"
	case "python", "py", "python3":
		return "python"
	case "typescript", "ts", "tsx", "vue":
		return "typescript"
	case "javascript", "js", "jsx", "mjs", "cjs", "node":
		return "javascript"
	default:
		return strings.ToLower(strings.TrimSpace(lang))
	}
}

func detectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	langMap := map[string]string{
		".go":   "go",
		".py":   "python",
		".ts":   "typescript",
		".tsx":  "typescript",
		".vue":  "typescript",
		".js":   "javascript",
		".jsx":  "javascript",
		".mjs":  "javascript",
		".cjs":  "javascript",
		".rs":   "rust",
		".java": "java",
		".c":    "c",
		".cpp":  "cpp",
		".h":    "c",
	}
	if lang, ok := langMap[ext]; ok {
		return lang
	}
	return "unknown"
}

// --- per-language diagnostic runners ---
//
// Every runner returns (diagnostics, checker, note, error). No error with zero
// diagnostics means "checked and clean"; a non-nil error means "could not be
// checked" and is surfaced to the caller, so an unchecked file is never
// presented as a clean one.

// goDiagRe matches the "<file>:<line>:<col>: <message>" shape shared by gofmt
// and go vet.
var goDiagRe = regexp.MustCompile(`^(.+?):(\d+):(\d+):\s*(.*)$`)

// goVetTimeout bounds `go vet`, which has to load and type-check a whole package.
const goVetTimeout = 90 * time.Second

// runGoDiagnostics checks a Go file with the real toolchain:
//
//  1. `gofmt -e` for syntax errors — per file, fast, no module required.
//  2. `go vet <package>` for the type/semantic errors gopls would surface,
//     skipped while the file does not parse (vet cannot parse it either).
//
// The previous implementation only looked for lines containing both "TODO" and
// "FIXME" and called that diagnostics, while claiming "In production, this would
// use gopls".
func runGoDiagnostics(ctx context.Context, filePath string) ([]LSPDiagnostic, string, string, error) {
	if _, err := exec.LookPath("gofmt"); err != nil {
		return nil, "", "", fmt.Errorf("gofmt not found on PATH; install the Go toolchain to check Go files")
	}

	syntax := make([]LSPDiagnostic, 0)
	cmd := exec.CommandContext(ctx, "gofmt", "-e", filePath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		syntax = parseGoDiagnostics(stderr.String(), filePath, "")
		for i := range syntax {
			syntax[i].Severity = "error"
		}
	}
	if len(syntax) > 0 {
		return syntax, "gofmt", "syntax errors found: `go vet` was skipped until the file parses", nil
	}

	moduleRoot, pkgRel := goModuleContext(filePath)
	if moduleRoot == "" {
		return nil, "gofmt", "file is not inside a Go module: only syntax was checked", nil
	}

	pattern := "."
	if pkgRel != "" {
		pattern = "./" + filepath.ToSlash(pkgRel)
	}

	vctx, cancel := context.WithTimeout(ctx, goVetTimeout)
	defer cancel()

	vet := exec.CommandContext(vctx, "go", "vet", pattern)
	vet.Dir = moduleRoot
	var out bytes.Buffer
	vet.Stdout = &out
	vet.Stderr = &out
	runErr := vet.Run()

	if vctx.Err() == context.DeadlineExceeded {
		return nil, "go vet", "", fmt.Errorf("`go vet %s` in %s timed out after %s", pattern, moduleRoot, goVetTimeout)
	}
	if runErr == nil {
		return nil, "go vet", "", nil
	}

	diags := parseGoDiagnostics(out.String(), filePath, moduleRoot)
	if len(diags) == 0 {
		// The package failed to build but nothing was attributed to this file
		// (the error is in a sibling file, or the failure is environmental).
		// Report it as an error rather than a clean result for this file.
		return nil, "go vet", "", fmt.Errorf("`go vet %s` in %s failed with no diagnostic in %s; output:\n%s",
			pattern, moduleRoot, filepath.Base(filePath), utils.Truncate(strings.TrimSpace(out.String()), 1200))
	}
	for i := range diags {
		diags[i].Severity = "error"
	}
	return diags, "go vet", "", nil
}

// parseGoDiagnostics keeps only the `<file>:<line>:<col>: <msg>` entries that
// belong to targetFile. base resolves relative references: `go vet` prints paths
// relative to the directory it ran in.
func parseGoDiagnostics(out, targetFile, base string) []LSPDiagnostic {
	var diags []LSPDiagnostic
	for _, raw := range strings.Split(out, "\n") {
		l := strings.TrimSpace(raw)
		if l == "" {
			continue
		}
		m := goDiagRe.FindStringSubmatch(l)
		if m == nil {
			// Position-less lines (package headers, "exit status 1") belong to
			// the package, not to this file.
			continue
		}
		ref := m[1]
		if !filepath.IsAbs(ref) && base != "" {
			ref = filepath.Join(base, ref)
		}
		if !sameFile(ref, targetFile, base) {
			continue
		}
		line, _ := strconv.Atoi(m[2])
		col, _ := strconv.Atoi(m[3])
		diags = append(diags, LSPDiagnostic{
			File:     targetFile,
			Line:     line,
			Column:   col,
			Severity: "warning",
			Message:  m[4],
		})
	}
	return diags
}

// goModuleContext walks up to the enclosing go.mod and returns the module root
// plus the package directory relative to it ("" when the file sits at the root).
func goModuleContext(filePath string) (moduleRoot string, pkgRel string) {
	fileDir := filepath.Dir(filePath)
	for dir := fileDir; ; {
		if pathExists(filepath.Join(dir, "go.mod")) {
			rel, err := filepath.Rel(dir, fileDir)
			if err != nil || rel == "." {
				rel = ""
			}
			return dir, rel
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ""
		}
		dir = parent
	}
}

// runPythonDiagnostics parses a Python file with the real interpreter (see
// pythonSyntaxCheck). The previous implementation scanned the text for shapes
// like "multiple 'as' in a single import" and never ran a parser at all.
func runPythonDiagnostics(filePath string) ([]LSPDiagnostic, string, string, error) {
	line, message, ok := pythonSyntaxCheck(filePath)
	if !ok {
		return nil, "", "", fmt.Errorf(
			"no python interpreter found on PATH (tried python3, python, py); install Python to check .py files")
	}
	if message == "" {
		return nil, "python", "checked syntax only; use the `lint` tool (pylint/flake8) for style and lint rules", nil
	}
	if line == 0 {
		line = 1
	}
	return []LSPDiagnostic{{
		File:     filePath,
		Line:     line,
		Column:   1,
		Severity: "error",
		Message:  message,
	}}, "python", "", nil
}

// runJSDiagnostics syntax-checks a JavaScript file with `node --check`.
//
// Syntax only, deliberately. Project-wide rules (eslint) and type checking
// belong to the `lint` tool or to a .ts file, and `node --check` parses both
// CommonJS and ES modules, so it never invents the "Cannot use import statement
// outside a module" error that a naive checker produces on modern sources.
func runJSDiagnostics(filePath string) ([]LSPDiagnostic, string, string, error) {
	line, message, ok := nodeCheckSyntax(filePath)
	if !ok {
		return nil, "", "", fmt.Errorf("node not found on PATH; install Node.js to check JavaScript files")
	}
	if line == 0 && message == "" {
		return nil, "node --check", "checked syntax only; use the `lint` tool for eslint rules and `lsp_diagnostics` on a .ts file for type errors", nil
	}
	if message == "" {
		message = "syntax error"
	}
	return []LSPDiagnostic{{
			File:     filePath,
			Line:     line,
			Column:   1,
			Severity: "error",
			Message:  message,
		}}, "node --check",
		"checked syntax only; eslint rules and type errors are not covered here", nil
}
