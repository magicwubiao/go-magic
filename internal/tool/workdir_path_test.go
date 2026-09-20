package tool

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 本文件钉住"工具用绝对路径操作文件"的路径解析语义。
//
// 背景（用户报告："工具 绝对路径的文件会操作失败"）：resolvePath 此前用
// 区分大小写的 strings.HasPrefix 判定"路径是否在工作目录内"，且在识别绝对
// 路径前不做任何归一化。于是 Windows 上这些**完全合法**的写法全部失败：
//
//	"d:\proj\x"        → 误判越界（盘符小写）
//	"D:\Project\X"     → 误判越界（任一段大小写不同）
//	"/D:/proj/x"       → 被当成相对路径拼接，路径被拼坏
//	"/d/proj/x"        → 被当成相对路径拼接，指向 <workdir>\d\proj\x
//	"\"D:\proj\x\""    → 同上（模型常连同引号一起传）
//
// 而文件面板（internal/server/fs.go 的 resolveFSPath + normalizeFSPath）早已
// 修过同类问题，工具链却没有——表现为"面板里能打开的文件，让 agent 用绝对
// 路径去读却报错"。

func setupToolWorkDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return dir
}

// TestResolvePathAbsoluteVariantsResolveToSameFile 覆盖 Windows 上各种绝对路径
// 写法必须都指向工作目录内的同一个文件。
func TestResolvePathAbsoluteVariantsResolveToSameFile(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific path variants")
	}
	workdir := setupToolWorkDir(t)
	want := filepath.Join(workdir, "a.txt")

	abs := want
	slashed := filepath.ToSlash(abs) // D:/proj/a.txt
	drivePrefix := "/" + slashed     // /D:/proj/a.txt（浏览器 URL 处理产物）
	bashStyle := "/" + strings.ToLower(slashed[0:1]) + slashed[2:]

	cases := []struct {
		name string
		in   string
	}{
		{"relative", "a.txt"},
		{"absolute-backslash", abs},
		{"absolute-forward-slash", slashed},
		{"lowercase-drive", strings.ToLower(abs[0:1]) + abs[1:]},
		{"upper-case-components", strings.ToUpper(abs)},
		{"leading-slash-drive-prefix", drivePrefix},
		{"git-bash-drive-style", bashStyle},
		{"quoted-absolute", `"` + abs + `"`},
		{"single-quoted-absolute", `'` + abs + `'`},
	}

	ctx := WithWorkDir(context.Background(), workdir)
	for _, tc := range cases {
		got, err := resolvePath(ctx, tc.in)
		if err != nil {
			t.Errorf("resolvePath(%s: %q) = error %v, want %q", tc.name, tc.in, err, want)
			continue
		}
		// Windows 上大小写不敏感：解析结果保留调用方写法（c:\... / D:\USERS\...）
		// 是允许的，只要指向同一个文件即可。
		if !samePath(got, want) {
			t.Errorf("resolvePath(%s: %q) = %q, want %q", tc.name, tc.in, got, want)
			continue
		}
		if _, err := os.Stat(got); err != nil {
			t.Errorf("resolvePath(%s: %q) resolved to %q which does not exist: %v", tc.name, tc.in, got, err)
		}
	}
}

func samePath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// TestResolvePathMalformedWorkDirStillReachable 覆盖"工作目录本身是畸形形态"
// （历史会话里存过 "/D:/project/..."）：相对路径与工作目录内绝对路径都必须可用。
func TestResolvePathMalformedWorkDirStillReachable(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific")
	}
	workdir := setupToolWorkDir(t)
	malformed := "/" + filepath.ToSlash(workdir)
	want := filepath.Join(workdir, "a.txt")

	ctx := WithWorkDir(context.Background(), malformed)
	for _, in := range []string{"a.txt", want, filepath.ToSlash(want)} {
		got, err := resolvePath(ctx, in)
		if err != nil {
			t.Fatalf("resolvePath(%q) with malformed workdir = %v, want %q", in, err, want)
		}
		if filepath.Clean(got) != filepath.Clean(want) {
			t.Fatalf("resolvePath(%q) with malformed workdir = %q, want %q", in, got, want)
		}
	}
}

