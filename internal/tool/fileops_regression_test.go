package tool

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestListFilesAppliesPattern(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.go", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctx := WithWorkDir(context.Background(), dir)
	result, err := (&ListFilesTool{}).Execute(ctx, map[string]interface{}{"path": ".", "pattern": "*.go"})
	if err != nil {
		t.Fatal(err)
	}
	files := result.(map[string]interface{})["files"].([]map[string]interface{})
	if len(files) != 1 || files[0]["name"] != "a.go" {
		t.Fatalf("pattern did not filter listing: %#v", files)
	}
}

func TestReadFilePreservesFinalNewlineAndIntegerPagination(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lines.txt"), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := WithWorkDir(context.Background(), dir)
	reader := &ReadFileTool{}

	result, err := reader.Execute(ctx, map[string]interface{}{"path": "lines.txt", "offset": 2, "limit": 1})
	if err != nil {
		t.Fatal(err)
	}
	got := result.(map[string]interface{})
	if got["content"] != "two\n" || got["firstLine"] != 2 || got["read"] != 1 {
		t.Fatalf("unexpected paginated result: %#v", got)
	}

	result, err = reader.Execute(ctx, map[string]interface{}{"path": "lines.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.(map[string]interface{})["content"]; got != "one\ntwo\nthree\n" {
		t.Fatalf("final newline not preserved: %q", got)
	}
}

func TestDirectoryTreeContainsEachDirectoryOnce(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := WithWorkDir(context.Background(), dir)
	result, err := NewDirectoryTreeTool().Execute(ctx, map[string]interface{}{"path": ".", "max_depth": 3})
	if err != nil {
		t.Fatal(err)
	}
	root := result.(*TreeNode)
	if len(root.Children) != 1 {
		t.Fatalf("root should contain one subdirectory: %#v", root.Children)
	}
	sub := root.Children[0]
	if sub.Name != "sub" || sub.Path != filepath.Join(dir, "sub") || len(sub.Children) != 1 {
		t.Fatalf("subdirectory node malformed or duplicated: %#v", sub)
	}
	if sub.Children[0].Name != "nested" {
		t.Fatalf("nested directory missing: %#v", sub.Children)
	}
}

func TestDirectoryTreeUsesSchemaDefaultDepth(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "one", "two", "three"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := WithWorkDir(context.Background(), dir)
	result, err := NewDirectoryTreeTool().Execute(ctx, map[string]interface{}{"path": "."})
	if err != nil {
		t.Fatal(err)
	}
	root := result.(*TreeNode)
	if len(root.Children) != 1 || len(root.Children[0].Children) != 1 || len(root.Children[0].Children[0].Children) != 1 {
		t.Fatalf("default depth should include three nested directory levels: %#v", root)
	}
}

func TestBatchWritePreservesExistingPermissionMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission modes are not honored on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(path, []byte("echo old\n"), 0o751); err != nil {
		t.Fatal(err)
	}
	ctx := WithWorkDir(context.Background(), dir)
	params := map[string]interface{}{
		"operation":  "batch_write",
		"operations": []interface{}{map[string]interface{}{"path": "script.sh", "content": "echo new\n"}},
	}
	if _, err := NewBatchFileOpsTool().Execute(ctx, params); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o751 {
		t.Fatalf("permission mode changed: got %04o, want 0751", got)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "echo new") {
		t.Fatalf("batch write content not committed: %q, %v", data, err)
	}
}

func TestListFilesRejectsInvalidPattern(t *testing.T) {
	_, err := (&ListFilesTool{}).Execute(context.Background(), map[string]interface{}{"path": t.TempDir(), "pattern": "["})
	if err == nil || !strings.Contains(err.Error(), "invalid file pattern") {
		t.Fatalf("invalid glob should return a descriptive error, got %v", err)
	}
}

