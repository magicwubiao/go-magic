package cliconn

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// 这些测试不依赖具体平台的 echo/cat：把测试二进制自己当成外部 CLI 拉起来，
// 用环境变量决定它扮演哪个程序。这样 Windows/Linux/macOS 走的是同一条 exec 路径，
// 而不是"在 Linux 上验证、在 Windows 上炸"。

// TestHelperProcess 不是测试，是子进程入口。
func TestHelperProcess(t *testing.T) {
	if os.Getenv("CLICONN_HELPER") != "1" {
		return
	}
	mode := os.Getenv("CLICONN_HELPER_MODE")
	switch mode {
	case "echo_args":
		fmt.Print(strings.Join(os.Args[1:], "|"))
	case "echo_stdin":
		buf := make([]byte, 0, 1024)
		tmp := make([]byte, 512)
		for {
			n, err := os.Stdin.Read(tmp)
			buf = append(buf, tmp[:n]...)
			if err != nil {
				break
			}
		}
		fmt.Print(string(buf))
	case "echo_env":
		fmt.Print(os.Getenv(os.Getenv("CLICONN_HELPER_ENV")))
	case "fail":
		fmt.Print("partial stdout")
		fmt.Fprint(os.Stderr, "boom")
		os.Exit(3)
	case "big":
		for i := 0; i < 6000; i++ {
			fmt.Print("0123456789")
		}
	case "big_cn":
		// 3 字节/字：32768 的截断点必然落在某个字的中间
		for i := 0; i < 20000; i++ {
			fmt.Print("中")
		}
	case "slow":
		time.Sleep(5 * time.Second)
	}
	os.Exit(0)
}

// helperConnector 返回指向"测试二进制"的连接器，args 里带上 helper 入口。
func helperConnector(mode string) Connector {
	return Connector{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess", "--", mode},
		Env:     []string{"CLICONN_HELPER=1", "CLICONN_HELPER_MODE=" + mode},
	}
}

func TestExpandArgs(t *testing.T) {
	spec := ToolSpec{
		Name:        "demo",
		Description: "d",
		Args:        []string{"list", "--limit", "{{limit}}", "{{?verbose}}--verbose", "--q={{query}}"},
		Params: []ParamSpec{
			{Name: "limit", Type: "integer"},
			{Name: "verbose", Type: "boolean"},
			{Name: "query"},
		},
	}

	r, err := ResolveParams(spec, map[string]any{"limit": float64(5), "verbose": true, "query": "a b"})
	if err != nil {
		t.Fatalf("ResolveParams: %v", err)
	}
	got, err := ExpandArgs([]string{"--json"}, spec, r, nil)
	if err != nil {
		t.Fatalf("ExpandArgs: %v", err)
	}
	want := []string{"--json", "list", "--limit", "5", "--verbose", "--q=a b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExpandArgs = %#v, want %#v", got, want)
	}

	// 可选参数缺省：整条 arg 省略，而不是塞空串
	r2, err := ResolveParams(spec, map[string]any{})
	if err != nil {
		t.Fatalf("ResolveParams: %v", err)
	}
	got2, err := ExpandArgs(nil, spec, r2, nil)
	if err != nil {
		t.Fatalf("ExpandArgs: %v", err)
	}
	want2 := []string{"list", "--q="}
	if !reflect.DeepEqual(got2, want2) {
		t.Fatalf("ExpandArgs(empty) = %#v, want %#v", got2, want2)
	}
}

