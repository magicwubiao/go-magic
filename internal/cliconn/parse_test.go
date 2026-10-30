package cliconn

import (
	"reflect"
	"testing"
)

func TestParseCommandGHPRList(t *testing.T) {
	p, err := ParseCommand(`gh pr list --repo owner/repo --limit 20 --state open --json`)
	if err != nil {
		t.Fatalf("ParseCommand: %v", err)
	}
	if p.Command != "gh" {
		t.Fatalf("command = %q, want gh", p.Command)
	}
	if p.Tool.Name != "pr_list" {
		t.Fatalf("tool name = %q, want pr_list", p.Tool.Name)
	}
	wantArgs := []string{
		"pr", "list",
		"--repo", "{{repo}}",
		"--limit", "{{limit}}",
		"--state", "{{state}}",
		"{{?json}}--json",
	}
	if !reflect.DeepEqual(p.Tool.Args, wantArgs) {
		t.Fatalf("args = %v, want %v", p.Tool.Args, wantArgs)
	}

	byName := map[string]ParamSpec{}
	for _, prm := range p.Tool.Params {
		byName[prm.Name] = prm
	}
	if len(byName) != 4 {
		t.Fatalf("params = %d items, want 4: %+v", len(p.Tool.Params), p.Tool.Params)
	}
	if prm := byName["repo"]; prm.Type != "" || !prm.Required {
		t.Fatalf("repo param = %+v, want string+required", prm)
	}
	if prm := byName["limit"]; prm.Type != "integer" {
		t.Fatalf("limit param = %+v, want integer", prm)
	}
	if prm := byName["state"]; prm.Type != "" {
		t.Fatalf("state param = %+v, want string (omitted)", prm)
	}
	if prm := byName["json"]; prm.Type != "boolean" {
		t.Fatalf("json param = %+v, want boolean", prm)
	}
	// 描述回填原始命令，用户至少有一句可改的说明。
	if p.Tool.Description == "" {
		t.Fatal("description should default to the raw command")
	}
}

func TestParseCommandInlineEqualsAndNegative(t *testing.T) {
	p, err := ParseCommand(`kubectl get pods --context=prod-1 --offset -5 -n {namespace}`)
	if err != nil {
		t.Fatalf("ParseCommand: %v", err)
	}
	wantArgs := []string{
		"get", "pods",
		"--context={{context}}",
		"--offset", "{{offset}}",
		"-n", "{{namespace}}",
	}
	if !reflect.DeepEqual(p.Tool.Args, wantArgs) {
		t.Fatalf("args = %v, want %v", p.Tool.Args, wantArgs)
	}
	types := map[string]string{}
	for _, prm := range p.Tool.Params {
		types[prm.Name] = prm.Type
	}
	if types["context"] != "" {
		t.Fatalf("context type = %q, want omitted string", types["context"])
	}
	if types["offset"] != "integer" {
		t.Fatalf("offset type = %q, want integer (negative number is a value, not a flag)", types["offset"])
	}
	if types["namespace"] != "" {
		t.Fatalf("namespace type = %q, want omitted string", types["namespace"])
	}
	if p.Tool.Name != "get_pods" {
		t.Fatalf("tool name = %q, want get_pods", p.Tool.Name)
	}
}

func TestParseCommandBooleanFlagBeforeAnotherFlag(t *testing.T) {
	p, err := ParseCommand(`ffmpeg -i in.mp4 -v --codec copy`)
	if err != nil {
		t.Fatalf("ParseCommand: %v", err)
	}
	// -v 后面跟的是另一个选项 → -v 是布尔开关；--codec 后面的 copy 是值。
	wantArgs := []string{"-i", "{{i}}", "{{?v}}-v", "--codec", "{{codec}}"}
	if !reflect.DeepEqual(p.Tool.Args, wantArgs) {
		t.Fatalf("args = %v, want %v", p.Tool.Args, wantArgs)
	}
}

func TestParseCommandQuotedValueAndSeparator(t *testing.T) {
	p, err := ParseCommand(`git log --grep "fixed crash" --since 2.weeks --pretty`)
	if err != nil {
		t.Fatalf("ParseCommand: %v", err)
	}
	wantArgs := []string{
		"log",
		"--grep", "{{grep}}",
		"--since", "{{since}}",
		"{{?pretty}}--pretty",
	}
	if !reflect.DeepEqual(p.Tool.Args, wantArgs) {
		t.Fatalf("args = %v, want %v", p.Tool.Args, wantArgs)
	}

	// 裸 "--" 之后全部按位置参数处理，不再当成选项。
	p2, err := ParseCommand(`git add -- UPPER NEXT`)
	if err != nil {
		t.Fatalf("ParseCommand: %v", err)
	}
	// UPPER/NEXT 在 "--" 之后，不进工具名，但仍是显式占位符写法 → 参数。
	byName := map[string]bool{}
	for _, prm := range p2.Tool.Params {
		byName[prm.Name] = true
	}
	if !byName["upper"] || !byName["next"] {
		t.Fatalf("params = %+v, want upper & next", p2.Tool.Params)
	}
	if p2.Tool.Name != "add" {
		t.Fatalf("tool name = %q, want add (post-\"--\" words are not subcommands)", p2.Tool.Name)
	}
}

func TestParseCommandToolNameFromWords(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{`docker compose up -d`, "compose_up"},   // 首个 token 是命令，不算子命令词
		{`C:\tools\ffmpeg.exe -i {{in}}`, "run"}, // 命令带路径，无子命令词
		{`kubectl config use-context {CTX_NAME}`, "config_use_context"},
	}
	for _, c := range cases {
		p, err := ParseCommand(c.input)
		if err != nil {
			t.Fatalf("ParseCommand(%q): %v", c.input, err)
		}
		if p.Tool.Name != c.want {
			t.Fatalf("ParseCommand(%q) name = %q, want %q", c.input, p.Tool.Name, c.want)
		}
	}
}

func TestParseCommandDedupesParamNames(t *testing.T) {
	p, err := ParseCommand(`tool --tag go --tag rust`)
	if err != nil {
		t.Fatalf("ParseCommand: %v", err)
	}
	names := []string{}
	for _, prm := range p.Tool.Params {
		names = append(names, prm.Name)
	}
	if len(names) != 2 || names[0] != "tag" || names[1] != "tag_2" {
		t.Fatalf("param names = %v, want [tag tag_2]", names)
	}
}

func TestParseCommandErrors(t *testing.T) {
	if _, err := ParseCommand("   "); err == nil {
		t.Fatal("empty input should error")
	}
	// 全部 token 都拼不成合法名时回退 "run"，不应报错。
	p, err := ParseCommand(`-x -y`)
	if err != nil {
		t.Fatalf("flags-only input should parse, got %v", err)
	}
	if p.Command != "-x" {
		t.Fatalf("command = %q, want -x", p.Command)
	}
}

func TestTokenizeCommandQuotes(t *testing.T) {
	got := tokenizeCommand(`cmd "a b" 'c  d' plain`)
	want := []string{"cmd", "a b", "c  d", "plain"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tokens = %q, want %q", got, want)
	}
}
