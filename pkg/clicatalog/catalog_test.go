package clicatalog

import (
	"strings"
	"testing"
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
