package plugins

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/magicwubiao/go-magic/internal/cliconn"
	"github.com/magicwubiao/go-magic/internal/mcp"
)

const validManifest = `{
  "name": "github-workflow",
  "version": "1.0.0",
  "description": "PR review helpers",
  "connectors": {
    "gh": {
      "command": "gh",
      "tools": [
        {"name": "pr_list", "description": "list PRs", "args": ["pr", "list"], "read_only": true}
      ]
    }
  },
  "mcp_servers": {
    "docs": {"url": "https://example.com/mcp", "transport": "http"}
  },
  "requires_env": ["GH_TOKEN"]
}`

func TestParseValid(t *testing.T) {
	m, err := Parse([]byte(validManifest))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Name != "github-workflow" || len(m.Connectors) != 1 || len(m.MCPServers) != 1 {
		t.Fatalf("unexpected manifest: %+v", m)
	}
}

func TestParseRejectsUnknownField(t *testing.T) {
	// 作者把键名写错（connectors vs connector）时必须报错，
	// 静默忽略等于"装上了但什么都没生效"。
	_, err := Parse([]byte(`{"name":"x","connector":{}}`))
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestValidateRules(t *testing.T) {
	cases := []struct {
		name string
		in   Manifest
	}{
		{"bad name", Manifest{Name: "bad name"}},
		{"empty", Manifest{Name: "ok"}},
		{"bad version", Manifest{Name: "ok", Version: "v1", Skills: []string{"s"}}},
		{"connector without command", Manifest{
			Name:       "ok",
			Connectors: map[string]cliconn.Connector{"c": {}},
		}},
		{"remote without url", Manifest{
			Name:       "ok",
			MCPServers: map[string]mcp.ServerConfig{"s": {Transport: mcp.TransportHTTP}},
		}},
		{"stdio without command", Manifest{
			Name:       "ok",
			MCPServers: map[string]mcp.ServerConfig{"s": {}},
		}},
	}
	for _, tc := range cases {
		if err := tc.in.Validate(); err == nil {
			t.Errorf("%s: expected validation error", tc.name)
		}
	}
}

func TestConflicts(t *testing.T) {
	m := Manifest{
		Name:       "p",
		Connectors: map[string]cliconn.Connector{"gh": {Command: "gh"}},
		MCPServers: map[string]mcp.ServerConfig{"docs": {URL: "u", Transport: mcp.TransportHTTP}},
	}
	got := m.Conflicts(
		map[string]cliconn.Connector{"gh": {Command: "gh"}},
		map[string]mcp.ServerConfig{},
	)
	if len(got) != 1 || got[0] != "cli:gh" {
		t.Fatalf("got %v", got)
	}
}

func TestMissingEnv(t *testing.T) {
	m := Manifest{Name: "p", RequiresEnv: []string{"PLUGINS_TEST_UNSET_XYZ"}}
	got := m.MissingEnv()
	if len(got) != 1 || got[0] != "PLUGINS_TEST_UNSET_XYZ" {
		t.Fatalf("got %v", got)
	}

	t.Setenv("PLUGINS_TEST_SET_XYZ", "1")
	m = Manifest{Name: "p", RequiresEnv: []string{"PLUGINS_TEST_SET_XYZ"}}
	if got := m.MissingEnv(); len(got) != 0 {
		t.Fatalf("want none missing, got %v", got)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plugins", "installed.json")
	s := NewStore(path)

	m, err := Parse([]byte(validManifest))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put(Record{Manifest: m, Source: "/tmp/x"}); err != nil {
		t.Fatalf("put: %v", err)
	}

	// 重新打开（模拟下次启动）
	s2 := NewStore(path)
	list, err := s2.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Name != m.Name {
		t.Fatalf("unexpected list: %+v", list)
	}
	if list[0].InstalledAt.IsZero() {
		t.Fatal("InstalledAt should be filled in")
	}

	rec, ok, err := s2.Delete(m.Name)
	if err != nil || !ok {
		t.Fatalf("delete: %v %v", ok, err)
	}
	if rec.Name != m.Name {
		t.Fatalf("deleted wrong record: %+v", rec)
	}
	list, _ = s2.List()
	if len(list) != 0 {
		t.Fatalf("record should be gone, got %+v", list)
	}
}

func TestStoreToleratesCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "installed.json")
	if err := os.WriteFile(path, []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	list, err := NewStore(path).List()
	if err != nil {
		t.Fatalf("corrupt record file must not fail: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("want empty, got %+v", list)
	}
}

func TestDiscover(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good")
	if err := os.MkdirAll(good, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(good, ManifestFile), []byte(validManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	// 没有 plugin.json 的目录应被跳过
	if err := os.MkdirAll(filepath.Join(dir, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, errs := Discover(dir)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(got) != 1 || got[0].Name != "github-workflow" {
		t.Fatalf("unexpected discover result: %+v", got)
	}
}
