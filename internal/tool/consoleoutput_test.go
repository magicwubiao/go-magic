package tool

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// 回归：中文 Windows 控制台代码页是 GBK（cp936），execute_command 直接
// string(output) 会让 PowerShell 输出的中文变成一串替换字符（????），
// 模型据乱码误判命令结果。decodeConsoleOutput 必须：
//   - 合法 UTF-8 原样通过（Go/Node/Python 子进程、chcp 65001）
//   - 非 UTF-8 时按 GB18030 解码（GBK 超集）
func TestDecodeConsoleOutputPassesThroughUTF8(t *testing.T) {
	in := []byte("中文输出 English mixed\n")
	if got := decodeConsoleOutput(in); got != string(in) {
		t.Errorf("decodeConsoleOutput(%q) = %q, want passthrough", in, got)
	}
	if got := decodeConsoleOutput(nil); got != "" {
		t.Errorf("decodeConsoleOutput(nil) = %q, want empty", got)
	}
}

func TestDecodeConsoleOutputDecodesGBK(t *testing.T) {
	text := "中文输出测试：系统信息 ipconfig 修复验证"
	gbk, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(text))
	if err != nil {
		t.Fatalf("encode GB18030: %v", err)
	}
	if utf8.Valid(gbk) {
		t.Fatal("test setup: encoded bytes must not be valid UTF-8, otherwise the test is vacuous")
	}
	if got := decodeConsoleOutput(gbk); got != text {
		t.Errorf("decodeConsoleOutput(GBK) = %q, want %q", got, text)
	}
}

// TestExecuteCommandChineseOutput 端到端：execute_command 在中文 Windows
// （PowerShell + GBK 控制台）下 echo 中文，输出必须可读。
// Windows 路径走 GBK 解码；Unix 路径走 UTF-8 直通，两种环境都应通过。
func TestExecuteCommandChineseOutput(t *testing.T) {
	work := t.TempDir()
	ctx := WithWorkDir(context.Background(), work)

	tool := NewSecureExecuteCommandTool(work)
	res, err := tool.Execute(ctx, map[string]interface{}{"command": "echo 中文输出测试"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	m, ok := res.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type %T", res)
	}
	if code, _ := m["exit_code"].(int); code != 0 {
		t.Fatalf("exit_code = %v, output = %q, error = %v", m["exit_code"], m["output"], m["error"])
	}
	out, _ := m["output"].(string)
	if !strings.Contains(out, "中文输出测试") {
		t.Errorf("output %q does not contain the echoed Chinese text (mojibake?)", out)
	}
}

// TestDiffPatchShowDiffMisusePointsToShowChanges 回归：把"对比两个文件"的
// file_a/file_b 传给 show_diff 时，报错必须指路 show_changes，
// 而不是让模型在 path/new_content 语义里反复试错。
func TestDiffPatchShowDiffMisusePointsToShowChanges(t *testing.T) {
	tool := &DiffPatchTool{}
	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"action": "show_diff",
		"file_a": "a.txt",
		"file_b": "b.txt",
	})
	if err == nil {
		t.Fatal("expected an error for show_diff with file_a/file_b")
	}
	if !strings.Contains(err.Error(), "show_changes") {
		t.Errorf("error %q should point to the show_changes action", err)
	}
}
