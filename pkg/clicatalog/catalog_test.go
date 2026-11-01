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

// gh 条目与目录里其他条目同一条纪律：声明式工具只放只读子命令；长尾走
// Raw 逃生工具（恒审批），声明式免审批面不能被写操作污染。
func TestGHEntryIsReadOnlyWithRaw(t *testing.T) {
	e, ok := Get("gh")
	if !ok {
		t.Fatal("gh entry not found")
	}
	if !e.Connector.Raw {
		t.Fatal("gh connector must enable Raw (its surface is too large to declare)")
	}
	for _, spec := range e.Connector.Tools {
		if !spec.ReadOnly {
			t.Errorf("gh tool %q must stay read-only (write ops belong to raw with approval)", spec.Name)
		}
	}

	// pr_list 的 repo 可省略：空值时 --repo 与 {{repo}} 一起被收走，
	// 命令在仓库目录内也能跑。
	var prList cliconn.ToolSpec
	for _, spec := range e.Connector.Tools {
		if spec.Name == "pr_list" {
			prList = spec
		}
	}
	r, err := cliconn.ResolveParams(prList, map[string]any{})
	if err != nil {
		t.Fatalf("ResolveParams without repo: %v", err)
	}
	argv, err := cliconn.ExpandArgs(e.Connector.Args, prList, r, nil)
	if err != nil {
		t.Fatalf("ExpandArgs without repo: %v", err)
	}
	if got, want := strings.Join(argv, " "), "pr list --limit 20 --state open"; got != want {
		t.Fatalf("argv = %q, want %q", got, want)
	}

	r2, err := cliconn.ResolveParams(prList, map[string]any{"repo": "octocat/hello"})
	if err != nil {
		t.Fatalf("ResolveParams with repo: %v", err)
	}
	argv2, err := cliconn.ExpandArgs(e.Connector.Args, prList, r2, nil)
	if err != nil {
		t.Fatalf("ExpandArgs with repo: %v", err)
	}
	if got, want := strings.Join(argv2, " "), "pr list --limit 20 --state open --repo octocat/hello"; got != want {
		t.Fatalf("argv = %q, want %q", got, want)
	}
}

// raw 不变量：目录的每个条目都必须开启 Raw 逃生工具（见 catalog.go 包注释）。
// 声明式工具只登记高频只读操作，CLI 的其余子命令面一律走 raw（恒审批）。
// 新增条目忘开 raw 会让模型对长尾子命令完全失明，这里锁死。
func TestCatalogEntriesShipRaw(t *testing.T) {
	for _, e := range All() {
		if !e.Connector.Raw {
			t.Errorf("%s: catalog entries must enable Raw (declared tools cover the read-only surface, raw covers the rest)", e.ID)
		}
	}
}

func TestGetAndSort(t *testing.T) {
	if _, ok := Get("gh"); !ok {
		t.Fatal("gh entry not found")
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

// readOnlySnapshot 钉死目录里每一个工具的「只读 / 可写」声明。
//
// 存在的理由：ReadOnly 是唯一能把一个 CLI 工具变成**免审批**的开关
// （internal/cliconn 的 ApprovalRequired 直接吃这个字段），而目录条目全是
// 手写参数模板。谁给 docker 加一条 `exec` 又顺手抄了上面的 read_only: true，
// 就得到一条静默免审批的任意命令——编译过得去，别的单测也不会响。
// 把清单固化在这里，任何新增或翻转都得先改这个常量，也就必须在 review 里
// 被看见。改它不是"修测试"，是"确认这个工具真的只读"。
const readOnlySnapshot = `docker: ps=ro images=ro inspect=ro logs=ro
gh: pr_list=ro issue_list=ro run_list=ro repo_view=ro
aws: whoami=ro s3_ls=ro ec2_instances=ro
aliyun: whoami=ro ecs_instances=ro oss_ls=ro
wrangler: whoami=ro deployments=ro kv_namespaces=ro
supabase: projects=ro orgs=ro db_diff=ro
tccli: cvm_instances=ro cvm_zones=ro
vercel: whoami=ro projects=ro deployments=ro`

func TestReadOnlyToolSnapshot(t *testing.T) {
	var lines []string
	for _, e := range All() {
		parts := make([]string, 0, len(e.Connector.Tools))
		for _, sp := range e.Connector.Tools {
			state := "rw"
			if sp.ReadOnly {
				state = "ro"
			}
			parts = append(parts, sp.Name+"="+state)
		}
		lines = append(lines, e.ID+": "+strings.Join(parts, " "))
	}
	got := strings.Join(lines, "\n")
	if got != readOnlySnapshot {
		t.Fatalf("目录的只读声明变了。\n"+
			"ro = 免审批，rw = 每次审批。逐条确认新增/翻转的工具真的只读，\n"+
			"确认后把 readOnlySnapshot 整体替换成下面的内容：\n\n%s", got)
	}
	// 写操作必须留在 raw（恒审批），不能靠 read_only 混进免审批面。
	for _, e := range All() {
		for _, sp := range e.Connector.Tools {
			if sp.ReadOnly {
				continue
			}
			if !e.Connector.Raw {
				t.Errorf("%s/%s 是写操作，但连接器没开 Raw：它只能走审批，而目录条目必须留 raw 逃生口", e.ID, sp.Name)
			}
		}
	}
}
