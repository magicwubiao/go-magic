package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile 创建父目录并写文件。
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// namesOf 返回规则文件的 Name 列表（用于断言顺序）。
func namesOf(files []RuleFile) []string {
	var names []string
	for _, f := range files {
		names = append(names, filepath.Base(f.Path))
	}
	return names
}

// dirsOfRel 返回规则文件相对 workRoot 的目录（用于断言发现层级）。
func dirsOfRel(files []RuleFile, workRoot string) []string {
	var dirs []string
	for _, f := range files {
		rel, err := filepath.Rel(workRoot, f.Dir)
		if err != nil {
			rel = f.Dir
		}
		dirs = append(dirs, filepath.ToSlash(rel))
	}
	return dirs
}

func join(a []string) string { return strings.Join(a, ",") }

// TestLoadRuleChainOrderGitBoundary 验证：向上发现 + 远→近顺序 +
// .git 根边界（边界目录自身规则收入，其父目录不再向上收）。
func TestLoadRuleChainOrderGitBoundary(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "AGENTS.md"), "root agents rules")
	writeFile(t, filepath.Join(root, "pkg", "CLAUDE.md"), "pkg claude rules")
	writeFile(t, filepath.Join(root, "pkg", "sub", "AGENTS.md"), "sub agents rules")
	// .git 标记放在 root/pkg：边界 = pkg，pkg 以上不收
	if err := os.MkdirAll(filepath.Join(root, "pkg", ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	workDir := filepath.Join(root, "pkg", "sub")
	files := LoadRuleChain(workDir, DefaultRuleNames)
	if len(files) != 2 {
		t.Fatalf("expected 2 rule files (pkg boundary + sub), got %d: %v", len(files), dirsOfRel(files, root))
	}
	// 远→近：先 pkg/CLAUDE.md 后 sub/AGENTS.md；root 的 AGENTS.md 因越界不收
	got := join(namesOf(files))
	want := join([]string{"CLAUDE.md", "AGENTS.md"})
	if got != want {
		t.Fatalf("order mismatch: got %s want %s", got, want)
	}
	dirs := dirsOfRel(files, root)
	if dirs[0] != "pkg" || dirs[1] != "pkg/sub" {
		t.Fatalf("unexpected dirs: %v", dirs)
	}
	// 逐条断言都在 pkg 边界内
	for _, f := range files {
		rel, err := filepath.Rel(root, f.Path)
		if err != nil || strings.HasPrefix(rel, "..") || !strings.HasPrefix(filepath.ToSlash(rel), "pkg/") {
			t.Fatalf("rule file escaped git boundary: %s", f.Path)
		}
	}
}

// TestLoadRuleChainRawContent 验证：内容原样保留（含代码块与 markdown
// 结构），不做破坏性清洗；AGENTS/CLAUDE 优先级序（同名目录多文件共存）。
func TestLoadRuleChainRawContent(t *testing.T) {
	root := t.TempDir()
	md := "# Team rules\n\n- always run tests\n\n```sh\nmake test\n```\n\n> quote stays"
	writeFile(t, filepath.Join(root, "AGENTS.md"), md)
	writeFile(t, filepath.Join(root, "CLAUDE.md"), "claude extra")

	files := LoadRuleChain(root, DefaultRuleNames)
	if len(files) != 2 {
		t.Fatalf("expected both rule files in same dir, got %d", len(files))
	}
	if files[0].Name != "AGENTS.md" {
		t.Errorf("AGENTS.md should come first within a dir, got %s", files[0].Name)
	}
	if !strings.Contains(files[0].Content, "```sh\nmake test\n```") {
		t.Errorf("code block must be preserved verbatim, got:\n%s", files[0].Content)
	}
	if !strings.Contains(files[0].Content, "# Team rules") {
		t.Errorf("markdown headings must be preserved")
	}
}

// TestLoadRuleChainEmptyAndMissingDir 验证：目录不存在 / 无规则文件 → 空。
func TestLoadRuleChainEmptyAndMissingDir(t *testing.T) {
	root := t.TempDir()
	if files := LoadRuleChain(root, DefaultRuleNames); len(files) != 0 {
		t.Fatalf("empty dir should yield no rules, got %v", namesOf(files))
	}
	if files := LoadRuleChain(filepath.Join(root, "nope", "deeper"), DefaultRuleNames); len(files) != 0 {
		t.Fatalf("missing dir should yield no rules, got %v", namesOf(files))
	}
}

// TestRuleChainSignatureChanges 验证：签名在文件修改后变化、未改时稳定。
func TestRuleChainSignatureChanges(t *testing.T) {
	root := t.TempDir()
	agents := filepath.Join(root, "AGENTS.md")
	writeFile(t, agents, "v1 rules")

	sig1 := RuleChainSignature(root, DefaultRuleNames)
	if sig1 == "" {
		t.Fatal("signature should not be empty with a rule file present")
	}
	if sig2 := RuleChainSignature(root, DefaultRuleNames); sig2 != sig1 {
		t.Fatal("signature must be stable while files unchanged")
	}
	writeFile(t, agents, "v2 rules with more words to change size")
	if sig3 := RuleChainSignature(root, DefaultRuleNames); sig3 == sig1 {
		t.Fatal("signature must change when a rule file is modified")
	}
	// 删除文件 → 签名归空
	if err := os.Remove(agents); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if sig4 := RuleChainSignature(root, DefaultRuleNames); sig4 != "" {
		t.Fatalf("signature should be empty after removing the only rule file, got %q", sig4)
	}
}

// TestFormatRuleContextOrderAndTruncation 验证：注入文本含就近优先声明、
// 文件按远→近出现；超限时丢远端保近端。
func TestFormatRuleContextOrderAndTruncation(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "AGENTS.md"), "far content "+strings.Repeat("x", 4000))
	writeFile(t, filepath.Join(root, "sub", "AGENTS.md"), "near content "+strings.Repeat("y", 4000))

	files := LoadRuleChain(filepath.Join(root, "sub"), DefaultRuleNames)
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	// 截断到仅能容纳一条
	out := FormatRuleContext(files, 600)
	if !strings.Contains(out, "near content") {
		t.Errorf("nearest rule must survive truncation")
	}
	if strings.Contains(out, "far content") {
		t.Errorf("farthest rule must be dropped first under cap")
	}
	// 默认上限：两条都在且顺序 远→近
	outFull := FormatRuleContext(files, 0)
	if !strings.Contains(outFull, "far content") || !strings.Contains(outFull, "near content") {
		t.Errorf("both rules expected under default cap")
	}
	idxFar := strings.Index(outFull, "far content")
	idxNear := strings.Index(outFull, "near content")
	if idxFar < 0 || idxNear < 0 || idxFar > idxNear {
		t.Errorf("output order must be far-then-near, far=%d near=%d", idxFar, idxNear)
	}
	if !strings.Contains(outFull, "closest") {
		t.Errorf("precedence note expected in output")
	}
}

