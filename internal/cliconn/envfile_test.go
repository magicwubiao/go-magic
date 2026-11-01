package cliconn

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件覆盖 2026-11 迭代新增的三个能力：
//   - {{envfile:NAME}} 凭据临时文件通道（凭据不进 argv）
//   - Connector.MaxOutputBytes 连接器级输出上限覆盖
//   - raw 工具的 cwd 参数与 Connector.RawMaxArgs 覆盖

func TestRunEnvFileMaterializesAndCleansUp(t *testing.T) {
	conn := helperConnector("slurp")
	conn.Env = append(conn.Env, "API_TOKEN=topsecret")
	spec := ToolSpec{
		Name:        "call",
		Description: "d",
		Args:        []string{"--token-file", "{{envfile:API_TOKEN}}"},
	}

	res, err := Run(context.Background(), conn, spec, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// 子进程读到的临时文件内容就是 env 真值
	if got := strings.TrimSpace(res.Stdout); got != "topsecret" {
		t.Fatalf("file content = %q, want topsecret", got)
	}
	// 回显 argv 不得含明文凭据（临时路径本身不是秘密，允许出现）
	if joined := strings.Join(res.Argv, " "); strings.Contains(joined, "topsecret") {
		t.Fatalf("credential leaked into argv echo: %q", joined)
	}
	// 进程结束后临时文件必须已删除：路径可从回显 argv 的末元素拿到
	path := res.Argv[len(res.Argv)-1]
	if !strings.Contains(filepath.Base(path), "cliconn-secret-") {
		t.Fatalf("expected temp secret file path in argv tail, got %q", path)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("temp secret file must be removed after run (stat err=%v)", err)
	}
}

func TestRunEnvFileRejectsPartialArg(t *testing.T) {
	conn := helperConnector("slurp")
	conn.Env = append(conn.Env, "API_TOKEN=topsecret")
	spec := ToolSpec{
		Name:        "call",
		Description: "d",
		Args:        []string{"--token={{envfile:API_TOKEN}}"}, // 拼进选项值：不合法
	}
	if _, err := Run(context.Background(), conn, spec, nil); err == nil {
		t.Fatal("partial-arg envfile placeholder must be rejected")
	}
}

func TestValidateEnvFileRefMustBeDeclared(t *testing.T) {
	conn := Connector{
		Command: "x",
		Env:     []string{"OTHER=1"},
		Tools: []ToolSpec{{
			Name:        "call",
			Description: "d",
			Args:        []string{"--token-file", "{{envfile:API_TOKEN}}"},
		}},
	}
	if err := conn.Validate(); err == nil {
		t.Fatal("undeclared envfile reference must fail validation")
	}
	conn.Env = []string{"API_TOKEN="}
	if err := conn.Validate(); err != nil {
		t.Fatalf("declared envfile reference should validate: %v", err)
	}
}

func TestConnectorMaxOutputBytesOverride(t *testing.T) {
	conn := helperConnector("big")
	conn.MaxOutputBytes = 100
	res, err := Run(context.Background(), conn, ToolSpec{Name: "b", Description: "d", Args: []string{"x"}}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Truncated || res.OutputLimit != 100 {
		t.Fatalf("truncated=%v outputLimit=%d, want true/100", res.Truncated, res.OutputLimit)
	}
	if len(res.Stdout) > 200 { // rune 边界允许少量溢出
		t.Fatalf("stdout too long: %d", len(res.Stdout))
	}

	// 超出硬上限的配置在 Validate 拒绝
	conn2 := Connector{Command: "x", MaxOutputBytes: MaxOutputBytesCap + 1,
		Tools: []ToolSpec{{Name: "t", Description: "d", Args: []string{"x"}}}}
	if err := conn2.Validate(); err == nil {
		t.Fatal("max_output_bytes above cap must be rejected")
	}
}

func TestRawToolCwdOverride(t *testing.T) {
	dir := t.TempDir()
	conn := helperConnector("pwd")
	conn.Args = nil // 只看 cwd 效果

	res, err := RunRaw(context.Background(), conn, nil, "", dir)
	if err != nil {
		t.Fatalf("RunRaw: %v", err)
	}
	if got := strings.TrimSpace(res.Stdout); got != dir {
		t.Fatalf("cwd = %q, want %q", got, dir)
	}

	// 相对路径按连接器 working_dir 解析
	base := t.TempDir()
	sub := filepath.Join(base, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	conn.WorkingDir = base
	res, err = RunRaw(context.Background(), conn, nil, "", "sub")
	if err != nil {
		t.Fatalf("RunRaw relative: %v", err)
	}
	if got := strings.TrimSpace(res.Stdout); got != sub {
		t.Fatalf("relative cwd = %q, want %q", got, sub)
	}

	// 不存在的目录：明确的参数错误
	if _, err := RunRaw(context.Background(), conn, nil, "", filepath.Join(base, "nope")); err == nil {
		t.Fatal("nonexistent cwd must be rejected")
	}
}

func TestRawMaxArgsOverride(t *testing.T) {
	conn := helperConnector("echo_args")
	conn.Args = nil
	conn.RawMaxArgs = 128

	args := make([]any, 100)
	for i := range args {
		args[i] = "a"
	}
	if _, err := RunRaw(context.Background(), conn, toStrings(args), "", ""); err != nil {
		t.Fatalf("100 args with RawMaxArgs=128 should pass: %v", err)
	}

	// 默认上限仍是 64
	conn2 := helperConnector("echo_args")
	conn2.Args = nil
	if _, err := RunRaw(context.Background(), conn2, toStrings(args), "", ""); err == nil {
		t.Fatal("100 args with default cap 64 must be rejected")
	}

	// Validate 拒绝超出硬上限的配置
	conn3 := Connector{Command: "x", RawMaxArgs: MaxRawArgsCap + 1,
		Tools: []ToolSpec{{Name: "t", Description: "d", Args: []string{"x"}}}}
	if err := conn3.Validate(); err == nil {
		t.Fatal("raw_max_args above cap must be rejected")
	}
}

func TestPinCommandPath(t *testing.T) {
	// 裸命令解析成功 → 固定成绝对路径（go 在编译环境必然可用）
	pinned := Connector{Command: "go"}.PinCommandPath()
	if !filepath.IsAbs(pinned.Command) {
		t.Fatalf("bare command must be pinned to absolute path, got %q", pinned.Command)
	}
	// 不存在的命令保留原名（由就绪探测报错，不在保存时造废配置）
	const missing = "definitely-not-a-real-binary-xyz"
	c := Connector{Command: missing}.PinCommandPath()
	if got := c.Command; got != missing {
		t.Fatalf("missing binary must keep its name, got %q", got)
	}
	// 已是绝对路径 → 原样返回（幂等）
	c = Connector{Command: os.Args[0]}.PinCommandPath()
	if got := c.Command; got != os.Args[0] {
		t.Fatalf("absolute command must be unchanged, got %q", got)
	}
}

func TestEffectiveLimitsDefaults(t *testing.T) {
	var c Connector
	if got := c.EffectiveMaxOutputBytes(); got != DefaultMaxOutputBytes {
		t.Fatalf("output limit = %d, want %d", got, DefaultMaxOutputBytes)
	}
	if got := c.EffectiveRawMaxArgs(); got != MaxRawArgs {
		t.Fatalf("raw max args = %d, want %d", got, MaxRawArgs)
	}
	c.MaxOutputBytes = MaxOutputBytesCap * 2 // 无效值被 Effective 钳制
	if got := c.EffectiveMaxOutputBytes(); got != MaxOutputBytesCap {
		t.Fatalf("clamped output limit = %d, want %d", got, MaxOutputBytesCap)
	}
}

func TestRawToolCwdInSchema(t *testing.T) {
	tool := NewRawTool("gh", Connector{Command: "gh"})
	props := tool.Schema()["properties"].(map[string]any)
	if _, ok := props["cwd"]; !ok {
		t.Fatal("raw schema must expose cwd")
	}
}

// toStrings 把 any 切片转成 string 切片（测试辅助）。
func toStrings(in []any) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		out = append(out, v.(string))
	}
	return out
}
