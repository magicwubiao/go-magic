package acp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// policy.go —— 客户端侧的两道闸：给不给权限、能碰哪些文件。
//
// ACP 的信任模型是"agent 是不可信的外部进程"（规范 security 一节点明）。
// agent 会请求授权、会要求读写文件，客户端必须自己判定，不能因为对端
// 说了句 "read_file" 就放行。

// PermissionRequest 是一次授权请求（去掉协议字段后的视图）。
type PermissionRequest struct {
	SessionID string
	ToolCall  ToolCall
	Options   []PermissionOption
}

// PermissionFunc 决定如何回答 session/request_permission。
//
// 实现必须返回 Options 里**真实存在**的 optionId：返回一个自造的值，
// 严格实现会拒绝，宽松实现会当成未知结果——规范明确要求"不认识的结果
// 不得视为批准"。
type PermissionFunc func(ctx context.Context, req PermissionRequest) PermissionOutcome

// DefaultAllowedKinds 是默认放行的工具种类：只读与检索类。
// 写入/执行/删除/移动一律要显式配置才放行——这是刻意的保守默认值。
var DefaultAllowedKinds = []string{
	ToolKindRead,
	ToolKindSearch,
	ToolKindThink,
	ToolKindFetch,
}

// AllowKinds 构造一个按 ToolKind 白名单放行的策略。
//
// 选择顺序：优先 allow_once（最小授权），没有再退 allow_always；
// 拒绝同理优先 reject_once。找不到匹配选项时返回 cancelled——规范里
// cancelled 表示"这轮没做出决定"，比伪造一个拒绝更诚实。
func AllowKinds(kinds ...string) PermissionFunc {
	allowed := make(map[string]struct{}, len(kinds))
	for _, k := range kinds {
		allowed[strings.TrimSpace(k)] = struct{}{}
	}

	return func(ctx context.Context, req PermissionRequest) PermissionOutcome {
		kind := ""
		if req.ToolCall.Kind != nil {
			kind = *req.ToolCall.Kind
		}

		if _, ok := allowed[kind]; ok {
			if opt, found := pickOption(req.Options, PermissionAllowOnce, PermissionAllowAlways); found {
				return PermissionOutcome{Outcome: OutcomeSelected, OptionID: opt}
			}
		}
		if opt, found := pickOption(req.Options, PermissionRejectOnce, PermissionRejectAlways); found {
			return PermissionOutcome{Outcome: OutcomeSelected, OptionID: opt}
		}
		return PermissionOutcome{Outcome: OutcomeCancelled}
	}
}

// DenyAllPermissions 永远拒绝（没有拒绝选项时回 cancelled）。
func DenyAllPermissions() PermissionFunc {
	return func(ctx context.Context, req PermissionRequest) PermissionOutcome {
		if opt, found := pickOption(req.Options, PermissionRejectOnce, PermissionRejectAlways); found {
			return PermissionOutcome{Outcome: OutcomeSelected, OptionID: opt}
		}
		return PermissionOutcome{Outcome: OutcomeCancelled}
	}
}

func pickOption(options []PermissionOption, preferred ...string) (string, bool) {
	for _, want := range preferred {
		for _, opt := range options {
			if opt.Kind == want && opt.OptionID != "" {
				return opt.OptionID, true
			}
		}
	}
	return "", false
}

// RootSet 是会话允许访问的目录集合（cwd + additionalDirectories）。
//
// 规范：所有路径必须是绝对路径，客户端应在**每次使用时**校验是否落在
// root set 内；符号链接要按解析后的真实路径判定，否则 agent 建一个指向
// /etc 的软链就把边界绕过去了。
type RootSet struct {
	roots []string
}

// NewRootSet 构造 root set。cwd 必须存在且为绝对路径。
func NewRootSet(cwd string, extra []string) (*RootSet, error) {
	if cwd == "" {
		return nil, fmt.Errorf("acp: session cwd is empty")
	}
	if !filepath.IsAbs(cwd) {
		return nil, fmt.Errorf("acp: session cwd must be absolute: %s", cwd)
	}

	resolved, err := resolveExisting(cwd)
	if err != nil {
		return nil, fmt.Errorf("acp: resolve cwd %s: %w", cwd, err)
	}

	set := &RootSet{roots: []string{resolved}}
	for _, dir := range extra {
		if dir == "" {
			continue
		}
		if !filepath.IsAbs(dir) {
			return nil, fmt.Errorf("acp: additional directory must be absolute: %s", dir)
		}
		r, err := resolveExisting(dir)
		if err != nil {
			return nil, fmt.Errorf("acp: resolve %s: %w", dir, err)
		}
		set.roots = append(set.roots, r)
	}
	return set, nil
}