// TestFormatRuleContextEmpty 验证：无规则时输出空串。
func TestFormatRuleContextEmpty(t *testing.T) {
	if out := FormatRuleContext(nil, 0); out != "" {
		t.Fatalf("nil files should render empty, got %q", out)
	}
}

// TestLoadRuleChainNoVCSWalksToFsRoot 验证：无 VCS 标记时一路收到文件系统根，
// 但不把无关目录里不存在的文件算进来；父目录规则会被收入。
func TestLoadRuleChainNoVCSWalksToFsRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "AGENTS.md"), "parent rules")
	writeFile(t, filepath.Join(root, "a", "b", "CLAUDE.md"), "deep rules")

	files := LoadRuleChain(filepath.Join(root, "a", "b"), DefaultRuleNames)
	if len(files) != 2 {
		t.Fatalf("expected parent + deep rules, got %d: %v", len(files), namesOf(files))
	}
	// 远→近：父目录 AGENTS.md 在前，深目录 CLAUDE.md 在后
	if files[0].Name != "AGENTS.md" || files[1].Name != "CLAUDE.md" {
		t.Fatalf("wrong order: %v", namesOf(files))
	}
	// 全部位于测试根内（不会把 /tmp 等更高层真实 AGENTS.md 误收进断言路径之外）
	for _, f := range files {
		rel, err := filepath.Rel(root, f.Path)
		if err != nil || strings.HasPrefix(rel, "..") {
			t.Fatalf("rule file outside test root: %s", f.Path)
		}
	}
}
