package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type workDirKey struct{}
type sessionIDKey struct{}
type fileSecurityKey struct{}
type workDirUserSetKey struct{}

type FileSecurityConfig struct {
	Enabled          bool
	AllowedPaths     []string
	BlockedPaths     []string
	SessionIsolation bool
	DefaultFileMode  os.FileMode
	DefaultDirMode   os.FileMode
	MaxFileSizeKB    int
	AllowSymlinks    bool
}

func WithWorkDir(ctx context.Context, workDir string) context.Context {
	if workDir == "" {
		return ctx
	}
	return context.WithValue(ctx, workDirKey{}, workDir)
}

// WithWorkDirUserSet marks whether the working directory was explicitly
// chosen by the user. When true, session isolation is skipped so that file
// and git operations run directly in the user-selected directory instead of
// a nested <session_id> subdirectory.
func WithWorkDirUserSet(ctx context.Context, userSet bool) context.Context {
	if !userSet {
		return ctx
	}
	return context.WithValue(ctx, workDirUserSetKey{}, true)
}

// WorkDirUserSetFromContext reports whether the working directory was set by
// the user. Absent context (CLI, gateway sessions) returns false.
func WorkDirUserSetFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	if v, ok := ctx.Value(workDirUserSetKey{}).(bool); ok {
		return v
	}
	return false
}

func WorkDirFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(workDirKey{}).(string); ok {
		return v
	}
	return ""
}

func WithSessionID(ctx context.Context, sessionID string) context.Context {
	if sessionID == "" {
		return ctx
	}
	return context.WithValue(ctx, sessionIDKey{}, sessionID)
}

func SessionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(sessionIDKey{}).(string); ok {
		return v
	}
	return ""
}

func WithFileSecurity(ctx context.Context, config FileSecurityConfig) context.Context {
	return context.WithValue(ctx, fileSecurityKey{}, config)
}

func FileSecurityFromContext(ctx context.Context) FileSecurityConfig {
	if ctx == nil {
		return defaultFileSecurity()
	}
	if v, ok := ctx.Value(fileSecurityKey{}).(FileSecurityConfig); ok {
		return v
	}
	return defaultFileSecurity()
}

func defaultFileSecurity() FileSecurityConfig {
	return FileSecurityConfig{
		Enabled:          true,
		AllowedPaths:     []string{},
		BlockedPaths:     []string{"/etc/", "/usr/", "/var/", "/root/", "/home/"},
		SessionIsolation: true,
		DefaultFileMode:  0600,
		DefaultDirMode:   0700,
		MaxFileSizeKB:    10240,
		AllowSymlinks:    false,
	}
}

func resolvePath(ctx context.Context, path string) (string, error) {
	security := FileSecurityFromContext(ctx)

	if !security.Enabled {
		if workDir := WorkDirFromContext(ctx); workDir != "" && !filepath.IsAbs(path) {
			path = filepath.Join(workDir, path)
		}
		return filepath.Abs(path)
	}

	if path == "" {
		return "", fmt.Errorf("path cannot be empty")
	}

	baseWorkDir := WorkDirFromContext(ctx)

	// Note: Session-level directory isolation is already handled upstream:
	//   - If the user selected a directory explicitly, it is used as-is.
	//   - Otherwise getSessionWorkDir produces a per-session "<name>-<shortId>"
	//     directory under the configured WorkingDir.
	// Therefore we no longer nest an additional <session_id> subdirectory here.

	if !filepath.IsAbs(path) {
		if baseWorkDir != "" {
			path = filepath.Join(baseWorkDir, path)
		}
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path: %w", err)
	}

	if err := checkPathEscape(absPath, baseWorkDir); err != nil {
		return "", err
	}

	if !security.AllowSymlinks {
		if err := checkSymlink(absPath); err != nil {
			return "", err
		}
	}

	if err := checkPathAllowed(absPath, security); err != nil {
		return "", err
	}

	if err := checkPathBlocked(absPath, security); err != nil {
		return "", err
	}

	return absPath, nil
}

func sanitizeSessionID(sessionID string) string {
	result := strings.Builder{}
	for _, c := range sessionID {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			result.WriteRune(c)
		}
	}
	return result.String()
}

func checkPathEscape(absPath, baseDir string) error {
	if baseDir == "" {
		return nil
	}

	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return fmt.Errorf("failed to resolve base directory: %w", err)
	}

	absPath = filepath.Clean(absPath)
	baseAbs = filepath.Clean(baseAbs)

	if !strings.HasPrefix(absPath, baseAbs+string(filepath.Separator)) && absPath != baseAbs {
		return fmt.Errorf("path escape detected: path '%s' is outside working directory '%s'", absPath, baseAbs)
	}

	return nil
}

func checkSymlink(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			dir := filepath.Dir(path)
			return checkSymlink(dir)
		}
		return fmt.Errorf("failed to check symlink: %w", err)
	}

	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symlinks are not allowed: %s", path)
	}

	return nil
}

func checkPathAllowed(absPath string, security FileSecurityConfig) error {
	if len(security.AllowedPaths) == 0 {
		return nil
	}

	for _, allowed := range security.AllowedPaths {
		allowedAbs, err := filepath.Abs(allowed)
		if err != nil {
			continue
		}
		allowedAbs = filepath.Clean(allowedAbs)
		absPathClean := filepath.Clean(absPath)

		if absPathClean == allowedAbs || strings.HasPrefix(absPathClean, allowedAbs+string(filepath.Separator)) {
			return nil
		}
	}

	return fmt.Errorf("path '%s' is not in the allowed paths list", absPath)
}

func checkPathBlocked(absPath string, security FileSecurityConfig) error {
	if len(security.BlockedPaths) == 0 {
		return nil
	}

	absPathClean := filepath.Clean(absPath)

	for _, blocked := range security.BlockedPaths {
		blocked = filepath.Clean(blocked)

		if absPathClean == blocked || strings.HasPrefix(absPathClean, blocked+string(filepath.Separator)) {
			return fmt.Errorf("path '%s' is blocked", absPath)
		}
	}

	return nil
}

func ParseFileMode(modeStr string, defaultMode os.FileMode) os.FileMode {
	if modeStr == "" {
		return defaultMode
	}

	mode, err := strconv.ParseUint(modeStr, 8, 32)
	if err != nil {
		return defaultMode
	}

	return os.FileMode(mode)
}

func EnsureSessionDir(ctx context.Context) (string, error) {
	baseWorkDir := WorkDirFromContext(ctx)
	sessionID := SessionIDFromContext(ctx)

	if baseWorkDir == "" || sessionID == "" {
		return baseWorkDir, nil
	}

	// Note: session work directory creation is handled upstream: either the user
	// explicitly selected a dir, or getSessionWorkDir created a per-session
	// "<name>-<shortId>" directory. Either way we just need to make sure the
	// base directory exists without nesting an additional <session_id>
	// subdirectory.
	if err := os.MkdirAll(baseWorkDir, 0700); err != nil {
		return "", fmt.Errorf("failed to ensure work directory: %w", err)
	}

	return baseWorkDir, nil
}
