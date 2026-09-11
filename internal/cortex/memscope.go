package cortex

import (
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

// 本文件负责「记忆的项目归属」判定：从记忆文本里识别绝对路径，推导出该条
// 记忆真正描述的项目 scope，并在召回时丢弃与本会话工作目录无关的项目记忆。
//
// 背景（记忆串事故）：applyMemoryScope 曾把当前会话目录 scope 无条件盖到
// 抽取出的所有非 user/preference 记忆上，而 LLM 抽取器吃的是整段会话（含
// assistant 自己的输出）。于是「助手在 A 项目会话里误述了 B 项目的路径」会
// 被当成事实抽取、盖上 A 的 scope、以高重要度固化，之后在 A 的会话里被召回
// 并再次强化——形成跨项目污染。修复分三道闸：写入按内容路径归属、抽取只取
// 事实来源（见 integration.go）、召回做路径守卫（见 RecallForInputScope）。

// winAbsPathRe 匹配 Windows 盘符绝对路径（D:\a\b 或 D:/a/b）。终止符覆盖
// 空白与中文标点，避免把整句都吞进来。前置边界（非字母数字）用于排除
// "https://host/x" 被误判成盘符路径 "s://host/x" —— 取子匹配组 1。
var winAbsPathRe = regexp.MustCompile(`(?:^|[^A-Za-z0-9])([A-Za-z]:[\\/][^\s"'` + "`" + `，。；：、（）【】<>|?*]+)`)

// posixAbsPathRe 匹配“看起来像项目目录”的 POSIX 绝对路径。只认常见根，
// 避免把 /api/sessions 这类接口路径误判为文件系统路径。
var posixAbsPathRe = regexp.MustCompile(`/(?:Users|home|opt|srv|var/www|data|mnt|workspace|workspaces|projects?|code|repos?)/[^\s"'` + "`" + `，。；：、（）【】<>|?*]+`)

// scopePathNoise 是「不应作为项目归属依据」的路径前缀（工具链/系统/临时目录）。
// 记忆里出现这些路径通常只是描述运行环境，不是项目本身。
var scopePathNoise = []string{
	"/tmp", "/usr", "/etc", "/var/folders", "/private/var", "/dev/null",
	"/node_modules/", "/.git/", "/vendor/", "/dist/", "/build/",
	"/appdata/", "/windows/", "/program files", "/programdata/",
	"/.magic/", "/.workbuddy/", "/.cache/", "/.npm/", "/.cargo/", "/go/pkg/",
}

// looksLikeWinPath 报告路径是否为 Windows 风格路径（盘符前缀或含反斜杠）。
// 归一化按路径自身形态判定而非 runtime.GOOS：记忆内容里的路径来自用户与
// 工具输出，宿主 OS 未必是 Windows（Linux CI、容器部署都会遇到 D:\ 路径），
// 若只在 GOOS==windows 时小写，同一 Windows 路径在不同宿主上会落到不同桶。
func looksLikeWinPath(p string) bool {
	if len(p) >= 2 && p[1] == ':' &&
		(p[0] >= 'a' && p[0] <= 'z' || p[0] >= 'A' && p[0] <= 'Z') {
		return true
	}
	return strings.Contains(p, `\`)
}

// normalizeScopePath 把目录归一化为记忆 scope 键。必须与 server.normalizeDirScope
// 保持一致（filepath.Clean + Windows 风格路径小写），否则读写两侧落不到同一个桶。
// 该函数是纯函数，行为可直接对齐；若 server 侧规则变更需同步此处。
func normalizeScopePath(dir string) string {
	if dir == "" {
		return ""
	}
	isWin := runtime.GOOS == "windows" || looksLikeWinPath(dir)
	p := dir
	if isWin {
		p = strings.ReplaceAll(p, "/", `\`)
	}
	p = filepath.Clean(p)
	if isWin {
		return strings.ToLower(p)
	}
	return p
}

// looksLikeFilePath 判断路径末段是否像文件名（带扩展名），据此回退到父目录，
// 防止把 D:\proj\src\a.jsx 当成一个 scope 建桶。
func looksLikeFilePath(p string) bool {
	base := filepath.Base(p)
	dot := strings.LastIndex(base, ".")
	if dot <= 0 || dot == len(base)-1 {
		return false
	}
	ext := base[dot+1:]
	if len(ext) == 0 || len(ext) > 6 {
		return false
	}
	for _, r := range ext {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// isNoiseScopePath 报告归一化后的路径是否属于工具链/系统噪音前缀。
func isNoiseScopePath(norm string) bool {
	low := strings.ToLower(strings.ReplaceAll(norm, `\`, "/"))
	if !strings.Contains(low, "/") {
		return true // 单段路径（如 "C:\"）没有项目语义
	}
	// 补尾斜杠：noise 表里的条目带两侧斜杠（如 "/.magic/"），路径以该段结尾时
	// 也要命中（C:\Users\x\.magic → c:/users/x/.magic/）。
	low += "/"
	for _, n := range scopePathNoise {
		if strings.Contains(low, n) {
			return true
		}
	}
	return false
}

// projectPathCandidates 从文本中提取候选项目路径（归一化、去噪、按层级从浅到深
// 排序）。文件路径回退到其父目录。
func projectPathCandidates(content string) []string {
	if content == "" {
		return nil
	}
	raw := make([]string, 0, 4)
	for _, m := range winAbsPathRe.FindAllStringSubmatch(content, -1) {
		if len(m) > 1 {
			raw = append(raw, m[1])
		}
	}
	raw = append(raw, posixAbsPathRe.FindAllString(content, -1)...)
	if len(raw) == 0 {
		return nil
	}

	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		// 去掉尾部标点（句号/逗号/括号等可能被正则带进来）
		r = strings.TrimRight(r, `.,;:!?)]}>，。；：、）】》"'`)
		if r == "" {
			continue
		}
		p := r
		if looksLikeFilePath(p) {
			p = filepath.Dir(p)
		}
		norm := normalizeScopePath(p)
		if norm == "" || isNoiseScopePath(norm) || seen[norm] {
			continue
		}
		seen[norm] = true
		out = append(out, norm)
	}
	// 浅路径更可能是项目根：D:\project\go\app 优先于 D:\project\go\app\web\src
	sort.SliceStable(out, func(i, j int) bool {
		return strings.Count(out[i], `\`)+strings.Count(out[i], "/") <
			strings.Count(out[j], `\`)+strings.Count(out[j], "/")
	})
	return out
}

// pathRelated 报告两个归一化路径是否指向同一项目（相等、互为子目录）。
func pathRelated(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	sep := func(p string) string {
		if strings.Contains(p, `\`) {
			return `\`
		}
		return "/"
	}
	as, bs := sep(a), sep(b)
	return strings.HasPrefix(a, b+bs) || strings.HasPrefix(b, a+as)
}

// nameNeedsContext 报告项目名是否属于「容易与普通词混淆」的类型（不含连字符/
// 下划线/数字，如 article、magic）。这类名字只有在项目语境词附近出现时才认。
func nameNeedsContext(name string) bool {
	return !strings.ContainsAny(name, "-_0123456789")
}

// contextMarkers 是判断「提到的是项目」的语境词。
var contextMarkers = []string{"项目", "工程", "仓库", "代码库", "project", "repo", "repository", "codebase"}

// otherProjectByName 在文本中查找「别的项目」的名字级引用。用于内容只写项目名
// （无绝对路径）的场景，例如「项目 go-magic-saas 的提交信息使用中文」——这类
// 记忆若不归属到具体项目，就会变成全局可见并泄漏给其他项目会话。
//
// nameToScope 由记忆库已有 scope 的目录名构建（projectNameScopeMap）。
// 命中优先级：名字更长者优先（go-magic-saas 先于 go-magic），且跳过当前项目。
// 对易混名字（无连字符/下划线/数字）要求出现处附近有项目语境词，降低误判。
func otherProjectByName(content, sessionScope string, nameToScope map[string]string) string {
	if content == "" || len(nameToScope) == 0 {
		return ""
	}
	curName := ""
	if sessionScope != "" {
		curName = strings.ToLower(filepath.Base(sessionScope))
	}
	names := make([]string, 0, len(nameToScope))
	for n := range nameToScope {
		names = append(names, n)
	}
	sort.SliceStable(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })

	low := strings.ToLower(content)
	for _, n := range names {
		if n == curName || len(n) < 4 {
			continue
		}
		idx := strings.Index(low, n)
		if idx < 0 {
			continue
		}
		if nameNeedsContext(n) && !hasProjectContext(low, idx, len(n)) {
			continue
		}
		return nameToScope[n]
	}
	return ""
}

// hasProjectContext 检查命中位置附近是否出现项目语境词。
func hasProjectContext(low string, idx, nameLen int) bool {
	runes := []rune(low)
	rIdx := len([]rune(low[:idx]))
	rStart := rIdx - 12
	if rStart < 0 {
		rStart = 0
	}
	rEnd := rIdx + nameLen + 12
	if rEnd > len(runes) {
		rEnd = len(runes)
	}
	window := string(runes[rStart:rEnd])
	for _, mk := range contextMarkers {
		if strings.Contains(window, mk) {
			return true
		}
	}
	return false
}

// projectNameScopeMap 由已知 scope 列表构建「目录名 → scope」映射。
func projectNameScopeMap(scopes []string) map[string]string {
	out := make(map[string]string, len(scopes))
	for _, sc := range scopes {
		if sc == "" {
			continue
		}
		name := strings.ToLower(filepath.Base(sc))
		if name == "" || name == "." || name == "\\" || name == "/" {
			continue
		}
		if _, dup := out[name]; !dup {
			out[name] = sc
		}
	}
	return out
}

// memoryConflictsWithScope 报告一条记忆是否描述了「别的项目」——即记忆文本里
// 出现的项目路径既不是当前 scope 目录、也不在其之下/之上；或文本点名了记忆库中
// 已知的另一个项目。用于召回守卫：命中即丢弃，避免 A 项目的会话被 B 项目的
// 记忆误导（记忆串事故的止血闸）。没有项目线索的通用记忆不受影响。
func memoryConflictsWithScope(content, scope string, nameToScope map[string]string) bool {
	if scope == "" {
		return false
	}
	cands := projectPathCandidates(content)
	if len(cands) > 0 {
		related := false
		for _, c := range cands {
			if pathRelated(c, scope) {
				related = true
				break
			}
		}
		if !related {
			return true
		}
	}
	if other := otherProjectByName(content, scope, nameToScope); other != "" && !pathRelated(other, scope) {
		return true
	}
	return false
}

// knownProjectNameScope 查询记忆库中已知的项目名 → scope 映射（best-effort）。
func (m *Manager) knownProjectNameScope() map[string]string {
	if m == nil || m.memoryStore == nil {
		return nil
	}
	scopes, err := m.memoryStore.DistinctScopes()
	if err != nil {
		return nil
	}
	return projectNameScopeMap(scopes)
}