// TestExpandArgsEnvRefs {{env:NAME}} 的三条路径：正常展开、未声明报错、空值报错。
func TestExpandArgsEnvRefs(t *testing.T) {
	spec := ToolSpec{
		Name:        "post",
		Description: "d",
		Args:        []string{"-d", "{{payload}}", "{{env:WEBHOOK}}"},
		Params:      []ParamSpec{{Name: "payload", Required: true}},
	}
	r, err := ResolveParams(spec, map[string]any{"payload": "hi"})
	if err != nil {
		t.Fatalf("ResolveParams: %v", err)
	}

	got, err := ExpandArgs(nil, spec, r, map[string]string{"WEBHOOK": "https://oapi.example/robot?x=1"})
	if err != nil {
		t.Fatalf("ExpandArgs: %v", err)
	}
	want := []string{"-d", "hi", "https://oapi.example/robot?x=1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExpandArgs = %#v, want %#v", got, want)
	}

	// 未声明：报错而不是静默替换。
	if _, err := ExpandArgs(nil, spec, r, nil); err == nil {
		t.Fatal("undeclared env ref should fail")
	}
	// 已声明但为空（凭据还没填）：报错。
	if _, err := ExpandArgs(nil, spec, r, map[string]string{"WEBHOOK": ""}); err == nil {
		t.Fatal("empty env value should fail")
	}
	// env 值里藏模板语法：报错（防止被二次解析成参数占位符）。
	if _, err := ExpandArgs(nil, spec, r, map[string]string{"WEBHOOK": "{{limit}}"}); err == nil {
		t.Fatal("env value containing template syntax should fail")
	}
}

func TestResolveParamsValidation(t *testing.T) {
	spec := ToolSpec{
		Name:        "demo",
		Description: "d",
		Args:        []string{"{{mode}}"},
		Params: []ParamSpec{
			{Name: "mode", Required: true, Enum: []string{"fast", "slow"}},
		},
	}
	if _, err := ResolveParams(spec, map[string]any{}); err == nil {
		t.Fatal("missing required parameter should fail")
	}
	if _, err := ResolveParams(spec, map[string]any{"mode": "medium"}); err == nil {
		t.Fatal("value outside enum should fail")
	}
	if _, err := ResolveParams(spec, map[string]any{"mode": "FAST"}); err != nil {
		t.Fatalf("enum compare should be case-insensitive: %v", err)
	}
}

func TestConnectorValidate(t *testing.T) {
	base := Connector{
		Command: "gh",
		Tools: []ToolSpec{{
			Name:        "pr_list",
			Description: "list",
			Args:        []string{"pr", "list", "--limit", "{{limit}}"},
			Params:      []ParamSpec{{Name: "limit", Type: "integer"}},
		}},
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid connector rejected: %v", err)
	}

	cases := map[string]func(c *Connector){
		"no command":       func(c *Connector) { c.Command = " " },
		"no tools":         func(c *Connector) { c.Tools = nil },
		"bad tool name":    func(c *Connector) { c.Tools[0].Name = "pr list" },
		"undeclared param": func(c *Connector) { c.Tools[0].Params = nil },
		"bad env":          func(c *Connector) { c.Env = []string{"NOVALUE"} },
		"duplicate tool": func(c *Connector) {
			c.Tools = append(c.Tools, c.Tools[0])
		},
		"no args": func(c *Connector) { c.Tools[0].Args = nil },
	}
	for name, mutate := range cases {
		c := Connector{Command: base.Command, Tools: []ToolSpec{base.Tools[0]}}
		mutate(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s: expected validation error, got nil", name)
		}
	}

	if err := ValidateConnectorName("-bad"); err == nil {
		t.Error("connector name starting with '-' should be rejected")
	}
}

func TestRunPassesArgsWithoutShellParsing(t *testing.T) {
	conn := helperConnector("echo_args")
	conn.Args = nil // 只看工具参数
	spec := ToolSpec{
		Name:        "echo",
		Description: "d",
		Args:        []string{"{{payload}}"},
		Params:      []ParamSpec{{Name: "payload", Required: true}},
	}
	nasty := "a b; rm -rf / | tee x && echo pwned $(id)"

	res, err := Run(context.Background(), conn, spec, map[string]any{"payload": nasty})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %s", res.ExitCode, res.Stderr)
	}
	// helper 把 argv 用 | 拼起来打印；参数没被 shell 拆分时就是原样一段
	if got := strings.TrimSpace(res.Stdout); got != nasty {
		t.Fatalf("stdout = %q, want %q", got, nasty)
	}
}

