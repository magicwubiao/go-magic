package server

import (
	"reflect"
	"testing"

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

func TestTurnFileOpTracker_ResultDriven(t *testing.T) {
	tr := NewTurnFileOpTracker()

	// 失败的写不计入
	tr.Observe("write", "fail.txt", true)
	// 成功的写计入
	tr.Observe("write", "a.txt", false)
	// 读类动作不计入
	tr.Observe("read", "b.txt", false)
	// 写后又删 → 最终 delete
	tr.Observe("write", "c.txt", false)
	tr.Observe("delete", "c.txt", false)
	// 删后又写 → 最终 write（重建）
	tr.Observe("delete", "d.txt", false)
	tr.Observe("write", "d.txt", false)

	got := tr.Result()
	want := []types.FileOp{
		op("write", "a.txt"),
		op("delete", "c.txt"),
		op("write", "d.txt"),
	}
	if len(got) != len(want) {
		t.Fatalf("got %d ops (%+v), want %d (%+v)", len(got), got, len(want), want)
	}
	byPath := map[string]string{}
	for _, o := range got {
		byPath[o.Path] = o.Action
	}
	for _, w := range want {
		if byPath[w.Path] != w.Action {
			t.Errorf("path %s: got action %q, want %q", w.Path, byPath[w.Path], w.Action)
		}
	}
}

func TestTurnFileOpTracker_ObserveToolCall(t *testing.T) {
	tr := NewTurnFileOpTracker()

	// write_file 成功
	tr.ObserveToolCall("write_file", `{"path":"src/main.go","content":"x"}`)
	// file_edit 成功
	tr.ObserveToolCall("file_edit", `{"path":"src/util.go","operation":"replace"}`)
	// batch_write 成功
	tr.ObserveToolCall("batch_file_ops", `{"operation":"batch_write","files":["a.go","b.go"]}`)
	// batch_delete 成功
	tr.ObserveToolCall("batch_file_ops", `{"operation":"batch_delete","files":["old.bin"]}`)
	// 未知工具不应臆断
	tr.ObserveToolCall("some_unknown_tool", `{"path":"mystery.txt"}`)

	got := tr.Result()
	byPath := map[string]string{}
	for _, o := range got {
		byPath[o.Path] = o.Action
	}
	expect := map[string]string{
		"src/main.go": "write",
		"src/util.go": "write",
		"a.go":        "write",
		"b.go":        "write",
		"old.bin":     "delete",
	}
	if len(byPath) != len(expect) {
		t.Fatalf("got %+v, want %d entries", byPath, len(expect))
	}
	for p, a := range expect {
		if byPath[p] != a {
			t.Errorf("path %s: got %q, want %q", p, byPath[p], a)
		}
	}
}
