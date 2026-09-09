package server

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/pkg/types"
)

func op(action, path string) types.FileOp {
	return types.FileOp{Action: action, Path: path}
}

// stripParam 只比较语义字段（Action/Path），Param 仅是来源键名，不影响 UI 行为。
func stripParam(ops []types.FileOp) []types.FileOp {
	out := make([]types.FileOp, len(ops))
	for i, o := range ops {
		out[i] = types.FileOp{Action: o.Action, Path: o.Path}
	}
	return out
}

func TestExtractFileOps_basicMapping(t *testing.T) {
	cases := []struct {
		name     string
		toolName string
		args     string
		want     []types.FileOp
	}{
		{
			name:     "write_file via file_path",
			toolName: "write_file",
			args:     `{"file_path": "src/a.go", "content": "x"}`,
			want:     []types.FileOp{op("write", "src/a.go")},
		},
		{
			name:     "file_edit via path",
			toolName: "file_edit",
			args:     `{"path": "notes.md"}`,
			want:     []types.FileOp{op("write", "notes.md")},
		},
		{
			name:     "delete_file",
			toolName: "delete_file",
			args:     `{"path": "tmp/old.txt"}`,
			want:     []types.FileOp{op("delete", "tmp/old.txt")},
		},
		{
			name:     "read_file must map to read (not a change)",
			toolName: "read_file",
			args:     `{"path": "keep.txt"}`,
			want:     []types.FileOp{op("read", "keep.txt")},
		},
		{
			name:     "unknown tool with path falls back to access",
			toolName: "some_custom_tool",
			args:     `{"input_path": "x.csv"}`,
			want:     []types.FileOp{op("access", "x.csv")},
		},
		{
			name:     "diff_patch apply_patch maps to write",
			toolName: "diff_patch",
			args:     `{"action": "apply_patch", "path": "src/main.go", "patches": []}`,
			want:     []types.FileOp{op("write", "src/main.go")},
		},
		{
			name:     "diff_patch show_diff maps to read",
			toolName: "diff_patch",
			args:     `{"action": "show_diff", "path": "src/main.go"}`,
			want:     []types.FileOp{op("read", "src/main.go")},
		},
		{
			name:     "gitignore generate defaults to .gitignore",
			toolName: "gitignore",
			args:     `{"action": "generate"}`,
			want:     []types.FileOp{op("write", ".gitignore")},
		},
		{
			name:     "gitignore search produces no file op",
			toolName: "gitignore",
			args:     `{"action": "search", "query": "go"}`,
			want:     []types.FileOp{},
		},
		{
			name:     "same action+path deduped",
			toolName: "write_file",
			args:     `{"file_path": "a.txt", "path": "a.txt"}`,
			want:     []types.FileOp{op("write", "a.txt")},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stripParam(extractFileOps(tc.toolName, tc.args, ""))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("extractFileOps(%q, %s) = %+v, want %+v", tc.toolName, tc.args, got, tc.want)
			}
		})
	}
}

func TestExtractFileOps_batchSubdivision(t *testing.T) {
	t.Run("batch_write subdivides files array into write ops", func(t *testing.T) {
		args := `{"operation": "batch_write", "files": ["src/a.go", "src/b.go"]}`
		got := stripParam(extractFileOps("batch_file_ops", args, ""))
		want := []types.FileOp{op("write", "src/a.go"), op("write", "src/b.go")}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})

	t.Run("batch_read must not surface as write", func(t *testing.T) {
		args := `{"operation": "batch_read", "files": ["src/a.go", "src/b.go"]}`
		got := stripParam(extractFileOps("batch_file_ops", args, ""))
		want := []types.FileOp{op("read", "src/a.go"), op("read", "src/b.go")}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})

	t.Run("batch_delete via operations array", func(t *testing.T) {
		args := `{"operation": "batch_delete", "operations": [{"path": "out/old1.bin"}, {"path": "out/old2.bin"}]}`
		got := stripParam(extractFileOps("batch_file_ops", args, ""))
		want := []types.FileOp{op("delete", "out/old1.bin"), op("delete", "out/old2.bin")}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})

	t.Run("batch_search_replace counts as write", func(t *testing.T) {
		args := `{"operation": "batch_search_replace", "files": [{"path": "d/f.go"}]}`
		got := stripParam(extractFileOps("batch_file_ops", args, ""))
		want := []types.FileOp{op("write", "d/f.go")}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})

	t.Run("items array fallback keeps generic batch action", func(t *testing.T) {
		args := `{"items": [{"path": "x.txt"}]}`
		got := stripParam(extractFileOps("batch_file_ops", args, ""))
		want := []types.FileOp{op("batch", "x.txt")}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})
}