// Roots 返回只读副本（诊断/日志用）。
func (r *RootSet) Roots() []string {
	if r == nil {
		return nil
	}
	out := make([]string, len(r.roots))
	copy(out, r.roots)
	return out
}

// Contains 判断路径是否落在 root set 内（按真实路径判定）。
func (r *RootSet) Contains(path string) bool {
	if r == nil {
		return false
	}
	real, err := resolveExisting(path)
	if err != nil {
		return false
	}
	for _, root := range r.roots {
		rel, err := filepath.Rel(root, real)
		if err != nil {
			continue // 不同卷（Windows）直接判不包含
		}
		if rel == "." {
			return true
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if !filepath.IsAbs(rel) {
			return true
		}
	}
	return false
}

// resolveExisting 解析符号链接。目标可能尚未创建（写文件），
// 因此对不存在的部分退回"解析最近的存在祖先 + 拼回剩余段"。
func resolveExisting(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)

	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}

	// 逐级往上找存在的祖先。
	rest := ""
	cur := abs
	for {
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs, nil
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(resolved, rest), nil
		}
	}
}

// maxReadBytes 是单次 fs/read_text_file 回给 agent 的上限（8MB）。
// agent 要读多大的文件我们管不了，但客户端不能无条件把整个文件塞进内存。
const maxReadBytes = 8 << 20

// ReadText 实现 fs/read_text_file（line 1-based，limit 为行数）。
func (r *RootSet) ReadText(p ReadTextFileParams) (ReadTextFileResult, error) {
	if !filepath.IsAbs(p.Path) {
		return ReadTextFileResult{}, fmt.Errorf("path must be absolute: %s", p.Path)
	}
	if !r.Contains(p.Path) {
		return ReadTextFileResult{}, fmt.Errorf("path outside session roots: %s", p.Path)
	}

	info, err := os.Stat(p.Path)
	if err != nil {
		return ReadTextFileResult{}, err
	}
	if info.IsDir() {
		return ReadTextFileResult{}, fmt.Errorf("path is a directory: %s", p.Path)
	}
	if info.Size() > maxReadBytes {
		return ReadTextFileResult{}, fmt.Errorf("file too large (%d bytes, limit %d)", info.Size(), maxReadBytes)
	}

	data, err := os.ReadFile(p.Path)
	if err != nil {
		return ReadTextFileResult{}, err
	}

	content := string(data)
	if p.Line == nil && p.Limit == nil {
		return ReadTextFileResult{Content: content}, nil
	}

	lines := strings.Split(content, "\n")
	start := 1
	if p.Line != nil && *p.Line > 1 {
		start = *p.Line
	}
	if start > len(lines) {
		return ReadTextFileResult{Content: ""}, nil
	}
	end := len(lines)
	if p.Limit != nil && *p.Limit >= 0 && start-1+*p.Limit < end {
		end = start - 1 + *p.Limit
	}
	if end < start {
		return ReadTextFileResult{Content: ""}, nil
	}
	return ReadTextFileResult{Content: strings.Join(lines[start-1:end], "\n")}, nil
}

// WriteText 实现 fs/write_text_file。
//
// 不创建父目录：agent 传一个不存在的深层路径时，静默 MkdirAll 等于让它
// 在会话目录之外盖出一棵树；父目录不存在就让它失败，由 agent 自己的
// 创建目录工具处理（那属于 execute/edit，仍受权限策略约束）。
func (r *RootSet) WriteText(p WriteTextFileParams) error {
	if !filepath.IsAbs(p.Path) {
		return fmt.Errorf("path must be absolute: %s", p.Path)
	}
	if !r.Contains(p.Path) {
		return fmt.Errorf("path outside session roots: %s", p.Path)
	}
	if info, err := os.Stat(p.Path); err == nil && info.IsDir() {
		return fmt.Errorf("path is a directory: %s", p.Path)
	}
	return os.WriteFile(p.Path, []byte(p.Content), 0o644)
}
