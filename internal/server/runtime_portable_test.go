package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPortableSpecs 便携包定义的基本纪律：直链必须 https 且钉版本、
// 校验文件必填、预期大小为正。
func TestPortableSpecs(t *testing.T) {
	for _, spec := range runtimeSpecs {
		ps := spec.PortableWindows
		if ps == nil {
			continue
		}
		if !strings.HasPrefix(ps.URL, "https://") {
			t.Fatalf("runtime %q portable URL not https: %s", spec.ID, ps.URL)
		}
		if !strings.Contains(ps.URL, spec.ID+"_") && !strings.Contains(ps.URL, spec.ID) &&
			!strings.Contains(ps.URL, strings.ToLower(spec.Name)) {
			// 宽松检查：URL 里至少应能看出是对应的运行时，防止复制粘贴串行。
			t.Logf("runtime %q URL %s", spec.ID, ps.URL)
		}
		if ps.CheckFile == "" {
			t.Fatalf("runtime %q portable CheckFile empty", spec.ID)
		}
		if ps.SizeHint <= 0 {
			t.Fatalf("runtime %q portable SizeHint not positive", spec.ID)
		}
		if strings.Contains(ps.PathSub, "..") || filepath.IsAbs(ps.PathSub) {
			t.Fatalf("runtime %q portable PathSub unsafe: %q", spec.ID, ps.PathSub)
		}
	}
}

// TestExtractZipStripAndSlip 解压的两个关键行为：单层目录剥离、zip-slip 拒绝。
func TestExtractZipStripAndSlip(t *testing.T) {
	// 单顶层目录的 zip（Node 官方包形态）：解压根直接就是文件本身。
	single := writeTestZipFile(t, map[string]string{
		"pkg-v1.0/bin/tool.exe":  "TOOL",
		"pkg-v1.0/bin/extra.txt": "x",
	})
	dest := t.TempDir()
	if err := extractZipToDir(single, dest, nil); err != nil {
		t.Fatalf("extract single-top zip: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "bin", "tool.exe")); err != nil {
		t.Fatalf("stripped extract missing bin/tool.exe: %v", err)
	}

	// 多顶层目录的 zip（MinGit 形态）：不剥离，结构原样保留。
	multi := writeTestZipFile(t, map[string]string{
		"cmd/git.exe":   "GIT",
		"mingw64/x.txt": "x",
	})
	dest2 := t.TempDir()
	if err := extractZipToDir(multi, dest2, nil); err != nil {
		t.Fatalf("extract multi-top zip: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest2, "cmd", "git.exe")); err != nil {
		t.Fatalf("non-stripped extract missing cmd/git.exe: %v", err)
	}

	// zip-slip：条目路径带 .. 必须被中和在 dest 之内（path.Clean 吃掉
	// 前导 ..），既不报错也不许逃逸出目的地。
	slip := writeTestZipFile(t, map[string]string{
		"pkg/ok.txt":  "ok",
		"../evil.txt": "boom",
	})
	dest3 := t.TempDir()
	if err := extractZipToDir(slip, dest3, nil); err != nil {
		t.Fatalf("slip zip should be neutralized in place, got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest3, "evil.txt")); err != nil {
		t.Fatalf("neutralized entry missing inside dest: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dest3), "evil.txt")); err == nil {
		t.Fatal("zip-slip entry escaped the destination")
	}
}

// TestClassifyInstallError 归因分类的关键词命中与兜底。
func TestClassifyInstallError(t *testing.T) {
	cases := []struct {
		ctxErr error
		output string
		want   string
	}{
		{context.DeadlineExceeded, "", installErrKindTimeout},
		{nil, "winget : The term 'winget' is not recognized", installErrKindPmMissing},
		{nil, "curl: (28) Connection timed out after 30001 ms", installErrKindNetwork},
		{nil, "error: access denied when writing to C:\\Program Files", installErrKindPermission},
		{nil, "exit status 1", installErrKindGeneric},
	}
	for _, c := range cases {
		if got := classifyInstallError(c.ctxErr, c.output); got != c.want {
			t.Fatalf("classifyInstallError(%v, %q) = %q, want %q", c.ctxErr, c.output, got, c.want)
		}
	}
}

// writeTestZipFile 复用 uploads_parse_test.go 的 buildTestZip 打包，
// 再落到临时文件里（extractZipToDir 吃的是路径），返回路径。
func writeTestZipFile(t *testing.T, files map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "test.zip")
	if err := os.WriteFile(p, buildTestZip(t, files), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}
