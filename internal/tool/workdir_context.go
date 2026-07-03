package tool

import (
	"context"
	"path/filepath"
)

// workDirKey is the context key for per-session working directory.
type workDirKey struct{}

// WithWorkDir returns a new context that carries the given working directory.
// Tools that resolve relative paths should read it via WorkDirFromContext.
func WithWorkDir(ctx context.Context, workDir string) context.Context {
	if workDir == "" {
		return ctx
	}
	return context.WithValue(ctx, workDirKey{}, workDir)
}

// WorkDirFromContext returns the per-session working directory stored in ctx,
// or an empty string when none is set.
func WorkDirFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(workDirKey{}).(string); ok {
		return v
	}
	return ""
}

// resolvePath resolves a path against the working directory stored in ctx.
// If ctx carries a workDir and path is relative, the path is joined with
// workDir before being made absolute. Otherwise it behaves like filepath.Abs.
func resolvePath(ctx context.Context, path string) (string, error) {
	if workDir := WorkDirFromContext(ctx); workDir != "" && !filepath.IsAbs(path) {
		path = filepath.Join(workDir, path)
	}
	return filepath.Abs(path)
}
