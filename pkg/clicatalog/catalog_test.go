package clicatalog

import (
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/cliconn"
)

// 目录条目就是"写坏的连接器"的最可能来源：参数模板、占位符声明、工具名
// 都是手写的，改一条就要全量校验一遍。
func TestEntriesAreValidConnectors(t *testing.T) {
	seenID := map[string]bool{}
	seenName := map[string]bool{}
	for _, e := range All() {
		if seenID[e.ID] {
			t.Fatalf("duplicate entry id %q", e.ID)
		}
		seenID[e.ID] = true

		if e.Name == "" || e.Description == "" {
			t.Fatalf("%s: name and description are required", e.ID)
		}
		if seenName[e.DefaultName] {
			t.Fatalf("%s: duplicate default name %q", e.ID, e.DefaultName)
		}
		seenName[e.DefaultName] = true

		if err := e.Connector.Validate(); err != nil {
			t.Fatalf("%s: invalid connector template: %v", e.ID, err)
		}
		if e.Connector.Command == "" {
			t.Fatalf("%s: command is required", e.ID)
		}
		if len(e.Connector.Tools) == 0 {
			t.Fatalf("%s: at least one tool is required", e.ID)
		}
		// 目录安装完要立刻可用：每个平台都得有安装指引（可以不止一条）。
		for _, goos := range []string{"windows", "darwin", "linux"} {
			if len(e.Install.CommandsFor(goos)) == 0 {
				t.Fatalf("%s: missing install hints for %s", e.ID, goos)
			}
		}
		for _, r := range e.Requires {
			if strings.TrimSpace(r.Key) == "" || strings.TrimSpace(r.Label) == "" {
				t.Fatalf("%s: requirement needs key and label: %+v", e.ID, r)
			}
		}
	}
}

// git 条目只放只读子命令：目录里出现一条会改仓库状态的工具，用户点"添加"
// 时是看不见的（它默认免审批，因为 read_only）。
func TestGitEntryIsReadOnly(t *testing.T) {
	e, ok := Get("git")
	if !ok {
		t.Fatal("git entry not found")
	}
	if e.Connector.Command != "git" {
		t.Fatalf("command = %q, want git", e.Connector.Command)
	}
	for _, spec := range e.Connector.Tools {
		if !spec.ReadOnly {
			t.Errorf("git tool %q must stay read-only (write ops belong to the user's own connector)", spec.Name)
		}
	}
}

// 仓库位置写成可省略的 -C {{repo}}：{{repo}} 为空时整条占位符被省略，
// 紧跟的 -C 由 ExpandArgs 的选项名回退一起收走，命令退化成"在当前目录执行"。
// 这条断言保护的是"目录里的写法约定"和"展开语义"之间的耦合——悬空的 -C
// 会让每一次 git 调用都直接报错。
func TestGitRepoParamIsOptional(t *testing.T) {
	e, ok := Get("git")
	if !ok {
		t.Fatal("git entry not found")
	}
	var status cliconn.ToolSpec
	for _, spec := range e.Connector.Tools {
		if spec.Name == "status" {
			status = spec
		}
	}
	if status.Name == "" {
		t.Fatal("git status tool not found")
	}

	r, err := cliconn.ResolveParams(status, map[string]any{})
	if err != nil {
		t.Fatalf("ResolveParams: %v", err)
	}
	argv, err := cliconn.ExpandArgs(e.Connector.Args, status, r, nil)
	if err != nil {
		t.Fatalf("ExpandArgs: %v", err)
	}
	if got, want := strings.Join(argv, " "), "status --short --branch"; got != want {
		t.Fatalf("argv = %q, want %q", got, want)
	}

	// 给了仓库路径时 -C 必须回到原位。
	r2, err := cliconn.ResolveParams(status, map[string]any{"repo": "/srv/repo"})
	if err != nil {
		t.Fatalf("ResolveParams: %v", err)
	}
	argv2, err := cliconn.ExpandArgs(e.Connector.Args, status, r2, nil)
	if err != nil {
		t.Fatalf("ExpandArgs: %v", err)
	}
	if got, want := strings.Join(argv2, " "), "-C /srv/repo status --short --branch"; got != want {
		t.Fatalf("argv = %q, want %q", got, want)
	}
}

func TestGetAndSort(t *testing.T) {
	if _, ok := Get("jq"); !ok {
		t.Fatal("jq entry not found")
	}
	if _, ok := Get("nope"); ok {
		t.Fatal("unknown id must not resolve")
	}

	all := All()
	for i := 1; i < len(all); i++ {
		pi := categorySort(all[i-1].Category)
		ci := categorySort(all[i].Category)
		if pi > ci {
			t.Fatalf("categories are not sorted: %s before %s", all[i-1].Category, all[i].Category)
		}
	}
}