// TestResolvePathOutsideWorkDirStillRejected 确保修好大小写/形态问题之后，
// 真正的越界访问依旧被拒绝，且错误信息给出可自纠的提示（工作目录 + 改用相对路径）。
func TestResolvePathOutsideWorkDirStillRejected(t *testing.T) {
	workdir := setupToolWorkDir(t)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ctx := WithWorkDir(context.Background(), workdir)
	_, err := resolvePath(ctx, outside)
	if err == nil {
		t.Fatalf("resolvePath(%q) outside workdir = nil error, want rejection", outside)
	}
	msg := err.Error()
	if !strings.Contains(msg, "path escape detected") {
		t.Errorf("error should mention path escape, got: %v", err)
	}
	if !strings.Contains(msg, workdir) {
		t.Errorf("error should name the working directory %q, got: %v", workdir, err)
	}
	if !strings.Contains(msg, "relative") {
		t.Errorf("error should suggest retrying with a relative path, got: %v", err)
	}

	// 目录穿越同样必须被拒绝
	if _, err := resolvePath(ctx, filepath.Join("..", "escape.txt")); err == nil {
		t.Fatal("resolvePath(../escape.txt) = nil error, want rejection")
	}
}

// TestWithinDirContainment 钉住边界判定的语义与平台差异：同前缀的兄弟目录
// 不算"在目录内"，大小写敏感性随平台（Windows 不敏感、其它平台敏感）。
//
// 关键：Windows / 非 Windows 两种语义都在这里被断言，不靠 runtime.GOOS 分支
// "在本机跑到哪一支算哪一支"——之前的写法在 Windows 上绿、在 Linux CI 上红
// （硬编码 `\` 在 Linux 不是分隔符），属于测试自身的缺陷。
func TestWithinDirContainment(t *testing.T) {
	// 平台无关内核：显式给定分隔符与大小写策略。
	winSep, nixSep := `\`, "/"
	// Windows：不区分大小写，且必须认反斜杠。
	if !pathContains(`C:\Proj\WS`, `c:\proj\ws\sub\a.txt`, winSep, true) {
		t.Error("Windows containment must ignore case (mixed-case base, lowercase target)")
	}
	if !pathContains(`C:\proj\ws`, `C:\proj\ws`, winSep, true) {
		t.Error("a directory must contain itself")
	}
	if pathContains(`C:\proj\ws`, `C:\proj\ws-evil\a.txt`, winSep, true) {
		t.Error("prefix-matching sibling must not count as inside (\\ws-evil vs \\ws)")
	}
	// 非 Windows：区分大小写，用正斜杠。
	if !pathContains("/proj/ws", "/proj/ws/sub/a.txt", nixSep, false) {
		t.Error("non-Windows containment must accept a plain child path")
	}
	if pathContains("/proj/ws", "/PROJ/WS/a.txt", nixSep, false) {
		t.Error("non-Windows containment must stay case-sensitive")
	}
	if pathContains("/proj/ws", "/proj/ws-evil/a.txt", nixSep, false) {
		t.Error("prefix-matching sibling must not count as inside (/ws-evil vs /ws)")
	}

	// 接入层：确认 withinDir 确实按当前平台选对了语义，且分隔符不写死。
	base := filepath.Join("proj", "ws") // proj\ws 或 proj/ws
	target := filepath.Join(base, "sub", "a.txt")
	sibling := filepath.Join("proj", "ws-evil", "a.txt")
	if !withinDir(base, target) {
		t.Errorf("withinDir(%q, %q) = false, want true", base, target)
	}
	if withinDir(base, sibling) {
		t.Errorf("withinDir(%q, %q) = true, want false", base, sibling)
	}
	if runtime.GOOS == "windows" {
		if !withinDir(`D:\proj\ws`, `d:\PROJ\WS\sub\a.txt`) {
			t.Error("Windows withinDir must ignore case")
		}
		return
	}
	if withinDir("/proj/ws", "/PROJ/WS/a.txt") {
		t.Error("non-Windows withinDir must stay case-sensitive")
	}
}
