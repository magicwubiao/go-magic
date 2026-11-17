package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 幂等重放（already_applied）测试。
// 背景：2026-11-17 翻译任务中，agent 按过期对照清单整批下发 file_edit，
// 已改完的条目全部报 "old_content not found" → 6/6 连败 → 工具被隐藏 →
// 模型凭记忆瞎编参数连环失败。修复：old 不在、new 已在 → 按成功返回。
// ---------------------------------------------------------------------------

// TestFileEditIdempotentReplayAlreadyApplied 旧文不在、新文已在 → 返回
// alreadyAppliedError，且 replaceContentDetailed 不产出新内容。
func TestFileEditIdempotentReplayAlreadyApplied(t *testing.T) {
	tool := &FileEditTool{}
	// 文件当前状态：META 行已经是英文（此前编辑已生效）
	content := "const META = { index: 5, name: 'Hand-drawn' };\nctx.fillText('Max 3 turns', 1010, 858);\n"
	params := map[string]interface{}{
		"old_content": "const META = { index: 5, name: '手绘白板' };",
		"new_content": "const META = { index: 5, name: 'Hand-drawn' };",
	}
	_, _, err := tool.replaceContentDetailed(content, params, 2)
	if err == nil {
		t.Fatal("expected alreadyAppliedError, got nil")
	}
	var applied *alreadyAppliedError
	if !asAlreadyApplied(err, &applied) {
		t.Fatalf("expected *alreadyAppliedError, got %T: %v", err, err)
	}
	if applied.line != 1 {
		t.Errorf("expected new_content found at line 1, got %d", applied.line)
	}
	if !strings.Contains(err.Error(), "edit already applied") {
		t.Errorf("error message should state the edit is already applied, got: %v", err)
	}
}

// TestFileEditIdempotentReplayExecuteNoOp Execute 层：幂等重放必须按成功返回
// already_applied=true，且**不写盘**（文件内容与 mtime 均不变）。
func TestFileEditIdempotentReplayExecuteNoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.html")
	original := "<html lang=\"en\">\n<head>\n<meta charset=\"UTF-8\">\n</head>\n<body>Code Recording</body>\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	ctx := WithWorkDir(context.Background(), dir)
	tool := &FileEditTool{}
	result, err := tool.Execute(ctx, map[string]interface{}{
		"operation":   "replace",
		"path":        path,
		"old_content": "<html lang=\"zh-CN\">\n<head>\n<meta charset=\"UTF-8\">",
		"new_content": "<html lang=\"en\">\n<head>\n<meta charset=\"UTF-8\">",
	})
	if err != nil {
		t.Fatalf("idempotent replay must succeed, got error: %v", err)
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}
	if m["already_applied"] != true {
		t.Errorf("result must carry already_applied=true, got %v", m["already_applied"])
	}
	if m["bytes_written"].(int) != 0 {
		t.Errorf("no-op must not write bytes, got bytes_written=%v", m["bytes_written"])
	}

	// 文件内容不变
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != original {
		t.Errorf("file content must be untouched on idempotent replay")
	}
	// mtime 不变（证明没有发生任何写入）
	fi2, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !fi2.ModTime().Equal(fi.ModTime()) {
		t.Errorf("file mtime changed on no-op: %v -> %v", fi.ModTime(), fi2.ModTime())
	}
}

// TestFileEditStillFailsWhenNewNotPresent 旧文、新文都不在 → 仍是真实失败
// （幂等短路不能把真正的匹配失败吞掉）。
func TestFileEditStillFailsWhenNewNotPresent(t *testing.T) {
	tool := &FileEditTool{}
	content := "alpha\nbeta\ngamma\n"
	params := map[string]interface{}{
		"old_content": "delta",
		"new_content": "epsilon",
	}
	_, _, err := tool.replaceContentDetailed(content, params, 3)
	if err == nil {
		t.Fatal("expected genuine match failure, got nil")
	}
	if strings.Contains(err.Error(), "edit already applied") {
		t.Errorf("must NOT be classified as already applied: %v", err)
	}
	if !strings.Contains(err.Error(), "old_content not found") {
		t.Errorf("expected match diagnostics, got: %v", err)
	}
}

// TestFileEditIdempotentOnlyForContentMatch 幂等判定只作用于 old_content
// 匹配路径：没有 old_content 的行号替换不受影响。
func TestFileEditIdempotentOnlyForContentMatch(t *testing.T) {
	tool := &FileEditTool{}
	// 行号替换：line 1 换成新行，正常生效，与幂等逻辑无关
	content := "old line\nsecond\n"
	params := map[string]interface{}{
		"line_start":  1,
		"line_end":    1,
		"new_content": "new line",
	}
	got, _, err := tool.replaceContentDetailed(content, params, 2)
	if err != nil {
		t.Fatalf("line-based replace should work: %v", err)
	}
	if !strings.HasPrefix(got, "new line\n") {
		t.Errorf("line-based replace result unexpected: %q", got)
	}
}

// TestFailureHideExemptCoreEditTools 核心编辑工具永不进"失败即隐藏"名单：
// 连败达到阈值后，file_edit / write_file 必须仍在模型可见清单里
// （失败照常计入统计，但 schema 不能对模型消失）。
func TestFailureHideExemptCoreEditTools(t *testing.T) {
	for _, name := range []string{"file_edit", "write_file"} {
		r := NewRegistry()
		ft := &mockFailTool{BaseTool: *NewBaseTool(name, "core edit tool", nil), failRemaining: failureHideMinSamples}
		r.Register(ft)

		for i := 0; i < failureHideMinSamples; i++ {
			if _, err := r.Execute(context.Background(), name, nil); err == nil {
				t.Fatalf("%s: call %d should fail", name, i)
			}
		}
		if got := namesOfAvailable(r); !containsTool(got, name) {
			t.Errorf("%s must NEVER be hidden by recent failures, got %v", name, got)
		}
		if stats := r.GetStats()[name]; stats == nil || stats.Hidden {
			t.Errorf("%s stats.Hidden must stay false", name)
		}
		// 统计仍要如实记录失败（管理端可见）
		if stats := r.GetStats()[name]; stats != nil && stats.FailedCalls < failureHideMinSamples {
			t.Errorf("%s failures must still be counted, got %d", name, stats.FailedCalls)
		}
	}
}

// TestFailureHideStillHidesOtherTools 豁免名单只影响列出的工具，其它工具
// 的隐藏机制保持原样（回归保护）。
func TestFailureHideStillHidesOtherTools(t *testing.T) {
	r := NewRegistry()
	ft := &mockFailTool{BaseTool: *NewBaseTool("browser", "non-core tool", nil), failRemaining: failureHideMinSamples}
	r.Register(ft)
	for i := 0; i < failureHideMinSamples; i++ {
		_, _ = r.Execute(context.Background(), "browser", nil)
	}
	if got := namesOfAvailable(r); containsTool(got, "browser") {
		t.Errorf("non-core tool should still be hidden after %d failures, got %v", failureHideMinSamples, got)
	}
}

// asAlreadyApplied 是 errors.As 的局部包装，测试里避免直接依赖 errors 包的
// 泛型断言语法重复。
func asAlreadyApplied(err error, target **alreadyAppliedError) bool {
	for e := err; e != nil; {
		if a, ok := e.(*alreadyAppliedError); ok {
			*target = a
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}
