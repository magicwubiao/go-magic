package context

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// 静态规则文件链加载器。
//
// 业界主流智能体（Claude Code / Codex / Cursor / Gemini CLI）的共识是：
// 把「多会话共享的规范/约定」写进放在项目里的规则文件（AGENTS.md 等），
// 会话启动时从工作目录**向上逐级发现**并注入 system，越靠近工作目录的
// 规则优先级越高（后注入者覆盖先注入者）。本文件实现这一「规则链」语义，
// 供 server 侧把会话 work_dir 绑到 agent 的 ruleDir 后使用。
//
// 与既有 Manager（glob 单层加载 + 破坏性 markdown 清洗）不同：规则文件
// 内容按原文注入（代码块里的示例/配置同样重要，不做 processMarkdown 剥除）。

// DefaultRuleNames 向上逐级查找的规则文件名（业界标准族）。
var DefaultRuleNames = []string{"AGENTS.md", "CLAUDE.md", "CONTEXT.md"}

// DefaultMaxRuleChars 规则注入文本的默认总字符上限。超出时优先丢弃远端
// （更通用）文件，保住近端（更具体、优先）文件。
const DefaultMaxRuleChars = 16000

// vcsMarkers 判定「仓库根」的目录标记。命中的目录自身规则文件仍收入，
// 但其再向上的目录不再纳入（AGENTS.md 标准的 git-root 边界语义）。
var vcsMarkers = []string{".git", ".hg", ".svn", ".bzr"}

// RuleFile 表示规则链上的一条规则文件。
type RuleFile struct {
	Dir     string // 所在目录（绝对路径）
	Path    string // 文件绝对路径
	Name    string // 文件名（如 AGENTS.md）
	Depth   int    // 距工作目录的层级：0 = 工作目录自身
	Content string // 原文内容（去 BOM 与首尾空白，代码块保留）
}

// chainDirs 返回从 startDir 向上到边界（VCS 根或文件系统根）的目录链，
// 顺序为「远 → 近」（边界目录在最前，startDir 在最后）。startDir 存在
// VCS 标记时链上只有它自己（不越过仓库根向上收）。
func chainDirs(startDir string) []string {
	start := filepath.Clean(startDir)
	var farToNear []string
	d := start
	for {
		farToNear = append(farToNear, d)
		if hasVCSMarker(d) {
			break
		}
		parent := filepath.Dir(d)
		if parent == d { // 到达文件系统根
			break
		}
		d = parent
	}
	// farToNear 目前是 near→far，反转成 far→near
	dirs := make([]string, 0, len(farToNear))
	for i := len(farToNear) - 1; i >= 0; i-- {
		dirs = append(dirs, farToNear[i])
	}
	return dirs
}

// hasVCSMarker 报告目录下是否存在 VCS 根标记（.git 等）。
func hasVCSMarker(dir string) bool {
	for _, marker := range vcsMarkers {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return true
		}
	}
	return false
}

// loadRuleFile 读取单个规则文件并做最小清洗（去 BOM、去首尾空白）。
// 不做 markdown 结构性清洗——规则文件中的代码块是有意保留的内容。
func loadRuleFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	content := strings.TrimPrefix(string(data), "\uFEFF")
	return strings.TrimSpace(content), nil
}

// LoadRuleChain 从 startDir 向上逐级收集规则文件。返回顺序远 → 近
// （最后一个元素 = 离工作目录最近的规则，优先级最高）。目录内按
// names 给定顺序。startDir 不存在或链上无任何规则文件时返回空切片。
func LoadRuleChain(startDir string, names []string) []RuleFile {
	if len(names) == 0 {
		names = DefaultRuleNames
	}
	dirs := chainDirs(startDir)
	depth := len(dirs) - 1 // 最后一个元素 = startDir
	var files []RuleFile
	seen := make(map[string]bool)
	for i, dir := range dirs {
		for _, name := range names {
			p := filepath.Join(dir, name)
			if seen[p] {
				continue
			}
			content, err := loadRuleFile(p)
			if err != nil || content == "" {
				continue
			}
			seen[p] = true
			files = append(files, RuleFile{
				Dir:     dir,
				Path:    p,
				Name:    name,
				Depth:   depth - i,
				Content: content,
			})
		}
	}
	return files
}