func TestMergeFileOps(t *testing.T) {
	base := []types.FileOp{op("write", "a.txt"), op("read", "b.txt")}
	more := []types.FileOp{op("write", "a.txt"), op("delete", "a.txt"), op("read", "c.txt")}
	got := mergeFileOps(base, more)
	want := []types.FileOp{op("write", "a.txt"), op("read", "b.txt"), op("delete", "a.txt"), op("read", "c.txt")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestTurnFileOpTracker_SnapshotDiff(t *testing.T) {
	dir := t.TempDir()
	ctx := tool.WithWorkDir(context.Background(), dir)
	writeTool := &tool.WriteFileTool{}

	// 已存在文件 → 修改：写前快照 + 净 diff
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("old line 1\nold line 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tr := NewTurnFileOpTracker()
	args := map[string]interface{}{"path": "a.txt", "content": "new line 1\nold line 2\n"}
	tr.ToolStarting(ctx, "write_file", args)
	if _, err := writeTool.Execute(ctx, args); err != nil {
		t.Fatalf("write_file failed: %v", err)
	}
	ops := tr.Result()
	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %+v", ops)
	}
	o := ops[0]
	if o.Action != "write" || o.Path != "a.txt" {
		t.Fatalf("got action=%q path=%q, want write/a.txt", o.Action, o.Path)
	}
	if !strings.Contains(o.Diff, "-old line 1") || !strings.Contains(o.Diff, "+new line 1") {
		t.Fatalf("diff missing changed lines:\n%s", o.Diff)
	}
}

func TestTurnFileOpTracker_NewFileHasFullAdditions(t *testing.T) {
	dir := t.TempDir()
	ctx := tool.WithWorkDir(context.Background(), dir)
	writeTool := &tool.WriteFileTool{}

	tr := NewTurnFileOpTracker()
	args := map[string]interface{}{"path": "sub/created.txt", "content": "hello\nworld\n"}
	tr.ToolStarting(ctx, "write_file", args)
	if _, err := writeTool.Execute(ctx, args); err != nil {
		t.Fatalf("write_file failed: %v", err)
	}
	ops := tr.Result()
	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %+v", ops)
	}
	o := ops[0]
	if o.Action != "write" || o.Path != "sub/created.txt" {
		t.Fatalf("got action=%q path=%q, want write/sub/created.txt", o.Action, o.Path)
	}
	if !strings.Contains(o.Diff, "+hello") || !strings.Contains(o.Diff, "+world") {
		t.Fatalf("new file diff should be all additions:\n%s", o.Diff)
	}
}

func TestTurnFileOpTracker_IdenticalRewriteIsNoOp(t *testing.T) {
	dir := t.TempDir()
	ctx := tool.WithWorkDir(context.Background(), dir)
	writeTool := &tool.WriteFileTool{}
	if err := os.WriteFile(filepath.Join(dir, "same.txt"), []byte("unchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tr := NewTurnFileOpTracker()
	args := map[string]interface{}{"path": "same.txt", "content": "unchanged\n"}
	tr.ToolStarting(ctx, "write_file", args)
	if _, err := writeTool.Execute(ctx, args); err != nil {
		t.Fatalf("write_file failed: %v", err)
	}
	if ops := tr.Result(); len(ops) != 0 {
		t.Fatalf("identical rewrite must not surface as a change, got %+v", ops)
	}
}

func TestTurnFileOpTracker_BatchDelete(t *testing.T) {
	dir := t.TempDir()
	ctx := tool.WithWorkDir(context.Background(), dir)
	if err := os.WriteFile(filepath.Join(dir, "gone.txt"), []byte("bye\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	batchTool := tool.NewBatchFileOpsTool()

	tr := NewTurnFileOpTracker()
	args := map[string]interface{}{"operation": "batch_delete", "files": []interface{}{"gone.txt"}}
	tr.ToolStarting(ctx, "batch_file_ops", args)
	if _, err := batchTool.Execute(ctx, args); err != nil {
		t.Fatalf("batch_delete failed: %v", err)
	}
	ops := tr.Result()
	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %+v", ops)
	}
	o := ops[0]
	if o.Action != "delete" || o.Path != "gone.txt" {
		t.Fatalf("got action=%q path=%q, want delete/gone.txt", o.Action, o.Path)
	}
	if !strings.Contains(o.Diff, "-bye") {
		t.Fatalf("deleted file diff should show removed line:\n%s", o.Diff)
	}
}

func TestTurnFileOpTracker_FailedWriteNotListed(t *testing.T) {
	dir := t.TempDir()
	ctx := tool.WithWorkDir(context.Background(), dir)
	writeTool := &tool.WriteFileTool{}

	// 逃逸路径：工具会被安全策略拒绝，磁盘无净变化 → 不进入"变更的文件"。
	tr := NewTurnFileOpTracker()
	args := map[string]interface{}{"path": "../outside.txt", "content": "x"}
	tr.ToolStarting(ctx, "write_file", args)
	if _, err := writeTool.Execute(ctx, args); err == nil {
		t.Fatal("expected escape write to fail")
	}
	if ops := tr.Result(); len(ops) != 0 {
		t.Fatalf("failed write must not surface as a change, got %+v", ops)
	}
}

func TestTurnFileOpTracker_WriteThenDeleteIsNetNoop(t *testing.T) {
	dir := t.TempDir()
	ctx := tool.WithWorkDir(context.Background(), dir)
	writeTool := &tool.WriteFileTool{}
	batchTool := tool.NewBatchFileOpsTool()

	// 建了又删：净无变化 → 列表为空（旧实现会误报 delete）。
	tr := NewTurnFileOpTracker()
	tr.ToolStarting(ctx, "write_file", map[string]interface{}{"path": "temp.txt", "content": "ephemeral\n"})
	if _, err := writeTool.Execute(ctx, map[string]interface{}{"path": "temp.txt", "content": "ephemeral\n"}); err != nil {
		t.Fatalf("write_file failed: %v", err)
	}
	tr.ToolStarting(ctx, "batch_file_ops", map[string]interface{}{"operation": "batch_delete", "files": []interface{}{"temp.txt"}})
	if _, err := batchTool.Execute(ctx, map[string]interface{}{"operation": "batch_delete", "files": []interface{}{"temp.txt"}}); err != nil {
		t.Fatalf("batch_delete failed: %v", err)
	}
	if ops := tr.Result(); len(ops) != 0 {
		t.Fatalf("create-then-delete must be a net no-op, got %+v", ops)
	}
}

func TestTurnFileOpTracker_DeleteExistingShowsRemoval(t *testing.T) {
	dir := t.TempDir()
	ctx := tool.WithWorkDir(context.Background(), dir)
	if err := os.WriteFile(filepath.Join(dir, "old.txt"), []byte("keep me?\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	batchTool := tool.NewBatchFileOpsTool()

	// 先改后删 → 文件消失：应显示 delete + 全部行移除。
	tr := NewTurnFileOpTracker()
	tr.ToolStarting(ctx, "write_file", map[string]interface{}{"path": "old.txt", "content": "replaced\n"})
	if _, err := (&tool.WriteFileTool{}).Execute(ctx, map[string]interface{}{"path": "old.txt", "content": "replaced\n"}); err != nil {
		t.Fatalf("write_file failed: %v", err)
	}
	tr.ToolStarting(ctx, "batch_file_ops", map[string]interface{}{"operation": "batch_delete", "files": []interface{}{"old.txt"}})
	if _, err := batchTool.Execute(ctx, map[string]interface{}{"operation": "batch_delete", "files": []interface{}{"old.txt"}}); err != nil {
		t.Fatalf("batch_delete failed: %v", err)
	}
	ops := tr.Result()
	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %+v", ops)
	}
	o := ops[0]
	if o.Action != "delete" || o.Path != "old.txt" {
		t.Fatalf("got action=%q path=%q, want delete/old.txt", o.Action, o.Path)
	}
	if !strings.Contains(o.Diff, "-keep me?") {
		t.Fatalf("net-deleted file diff should show the original content removed:\n%s", o.Diff)
	}
}
