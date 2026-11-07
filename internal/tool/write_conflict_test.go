package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/pkg/utils"
)

// TestWriteFileRejectsStaleOverwriteFromOtherWriter 复现实测丢改动场景：
// 写者 A 写入后，写者 B 用基于旧版本的整文件内容覆盖 → 必须拒绝；
// B read_file 之后再写 → 放行。把"静默丢失"变成"显式报错 + 可恢复"。
func TestWriteFileRejectsStaleOverwriteFromOtherWriter(t *testing.T) {
	resetFileWriteGuardForTest()
	dir := t.TempDir()
	path := filepath.Join(dir, "price.go")

	wt := &WriteFileTool{}
	rt := &ReadFileTool{}
	ctxA := utils.WithWriterID(context.Background(), "task-aaa")
	ctxB := utils.WithWriterID(context.Background(), "task-bbb")

	if _, err := wt.Execute(ctxA, map[string]interface{}{"path": path, "content": "func Total() int { return 101 }\n"}); err != nil {
		t.Fatalf("A 首次写入应成功: %v", err)
	}

	_, err := wt.Execute(ctxB, map[string]interface{}{"path": path, "content": "func Fee() int { return 102 }\n"})
	if err == nil {
		t.Fatal("B 基于旧版本的整文件覆盖必须被拒绝，否则 A 的改动会被静默抹掉")
	}
	msg := err.Error()
	for _, want := range []string{"read_file", "another task"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("拒绝信息缺少行动指引 %q：%s", want, msg)
		}
	}

	// A 的改动必须原样保留
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "101") {
		t.Fatalf("被拒绝的写入不应落盘，文件内容被改动：%s", data)
	}

	// B 读过之后重试即可成功
	if _, err := rt.Execute(ctxB, map[string]interface{}{"path": path}); err != nil {
		t.Fatalf("read_file 应成功: %v", err)
	}
	if _, err := wt.Execute(ctxB, map[string]interface{}{"path": path, "content": "func Fee() int { return 102 }\n"}); err != nil {
		t.Fatalf("读过之后重试应放行: %v", err)
	}
	data, _ = os.ReadFile(path)
	if !strings.Contains(string(data), "102") {
		t.Fatalf("重试后应写入新内容：%s", data)
	}
}

// TestWriteFileRejectsOverwriteAfterFileEdit 文件 edit 同样改变文件版本，
// 随后的 stale write_file 也必须被拦（实测 10/10 被抹掉的那条路径）。
func TestWriteFileRejectsOverwriteAfterFileEdit(t *testing.T) {
	resetFileWriteGuardForTest()
	dir := t.TempDir()
	path := filepath.Join(dir, "ops.go")
	v0 := "func Alpha() int { return 1 }\nfunc Beta() int { return 2 }\n"
	if err := os.WriteFile(path, []byte(v0), 0644); err != nil {
		t.Fatal(err)
	}

	ctxA := utils.WithWriterID(context.Background(), "task-aaa")
	ctxB := utils.WithWriterID(context.Background(), "task-bbb")

	et := &FileEditTool{}
	if _, err := et.Execute(ctxA, map[string]interface{}{
		"operation": "replace", "path": path,
		"old_content": "func Alpha() int { return 1 }",
		"new_content": "func Alpha() int { return 101 }",
	}); err != nil {
		t.Fatalf("file_edit 应成功: %v", err)
	}

	wt := &WriteFileTool{}
	_, err := wt.Execute(ctxB, map[string]interface{}{
		"path":    path,
		"content": "func Alpha() int { return 1 }\nfunc Beta() int { return 102 }\n", // 基于 v0
	})
	if err == nil {
		t.Fatal("file_edit 之后的 stale write_file 必须被拒绝")
	}

	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "101") {
		t.Fatalf("Alpha 的修复不应被抹掉：%s", data)
	}
}

// TestWriteFileSameWriterChainedWritesAllowed 同一写者连写两次（都基于自己的
// 最新版本）不应被自己误判：这是最常见的一条路径。
func TestWriteFileSameWriterChainedWritesAllowed(t *testing.T) {
	resetFileWriteGuardForTest()
	dir := t.TempDir()
	path := filepath.Join(dir, "conf.json")

	wt := &WriteFileTool{}
	ctx := utils.WithWriterID(context.Background(), "task-solo")
	for i, content := range []string{"{\"v\": 1}", "{\"v\": 2}", "{\"v\": 3}"} {
		if _, err := wt.Execute(ctx, map[string]interface{}{"path": path, "content": content}); err != nil {
			t.Fatalf("第 %d 次同写者写入不应被拒绝: %v", i+1, err)
		}
	}
}

// TestWriteFileMainAgentNotCrossBlocked 主 agent（无写者标识）不参与跨写者
// 拦截：它没有任务上下文，而且它与子代理的交接通常发生在任务结束之后。
// 这是有意的取舍——真正的并发写者是并行子代理。
func TestWriteFileMainAgentNotCrossBlocked(t *testing.T) {
	resetFileWriteGuardForTest()
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.md")

	sub := utils.WithWriterID(context.Background(), "task-sub")
	if _, err := (&WriteFileTool{}).Execute(sub, map[string]interface{}{"path": path, "content": "sub write\n"}); err != nil {
		t.Fatalf("子代理写入应成功: %v", err)
	}
	if _, err := (&WriteFileTool{}).Execute(context.Background(), map[string]interface{}{"path": path, "content": "main write\n"}); err != nil {
		t.Fatalf("主 agent 覆盖不应被跨写者规则拦住: %v", err)
	}
}
