package acp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "acp.json")

	cfg := &Config{Agents: map[string]AgentConfig{
		"claude": {
			Command:        "npx",
			Args:           []string{"-y", "claude-code-acp"},
			Env:            []string{"ANTHROPIC_API_KEY=sk-test"},
			Workspace:      dir,
			TimeoutSeconds: 300,
			AutoApprove:    []string{ToolKindRead, ToolKindSearch},
		},
	}}
	if err := SaveConfig(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	agent, ok := got.Agents["claude"]
	if !ok {
		t.Fatal("agent missing after round trip")
	}
	if agent.Command != "npx" || len(agent.Args) != 2 {
		t.Errorf("agent = %+v", agent)
	}
	if !agent.EnabledValue() {
		t.Error("missing enabled field must mean enabled")
	}
	if agent.Timeout() != 300*time.Second {
		t.Errorf("timeout = %s", agent.Timeout())
	}
}

func TestLoadConfigMissingFileIsEmpty(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if len(cfg.Agents) != 0 {
		t.Errorf("agents = %v", cfg.Agents)
	}
}

func TestLoadConfigRejectsBrokenJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "acp.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("broken json must error")
	}
}

func TestAgentValidate(t *testing.T) {
	cases := []struct {
		name  string
		agent AgentConfig
		want  string
	}{
		{"empty-command", AgentConfig{}, "command is required"},
		{"relative-workspace", AgentConfig{Command: "x", Workspace: "rel"}, "workspace must be an absolute path"},
		{"bad-env", AgentConfig{Command: "x", Env: []string{"NO_EQUALS"}}, "KEY=VALUE"},
		{"bad-kind", AgentConfig{Command: "x", AutoApprove: []string{"nonsense"}}, "unknown tool kind"},
	}
	for _, c := range cases {
		err := c.agent.Validate(c.name)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want contains %q", c.name, err, c.want)
		}
	}

	if err := (AgentConfig{Command: "x"}).Validate("ok-name_1"); err != nil {
		t.Errorf("valid agent rejected: %v", err)
	}
	if err := (AgentConfig{Command: "x"}).Validate("-bad"); err == nil {
		t.Error("name starting with '-' must be rejected")
	}
}

func TestTimeoutClamp(t *testing.T) {
	if got := (AgentConfig{TimeoutSeconds: -5}).Timeout(); got != DefaultTimeoutSeconds*time.Second {
		t.Errorf("negative timeout = %s", got)
	}
	if got := (AgentConfig{TimeoutSeconds: 999999}).Timeout(); got != MaxTimeoutSeconds*time.Second {
		t.Errorf("huge timeout = %s", got)
	}
}

func TestAgentNamesOnlyEnabled(t *testing.T) {
	disabled := false
	cfg := &Config{Agents: map[string]AgentConfig{
		"a": {Command: "x"},
		"b": {Command: "x", Enabled: &disabled},
		"c": {Command: "x"},
	}}
	names := cfg.AgentNames()
	if len(names) != 2 || names[0] != "a" || names[1] != "c" {
		t.Errorf("names = %v", names)
	}
}

func TestAutoApproveAll(t *testing.T) {
	agent := AgentConfig{Command: "x", AutoApprove: []string{"all"}}
	kind := ToolKindExecute
	out := agent.PermissionPolicy()(nil, PermissionRequest{
		ToolCall: ToolCall{Kind: &kind},
		Options:  []PermissionOption{{OptionID: "allow-once", Kind: PermissionAllowOnce}},
	})
	if out.OptionID != "allow-once" {
		t.Errorf("outcome = %+v", out)
	}
}

func TestSaveConfigUsesRestrictivePerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits")
	}
	path := filepath.Join(t.TempDir(), "acp.json")
	if err := SaveConfig(path, &Config{Agents: map[string]AgentConfig{}}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Errorf("mode = %v, want 0600-ish (env may hold secrets)", info.Mode().Perm())
	}
}

func TestConfigJSONShape(t *testing.T) {
	// 字段名是用户手改的契约，改名前先确认没有破坏文档与示例。
	data, err := json.Marshal(AgentConfig{Command: "x"})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"command"} {
		if !strings.Contains(string(data), `"`+key+`"`) {
			t.Errorf("missing key %q in %s", key, data)
		}
	}
}