func TestRunStdinEnvAndExitCode(t *testing.T) {
	conn := helperConnector("echo_stdin")
	conn.Args = nil
	spec := ToolSpec{Name: "cat", Description: "d", Args: []string{"x"}, StdinParam: "text",
		Params: []ParamSpec{{Name: "text"}}}
	res, err := Run(context.Background(), conn, spec, map[string]any{"text": "hello 世界"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Stdout != "hello 世界" {
		t.Fatalf("stdout = %q", res.Stdout)
	}
	if res.StdinBytes == 0 {
		t.Fatal("stdin bytes not recorded")
	}

	envConn := helperConnector("echo_env")
	envConn.Args = nil
	envConn.Env = append(envConn.Env, "MY_TOKEN=secret-123", "CLICONN_HELPER_ENV=MY_TOKEN")
	res, err = Run(context.Background(), envConn, ToolSpec{Name: "e", Description: "d", Args: []string{"x"}}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Stdout != "secret-123" {
		t.Fatalf("env not passed through, stdout = %q", res.Stdout)
	}

	// 连接器的 env 必须覆盖进程继承的同名变量：重名键追加在数组末尾时，
	// getenv（Unix）与环境块解析（Windows）都只认第一个，配置就形同虚设。
	t.Setenv("MY_TOKEN", "stale-from-parent")
	res, err = Run(context.Background(), envConn, ToolSpec{Name: "e", Description: "d", Args: []string{"x"}}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Stdout != "secret-123" {
		t.Fatalf("connector env must override inherited env, stdout = %q", res.Stdout)
	}

	failConn := helperConnector("fail")
	failConn.Args = nil
	res, err = Run(context.Background(), failConn, ToolSpec{Name: "f", Description: "d", Args: []string{"x"}}, nil)
	if err != nil {
		t.Fatalf("non-zero exit must not be a Go error: %v", err)
	}
	if res.ExitCode != 3 {
		t.Fatalf("exit code = %d, want 3", res.ExitCode)
	}
	if res.Stdout != "partial stdout" || res.Stderr != "boom" {
		t.Fatalf("stdout/stderr = %q / %q", res.Stdout, res.Stderr)
	}
}

func TestRunTimeoutAndOutputCap(t *testing.T) {
	slow := helperConnector("slow")
	slow.Args = nil
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	res, err := Run(ctx, slow, ToolSpec{Name: "s", Description: "d", Args: []string{"x"}}, nil)
	if err != nil {
		t.Fatalf("timeout should be reported in Result, not as error: %v", err)
	}
	if !res.TimedOut || res.ExitCode != 124 {
		t.Fatalf("timed out = %v, exit code = %d", res.TimedOut, res.ExitCode)
	}

	big := helperConnector("big")
	big.Args = nil
	res, err = Run(context.Background(), big, ToolSpec{Name: "b", Description: "d", Args: []string{"x"}}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Truncated {
		t.Fatal("20000 bytes of output should be truncated")
	}
	if len(res.Stdout) != DefaultMaxOutputBytes {
		t.Fatalf("kept %d bytes, want %d", len(res.Stdout), DefaultMaxOutputBytes)
	}

	// 截断点落在多字节字符中间时，不能把尾巴上的半个字当成"这不是 UTF-8"
	// 转去解 GB18030 —— 那会把整段中文变成乱码。
	bigCN := helperConnector("big_cn")
	bigCN.Args = nil
	res, err = Run(context.Background(), bigCN, ToolSpec{Name: "b", Description: "d", Args: []string{"x"}}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Truncated {
		t.Fatal("60000 bytes of output should be truncated")
	}
	if !utf8.ValidString(res.Stdout) {
		t.Fatalf("truncated stdout is not valid UTF-8: %q", res.Stdout[max(0, len(res.Stdout)-8):])
	}
	for _, r := range res.Stdout {
		if r != '中' {
			t.Fatalf("stdout contains mojibake %q", r)
		}
	}
}

// approvalAwareShape 与 internal/agent 里的 approvalAware（未导出）形状一致。
//
// 存在的唯一目的是编译期断言：agent 判断"这个工具要不要人工确认"用的是
// `t.(approvalAware)` 类型断言，**少实现一个方法就断言失败、静默放行**。
// 单看 *Tool.RequiresApproval() 的返回值是发现不了的（那样测试会一直绿）。
type approvalAwareShape interface {
	RequiresApproval() bool
	ApprovalSummary() string
}

var _ approvalAwareShape = (*Tool)(nil)

func TestToolApprovalInterfaceShape(t *testing.T) {
	tool := NewTool("gh", Connector{Command: "gh"}, ToolSpec{Name: "pr_list", Description: "d", Args: []string{"pr", "list"}})
	var aw approvalAwareShape = tool // 断言一次，确保运行时断言也成立
	if !aw.RequiresApproval() {
		t.Fatal("non read_only tool must require approval")
	}
	if summary := aw.ApprovalSummary(); !strings.Contains(summary, "gh") || !strings.Contains(summary, "pr_list") {
		t.Fatalf("approval summary = %q", summary)
	}
}

func TestToolMetadataAndApproval(t *testing.T) {
	conn := Connector{Command: "gh", Description: "GitHub CLI"}
	spec := ToolSpec{Name: "pr_list", Description: "List PRs", Args: []string{"pr", "list"},
		Params: []ParamSpec{{Name: "limit", Type: "integer", Description: "max rows"}}}

	tool := NewTool("gh", conn, spec)
	if tool.Name() != "cli_gh_pr_list" {
		t.Fatalf("name = %q", tool.Name())
	}
	if !strings.Contains(tool.Description(), "GitHub CLI") || !strings.Contains(tool.Description(), "gh pr list") {
		t.Fatalf("description = %q", tool.Description())
	}
	if !tool.RequiresApproval() {
		t.Fatal("write-capable tool must require approval by default")
	}

	schema := tool.Schema()
	props, _ := schema["properties"].(map[string]interface{})
	if _, ok := props["limit"]; !ok {
		t.Fatalf("schema properties = %#v", props)
	}

	readOnly := spec
	readOnly.ReadOnly = true
	if NewTool("gh", conn, readOnly).RequiresApproval() {
		t.Fatal("read_only tool should skip approval")
	}

	on := true
	conn.RequireApproval = &on
	if !NewTool("gh", conn, readOnly).RequiresApproval() {
		t.Fatal("connector-level require_approval must win over read_only")
	}
}

func TestManagerFiltersAndUpsert(t *testing.T) {
	off := false
	m := New(map[string]Connector{
		"gh": {Command: "gh", Tools: []ToolSpec{{Name: "a", Description: "d", Args: []string{"a"}}}},
		"kubectl": {
			Command:  "kubectl",
			Profiles: []string{"work"},
			Tools:    []ToolSpec{{Name: "b", Description: "d", Args: []string{"b"}}},
		},
		"old": {Command: "x", Enabled: &off, Tools: []ToolSpec{{Name: "c", Description: "d", Args: []string{"c"}}}},
	})

	got := m.ToolsForProfile("home")
	if len(got) != 1 || got[0].Name() != "cli_gh_a" {
		t.Fatalf("home tools = %v", names(got))
	}
	got = m.ToolsForProfile("work")
	if len(got) != 2 {
		t.Fatalf("work tools = %v", names(got))
	}

	if err := m.Upsert("bad", Connector{}); err == nil {
		t.Fatal("upsert without command should fail")
	}
	if err := m.Upsert("new", Connector{Command: "n", Tools: []ToolSpec{{Name: "t", Description: "d", Args: []string{"t"}}}}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, ok := m.Get("new"); !ok {
		t.Fatal("upsert did not store connector")
	}
	if !m.Delete("new") {
		t.Fatal("delete should report existing connector")
	}
	if m.Delete("new") {
		t.Fatal("second delete should report missing connector")
	}
}

func names(tools []*Tool) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.Name())
	}
	return out
}

func TestCheckExecutableMissing(t *testing.T) {
	err := checkExecutable(os.Args[0] + "-does-not-exist")
	if err == nil {
		t.Fatal("missing absolute path should be rejected early")
	}
	if _, runErr := Run(context.Background(), Connector{Command: os.Args[0] + "-nope"},
		ToolSpec{Name: "x", Description: "d", Args: []string{"x"}}, nil); runErr == nil {
		t.Fatal("Run should fail for a missing executable")
	}
}

func TestIsBatchScriptHint(t *testing.T) {
	if !isBatchScript("C:/tools/foo.CMD") {
		// 非 Windows 平台 isBatchScript 恒为 false，这条断言只对 Windows 成立
		if _, err := exec.LookPath("cmd"); err == nil {
			t.Log("non-batch platform: skip")
		}
	}
}