// RuleChainSignature 返回规则链的 stat 签名（路径 + size + mtime 聚合）。
// 调用方可据此做缓存失效判断：文件未变时签名稳定，新增/修改/删除任一
// 规则文件都会改变签名，从而触发重新加载。签名与 LoadRuleChain 的
// 边界/名称规则完全一致。
func RuleChainSignature(startDir string, names []string) string {
	if len(names) == 0 {
		names = DefaultRuleNames
	}
	var sb strings.Builder
	for _, dir := range chainDirs(startDir) {
		for _, name := range names {
			p := filepath.Join(dir, name)
			info, err := os.Stat(p)
			if err != nil {
				continue
			}
			fmt.Fprintf(&sb, "%s|%d|%d;", p, info.Size(), info.ModTime().UnixNano())
		}
	}
	return sb.String()
}

// FormatRuleContext 把规则链拼成注入 system 的文本。files 需为
// LoadRuleChain 的输出（远 → 近）。maxChars <= 0 时用默认上限；
// 超限时先整体丢弃最远端文件，仍超则按远端优先逐文件截断，末尾补标记。
// 无规则文件时返回空串（调用方直接跳过注入）。
func FormatRuleContext(files []RuleFile, maxChars int) string {
	if len(files) == 0 {
		return ""
	}
	if maxChars <= 0 {
		maxChars = DefaultMaxRuleChars
	}

	var sb strings.Builder
	sb.WriteString("# Repository rules\n\n")
	sb.WriteString("The rules below were discovered from the project's rule files ")
	sb.WriteString("(AGENTS.md / CLAUDE.md / CONTEXT.md), searching upward from the ")
	sb.WriteString("working directory. When instructions conflict, the file closest ")
	sb.WriteString("to the working directory wins.\n")

	// 超限裁剪：近端（= files 末尾）优先保留。
	kept := make([]RuleFile, 0, len(files))
	acc := sb.Len()
	for i := len(files) - 1; i >= 0; i-- {
		f := files[i]
		overhead := len(fmt.Sprintf("\n\n### From %s (%s)\n\n", f.Dir, f.Name))
		if acc+overhead+len(f.Content) > maxChars && len(kept) > 0 {
			break // 近端已保底，丢弃更远的
		}
		kept = append(kept, f)
		acc += overhead + len(f.Content)
	}
	// kept 现在是近 → 远，转回远 → 近输出
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}

	// 输出并做字符级兜底截断（保近端）。
	var out strings.Builder
	out.WriteString(sb.String())
	charsLeft := maxChars - sb.Len()
	for _, f := range kept {
		header := fmt.Sprintf("\n\n### From %s (%s)\n\n", f.Dir, f.Name)
		content := f.Content
		if len(content) > charsLeft {
			content = truncateByRunes(content, charsLeft) + "\n...(rule truncated)"
			out.WriteString(header)
			out.WriteString(content)
			break
		}
		out.WriteString(header)
		out.WriteString(content)
		charsLeft -= len(header) + len(content)
	}
	return out.String()
}

// truncateByRunes 按字符（rune）安全截断，避免把多字节 UTF-8 切坏。
func truncateByRunes(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// SortRulesFarToNear 把规则文件排成远 → 近（供测试与外部复用）。
// LoadRuleChain 本身已有序，此函数供调用方在自定义场景下归一化。
func SortRulesFarToNear(files []RuleFile) {
	sort.SliceStable(files, func(i, j int) bool {
		if files[i].Depth != files[j].Depth {
			return files[i].Depth > files[j].Depth
		}
		return files[i].Path < files[j].Path
	})
}
