package acp

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRootSetRejectsTraversalAndOutsiders(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	set, err := NewRootSet(root, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !set.Contains(filepath.Join(sub, "file.txt")) {
		t.Error("path inside root should be allowed")
	}
	if set.Contains(filepath.Join(root, "..", "escape.txt")) {
		t.Error("path escaping root should be denied")
	}
	if set.Contains("/etc/passwd") && runtime.GOOS != "windows" {
		t.Error("absolute outsider should be denied")
	}
}

func TestRootSetRequiresAbsoluteCwd(t *testing.T) {
	if _, err := NewRootSet("relative/dir", nil); err == nil {
		t.Fatal("relative cwd must be rejected")
	}
	if _, err := NewRootSet(t.TempDir(), []string{"relative"}); err == nil {
		t.Fatal("relative additional dir must be rejected")
	}
}

func TestSymlinkEscapeDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks on Windows needs elevated privileges")
	}

	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	set, err := NewRootSet(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if set.Contains(filepath.Join(link, "secret.txt")) {
		t.Error("symlink escape must be denied")
	}
}

func TestReadTextLineRange(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "f.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc\nd"), 0o644); err != nil {
		t.Fatal(err)
	}

	set, err := NewRootSet(root, nil)
	if err != nil {
		t.Fatal(err)
	}

	line, limit := 2, 2
	res, err := set.ReadText(ReadTextFileParams{Path: path, Line: &line, Limit: &limit})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if res.Content != "b\nc" {
		t.Errorf("content = %q, want %q", res.Content, "b\nc")
	}

	// 起始行超出文件长度：返回空而不是报错（规范允许空结果）。
	line = 99
	res, err = set.ReadText(ReadTextFileParams{Path: path, Line: &line})
	if err != nil {
		t.Fatalf("read beyond EOF: %v", err)
	}
	if res.Content != "" {
		t.Errorf("content = %q, want empty", res.Content)
	}
}

func TestWriteTextDeniedOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	set, err := NewRootSet(root, nil)
	if err != nil {
		t.Fatal(err)
	}

	err = set.WriteText(WriteTextFileParams{
		Path:    filepath.Join(outside, "x.txt"),
		Content: "nope",
	})
	if err == nil {
		t.Fatal("write outside root must fail")
	}
	if !strings.Contains(err.Error(), "outside session roots") {
		t.Errorf("err = %v", err)
	}
}

func TestPermissionPolicyPicksLeastPrivilegeOption(t *testing.T) {
	options := []PermissionOption{
		{OptionID: "allow-always", Name: "Always", Kind: PermissionAllowAlways},
		{OptionID: "allow-once", Name: "Once", Kind: PermissionAllowOnce},
		{OptionID: "reject-once", Name: "Reject", Kind: PermissionRejectOnce},
	}

	policy := AllowKinds(ToolKindRead)
	kind := ToolKindRead
	out := policy(nil, PermissionRequest{ToolCall: ToolCall{Kind: &kind}, Options: options})
	if out.OptionID != "allow-once" {
		t.Errorf("outcome = %+v, want allow-once (least privilege)", out)
	}

	// 没有拒绝选项时返回 cancelled，而不是伪造一个批准。
	deny := DenyAllPermissions()
	out = deny(nil, PermissionRequest{ToolCall: ToolCall{Kind: &kind}, Options: []PermissionOption{
		{OptionID: "allow-once", Kind: PermissionAllowOnce},
	}})
	if out.Outcome != OutcomeCancelled {
		t.Errorf("outcome = %+v, want cancelled", out)
	}
}