func TestReadFilePreservesMissingFinalNewline(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lines.txt"), []byte("one\ntwo"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := (&ReadFileTool{}).Execute(WithWorkDir(context.Background(), dir), map[string]interface{}{"path": "lines.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.(map[string]interface{})["content"]; got != "one\ntwo" {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestWriteFilePreservesExistingModeAndAppendSizeLimit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission semantics are not honored on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(path, []byte("old\n"), 0o751); err != nil {
		t.Fatal(err)
	}
	ctx := WithWorkDir(context.Background(), dir)
	writer := &WriteFileTool{}
	if _, err := writer.Execute(ctx, map[string]interface{}{"path": "script.sh", "content": "new\n"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o751 {
		t.Fatalf("write_file changed existing permission mode: got %04o", got)
	}

	limited := WithFileSecurity(ctx, FileSecurityConfig{Enabled: true, MaxFileSizeKB: 1, DefaultFileMode: 0o600, DefaultDirMode: 0o700})
	_, err = writer.Execute(limited, map[string]interface{}{"path": "script.sh", "content": strings.Repeat("x", 1021), "append": true})
	if err == nil || !strings.Contains(err.Error(), "exceeds maximum") {
		t.Fatalf("append should enforce total file size limit, got %v", err)
	}
}

func TestBatchWriteEnforcesSizeLimitBeforeCreatingParent(t *testing.T) {
	dir := t.TempDir()
	ctx := WithFileSecurity(WithWorkDir(context.Background(), dir), FileSecurityConfig{Enabled: true, MaxFileSizeKB: 1, DefaultFileMode: 0o600, DefaultDirMode: 0o700})
	params := map[string]interface{}{
		"operation":  "batch_write",
		"operations": []interface{}{map[string]interface{}{"path": "not-created/large.txt", "content": strings.Repeat("x", 1025)}},
	}
	result, err := NewBatchFileOpsTool().Execute(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	results := result.(map[string]interface{})["results"].(map[string]interface{})
	if _, ok := results["not-created/large.txt"]; !ok {
		t.Fatalf("missing per-file failure result: %#v", results)
	}
	if _, err := os.Stat(filepath.Join(dir, "not-created")); !os.IsNotExist(err) {
		t.Fatalf("oversized write created its parent directory, stat error=%v", err)
	}
}

func TestDiffPatchApplyPreservesModeAndWorkspaceBoundary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission modes are not honored on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(path, []byte("before\n"), 0o751); err != nil {
		t.Fatal(err)
	}
	ctx := WithWorkDir(context.Background(), dir)
	result, err := NewDiffPatchTool().Execute(ctx, map[string]interface{}{
		"action":  "apply_patch",
		"path":    "script.sh",
		"patches": []interface{}{map[string]interface{}{"old_text": "before", "new_text": "after"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.(map[string]interface{})["path"] != path {
		t.Fatalf("patch returned unexpected path: %#v", result)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o751 {
		t.Fatalf("apply_patch changed permission mode: got %04o", got)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewDiffPatchTool().Execute(ctx, map[string]interface{}{
		"action":  "apply_patch",
		"path":    filepath.Join(t.TempDir(), "outside.txt"),
		"patches": []interface{}{map[string]interface{}{"old_text": "x", "new_text": "y"}},
	})
	if err == nil {
		t.Fatal("apply_patch accepted a path outside the work directory")
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil || string(after) != string(before) {
		t.Fatalf("failed outside patch modified workspace file: %q, %v", after, readErr)
	}
}

func TestBatchWriteBackupRejectsEscapingSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires platform privileges on Windows")
	}
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.bak")
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "data.txt")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, target+".bak"); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	ctx := WithWorkDir(context.Background(), dir)
	result, err := NewBatchFileOpsTool().Execute(ctx, map[string]interface{}{
		"operation":  "batch_write",
		"operations": []interface{}{map[string]interface{}{"path": "data.txt", "content": "new", "backup": true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	results := result.(map[string]interface{})["results"].(map[string]interface{})
	fileResult := results["data.txt"].(map[string]interface{})
	if fileResult["success"] != false {
		t.Fatalf("symlink backup destination was not rejected: %#v", fileResult)
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "keep" {
		t.Fatalf("backup overwrote file through symlink: %q, %v", data, err)
	}
	data, err = os.ReadFile(target)
	if err != nil || string(data) != "old" {
		t.Fatalf("target modified despite failed backup path validation: %q, %v", data, err)
	}
}
