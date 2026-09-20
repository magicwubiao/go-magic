package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	// 会话里存下来的工作目录可能是畸形 Windows 形态（历史数据里出现过
	// "/D:/project/..."，见 internal/server/fs.go 的 normalizeFSPath）。在注入
	// 点归一化，让所有下游使用者（工具路径解析、shell 的 cwd、文件变更跟踪）
	// 拿到同一个规范形态，而不是各自处理。
	return context.WithValue(ctx, workDirKey{}, normalizeToolPath(workDir))
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

	// 归一化 Windows 路径变体（/D:/a、/d/a、引号包裹、正斜杠盘符）后再判定
	// 绝对/相对，否则 filepath.IsAbs 会把 "/D:/a/b" 当成相对路径、把
	// "d:\a\b" 当成工作目录外的路径——两者都会让"用绝对路径操作文件"失败。
	path = normalizeToolPath(path)

	if !security.Enabled {
		if workDir := normalizeToolPath(WorkDirFromContext(ctx)); workDir != "" && !filepath.IsAbs(path) {
			path = filepath.Join(workDir, path)
		}
		return filepath.Abs(path)
	}

	if path == "" {
		return "", fmt.Errorf("path cannot be empty")
	}

	// 工作目录本身也可能是畸形形态（历史会话里存过 "/D:/project/..."），
	// 归一化后再参与拼接与边界判定，避免把工作目录内的绝对路径误判成越界。
	baseWorkDir := normalizeToolPath(WorkDirFromContext(ctx))

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

	// BlockedPaths 用于禁止访问系统敏感目录(如 /etc/、/home/)。
	// 但工作目录本身是系统/用户明确指定的安全区域，即使它位于被阻止的
	// 路径下(例如 /home/www/.magic/workspace/...)也必须允许访问，
	// 否则 AI 智能体无法读写自己的会话工作目录。
	if err := checkPathBlocked(absPath, baseWorkDir, security); err != nil {
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

	if !withinDir(baseAbs, absPath) {
		// 报错信息要能让模型自己纠正：说清工作目录，并给出"改用相对路径"的
		// 具体建议。只回一句 "path escape detected" 时，模型往往原样重试同一个
		// 绝对路径，于是同一个工具调用连续失败。
		return fmt.Errorf(
			"path escape detected: path '%s' is outside working directory '%s'; "+
				"retry with a path relative to the working directory (for example \"subdir/file.txt\") or an absolute path inside it",
			absPath, baseAbs)
	}

	return nil
}

// withinDir 判定 target 是否等于 dir、或位于 dir 之下。
//
// Windows 路径大小写不敏感，因此比较在 Windows 上不区分大小写：此前这里用
// 区分大小写的 strings.HasPrefix/!= 比较，把 "d:\proj\x"（模型常写小写盘符）
// 或任何大小写不一致的写法误判为"越界"，而 os.Open/WriteFile 本身完全能处理
// 这些路径——用户看到的就是"工具用绝对路径操作文件失败"，改用相对路径却正常。
// 非 Windows 平台保持原本的严格比较。
func withinDir(dir, target string) bool {
	return pathContains(
		filepath.Clean(dir),
		filepath.Clean(target),
		string(filepath.Separator),
		runtime.GOOS == "windows",
	)
}

// pathContains 是 withinDir 的平台无关内核：分隔符与大小写策略由调用方显式给出，
// 于是"Windows 不区分大小写 / 其它平台区分大小写"两种语义在任意宿主平台上都
// 可以被直接断言（CI 跑 Linux 时也能覆盖 Windows 分支，反之亦然）。
//
// 前置条件：dir/target 已 Clean。期望值在测试里用 filepath.Join 拼装，避免
// 硬编码分隔符——`\` 在 Linux 上只是普通字符，硬编码会让测试在异构平台假失败。
func pathContains(dir, target, sep string, foldCase bool) bool {
	if foldCase {
		dir = strings.ToLower(dir)
		target = strings.ToLower(target)
	}
	if target == dir {
		return true
	}
	return strings.HasPrefix(target, dir+sep)
}

// normalizeToolPath 归一化模型/前端常见的 Windows 路径变体，使其能被
// filepath.IsAbs 正确识别为绝对路径：
//
//	"D:/a/b"    → "D:\a\b"   （正斜杠写法）
//	"/D:/a/b"   → "D:\a\b"   （浏览器 URL 处理产物，见 server.resolveFSPath）
//	"/d/a/b"    → "D:\a\b"   （Git-Bash 风格盘符路径；仅当字面路径不存在时）
//	"\"D:\a\b\"" → "D:\a\b"  （模型把路径连同引号一起传进来）
//
// 非 Windows 平台、以及非盘符形态的路径原样返回。这是工具侧与
// internal/server/fs.go 的 normalizeFSPath 对齐的修复：文件面板早就修过这个
// 问题，但 agent 工具链没有，于是"文件面板能打开、工具却打不开同一路径"。
func normalizeToolPath(p string) string {
	if p == "" {
		return p
	}
	// 去掉成对包裹的引号（模型经常把路径写成 "\"D:\\a\\b.txt\""）。
	if len(p) >= 2 {
		if (p[0] == '"' && p[len(p)-1] == '"') || (p[0] == '\'' && p[len(p)-1] == '\'') {
			p = p[1 : len(p)-1]
		}
	}
	if p == "" || runtime.GOOS != "windows" {
		return p
	}

	// "/D:/a" / "\D:\a"：多余的前导分隔符 + 盘符
	if len(p) >= 3 && (p[0] == '/' || p[0] == '\\') && p[2] == ':' {
		return filepath.FromSlash(p[1:])
	}
	if len(p) >= 2 && p[1] == ':' {
		return filepath.FromSlash(p)
	}

	// "/d/a"（Git-Bash 风格）：映射成盘符路径，但仅当目标（或其父目录）确实
	// 存在于该盘符上时才认——否则保留字面含义，避免在"工作目录下真有个 d
	// 目录"的场景里把路径悄悄指到 D:\。
	if len(p) >= 4 && p[0] == '/' && p[2] == '/' && isASCIILetter(p[1]) {
		mapped := strings.ToUpper(p[1:2]) + ":" + filepath.FromSlash(p[2:])
		if pathExists(mapped) || pathExists(filepath.Dir(mapped)) {
			return mapped
		}
	}
	return p
}

func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
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
		allowedAbs, err := filepath.Abs(normalizeToolPath(allowed))
		if err != nil {
			continue
		}
		if withinDir(allowedAbs, absPath) {
			return nil
		}
	}

	return fmt.Errorf("path '%s' is not in the allowed paths list", absPath)
}

func checkPathBlocked(absPath, baseWorkDir string, security FileSecurityConfig) error {
	if len(security.BlockedPaths) == 0 {
		return nil
	}

	absPathClean := filepath.Clean(absPath)

	// 工作目录内的路径始终允许访问(已通过 checkPathEscape 校验未越界)，
	// 即使工作目录本身位于被阻止的路径下也不例外。
	if baseWorkDir != "" {
		if baseAbs, err := filepath.Abs(baseWorkDir); err == nil && withinDir(baseAbs, absPathClean) {
			return nil
		}
	}

	for _, blocked := range security.BlockedPaths {
		if withinDir(normalizeToolPath(blocked), absPathClean) {
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
