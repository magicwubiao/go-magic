package agentplugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile 在 dir 下创建文件 file,内容为 content(自动创建父目录)。
func writeFile(t *testing.T, dir, file, content string) {
	t.Helper()
	full := filepath.Join(dir, file)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

// validManifest 返回一份通过校验的最小 plugin.json 内容。
func validManifest() string {
	return `{
  "$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json",
  "name": "demo-plugin",
  "version": "1.0.0",
  "description": "a test plugin"
}`
}

// makePlugin 在临时目录创建一个含合法 plugin.json 的插件根,返回其路径。
func makePlugin(t *testing.T, manifest string) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "plugin.json", manifest)
	return root
}

// =============================================================================
// Manifest 校验
// =============================================================================

func TestLoadManifest_Valid(t *testing.T) {
	root := makePlugin(t, validManifest())
	m, warns, err := loadManifest(root)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if m.Name != "demo-plugin" {
		t.Errorf("name = %q, want demo-plugin", m.Name)
	}
	if len(warns) != 0 {
		t.Errorf("expected no warnings, got %v", warns)
	}
}

func TestLoadManifest_MissingSchema(t *testing.T) {
	root := makePlugin(t, `{"name":"demo-plugin"}`)
	_, _, err := loadManifest(root)
	if err == nil {
		t.Fatal("expected error for missing $schema")
	}
	if !strings.Contains(err.Error(), "$schema") {
		t.Errorf("error should mention $schema, got %v", err)
	}
}

func TestLoadManifest_UnsupportedSchema(t *testing.T) {
	root := makePlugin(t, `{"$schema":"https://example.com/v9.schema.json","name":"demo-plugin"}`)
	_, _, err := loadManifest(root)
	if err == nil {
		t.Fatal("expected error for unsupported schema")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("error should mention unsupported, got %v", err)
	}
}

func TestValidatePluginName(t *testing.T) {
	cases := []struct {
		name string
		ok   bool
	}{
		{"a", true},
		{"demo-plugin", true},
		{"my.plugin.name", true},
		{"plugin123", true},
		{"", false},            // 空
		{"Demo-Plugin", false}, // 大写
		{"-plugin", false},     // 连字符开头
		{"plugin-", false},     // 连字符结尾
		{".plugin", false},     // 点开头
		{"pl--ugin", false},    // 含 --
		{"pl..ugin", false},    // 含 ..
		{"pl_ugin", false},     // 下划线非法
	}
	for _, c := range cases {
		err := validatePluginName(c.name)
		if c.ok && err != nil {
			t.Errorf("name %q: expected ok, got %v", c.name, err)
		}
		if !c.ok && err == nil {
			t.Errorf("name %q: expected error, got ok", c.name)
		}
	}
}

func TestLoadManifest_UnknownFieldReported(t *testing.T) {
	root := makePlugin(t, `{
  "$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json",
  "name": "demo-plugin",
  "future-field": "ignored"
}`)
	_, warns, err := loadManifest(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, w := range warns {
		if strings.Contains(w, "future-field") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected warning about unknown field, got %v", warns)
	}
}

func TestLoadManifest_ExtensionsNotObject(t *testing.T) {
	root := makePlugin(t, `{
  "$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json",
  "name": "demo-plugin",
  "extensions": "not-an-object"
}`)
	m, warns, err := loadManifest(root)
	if err != nil {
		t.Fatalf("extensions not object should be non-fatal: %v", err)
	}
	if m.Extensions != nil {
		t.Errorf("extensions should be nil when not object")
	}
	found := false
	for _, w := range warns {
		if strings.Contains(w, "extensions") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected warning about extensions, got %v", warns)
	}
}

// =============================================================================
// 变量展开
// =============================================================================

func TestExpandVars(t *testing.T) {
	root := "/opt/plugin"
	data := "/var/lib/plugin-data"
	got := expandVars("${PLUGIN_ROOT}/bin:${PLUGIN_DATA}/cache", root, data)
	want := "/opt/plugin/bin:/var/lib/plugin-data/cache"
	if got != want {
		t.Errorf("expandVars = %q, want %q", got, want)
	}
}

func TestValidateAndExpandSpec_StdioArgsEnvExpanded(t *testing.T) {
	root := t.TempDir()
	data := t.TempDir()
	spec := MCPServerSpec{
		Type:    TransportStdio,
		Command: "node",
		Args:    []string{"${PLUGIN_ROOT}/server.js", "--data", "${PLUGIN_DATA}"},
		Env:     map[string]string{"CONFIG": "${PLUGIN_ROOT}/config.json"},
	}
	out, err := validateAndExpandSpec(spec, root, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Args[0] != filepath.Join(root, "server.js") {
		t.Errorf("args[0] = %q, want %q", out.Args[0], filepath.Join(root, "server.js"))
	}
	if out.Args[2] != data {
		t.Errorf("args[2] = %q, want %q", out.Args[2], data)
	}
	if out.Env["CONFIG"] != filepath.Join(root, "config.json") {
		t.Errorf("env CONFIG = %q, want %q", out.Env["CONFIG"], filepath.Join(root, "config.json"))
	}
	// command 不展开占位符。
	if out.Command != "node" {
		t.Errorf("command = %q, want node", out.Command)
	}
	// cwd 省略时默认为插件根。
	if out.Cwd != root {
		t.Errorf("cwd = %q, want %q", out.Cwd, root)
	}
}

func TestValidateAndExpandSpec_CommandRelativeResolved(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "run.sh", "#!/bin/sh\n")
	spec := MCPServerSpec{Type: TransportStdio, Command: "./run.sh"}
	out, err := validateAndExpandSpec(spec, root, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Command != filepath.Join(root, "run.sh") {
		t.Errorf("command = %q, want %q", out.Command, filepath.Join(root, "run.sh"))
	}
}

// =============================================================================
// 路径逃逸
// =============================================================================

func TestValidateAndExpandSpec_CwdEscapesRoot(t *testing.T) {
	root := t.TempDir()
	spec := MCPServerSpec{Type: TransportStdio, Command: "node", Cwd: "../../etc"}
	if _, err := validateAndExpandSpec(spec, root, t.TempDir()); err == nil {
		t.Fatal("expected error for cwd escaping root")
	}
}

func TestValidateAndExpandSpec_CwdWithPlaceholderEscapes(t *testing.T) {
	root := t.TempDir()
	spec := MCPServerSpec{Type: TransportStdio, Command: "node", Cwd: "${PLUGIN_ROOT}/../../etc"}
	if _, err := validateAndExpandSpec(spec, root, t.TempDir()); err == nil {
		t.Fatal("expected error for cwd escaping via placeholder")
	}
}

func TestValidateAndExpandSpec_CommandEscapesRoot(t *testing.T) {
	root := t.TempDir()
	spec := MCPServerSpec{Type: TransportStdio, Command: "./../../bin/evil"}
	if _, err := validateAndExpandSpec(spec, root, t.TempDir()); err == nil {
		t.Fatal("expected error for command escaping root")
	}
}

func TestValidateAndExpandSpec_CwdInDataDir(t *testing.T) {
	root := t.TempDir()
	data := t.TempDir()
	spec := MCPServerSpec{Type: TransportStdio, Command: "node", Cwd: "${PLUGIN_DATA}/work"}
	out, err := validateAndExpandSpec(spec, root, data)
	if err != nil {
		t.Fatalf("cwd in PLUGIN_DATA should be allowed: %v", err)
	}
	want := filepath.Join(data, "work")
	if out.Cwd != want {
		t.Errorf("cwd = %q, want %q", out.Cwd, want)
	}
}

// =============================================================================
// HTTP 传输校验
// =============================================================================

func TestValidateHTTPSpec(t *testing.T) {
	cases := []struct {
		name    string
		spec    MCPServerSpec
		wantErr bool
	}{
		{"missing url", MCPServerSpec{Type: TransportStreamableHTTP}, true},
		{"non-loopback http", MCPServerSpec{Type: TransportStreamableHTTP, URL: "http://example.com/mcp"}, true},
		{"loopback http ok", MCPServerSpec{Type: TransportStreamableHTTP, URL: "http://127.0.0.1:8080/mcp"}, false},
		{"https ok", MCPServerSpec{Type: TransportStreamableHTTP, URL: "https://example.com/mcp"}, false},
		{"user info rejected", MCPServerSpec{Type: TransportStreamableHTTP, URL: "https://user:pass@example.com/mcp"}, true},
		{"fragment rejected", MCPServerSpec{Type: TransportStreamableHTTP, URL: "https://example.com/mcp#frag"}, true},
		{"bad scheme", MCPServerSpec{Type: TransportStreamableHTTP, URL: "ftp://example.com"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := validateAndExpandSpec(c.spec, "", "")
			if c.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !c.wantErr && err != nil {
				t.Errorf("expected ok, got %v", err)
			}
		})
	}
}

// =============================================================================
// 失败隔离
// =============================================================================

// mcpConfig 返回一份合法的 mcp.json 内容,version 与 validManifest 一致。
func mcpConfig() string {
	return `{
  "$schema": "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json",
  "mcpServers": {
    "good": {"type": "stdio", "command": "node", "args": ["server.js"]}
  }
}`
}

func TestLoad_ManifestFatal_RejectsPlugin(t *testing.T) {
	// 非法 name → 致命错误,整个插件被拒绝。
	root := makePlugin(t, `{
  "$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json",
  "name": "Bad_Name"
}`)
	p := Load(root, t.TempDir())
	if !p.IsRejected() {
		t.Fatal("plugin with bad name should be rejected")
	}
	if p.ManifestValid {
		t.Error("ManifestValid should be false on fatal manifest error")
	}
	if p.Manifest != nil {
		t.Error("Manifest should be nil on fatal error")
	}
}

func TestLoad_SkillsNotDir_DisablesSkillsOnly(t *testing.T) {
	root := makePlugin(t, validManifest())
	// skills 是文件而非目录 → skills 组件无效,但插件不被拒绝。
	writeFile(t, root, "skills", "not a directory")
	p := Load(root, t.TempDir())
	if p.IsRejected() {
		t.Fatal("plugin should not be rejected for skills type error")
	}
	if p.Skills != nil {
		t.Error("skills should be nil when skills/ is not a dir")
	}
	found := false
	for _, w := range p.Warnings {
		if strings.Contains(w, "skills component disabled") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected skills-disabled warning, got %v", p.Warnings)
	}
}

func TestLoad_SkillsLoaded(t *testing.T) {
	root := makePlugin(t, validManifest())
	writeFile(t, root, "skills/greet/SKILL.md", "# Greet\n\nA greeting skill.")
	p := Load(root, t.TempDir())
	if p.IsRejected() {
		t.Fatalf("unexpected rejection: %s", p.FatalError)
	}
	if len(p.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(p.Skills))
	}
	if p.Skills[0].Skill == nil {
		t.Fatalf("skill not loaded: %s", p.Skills[0].Error)
	}
	if p.Skills[0].Name != "greet" {
		t.Errorf("skill name = %q, want greet", p.Skills[0].Name)
	}
}

func TestLoad_MCPInvalid_DisablesMCPOnly(t *testing.T) {
	root := makePlugin(t, validManifest())
	// mcp.json 含未知顶层字段 → 顶层无效,整个 MCP 禁用。
	writeFile(t, root, "mcp.json", `{
  "$schema": "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json",
  "mcpServers": {},
  "unknown": true
}`)
	writeFile(t, root, "skills/greet/SKILL.md", "# Greet\n")
	p := Load(root, t.TempDir())
	if p.IsRejected() {
		t.Fatalf("plugin should not be rejected: %s", p.FatalError)
	}
	if !p.MCPDisabled {
		t.Error("MCP should be disabled when top-level mcp.json invalid")
	}
	// skills 不受 MCP 失败影响。
	if len(p.Skills) != 1 {
		t.Errorf("skills should still load, got %d", len(p.Skills))
	}
}

func TestLoad_VersionMismatch_DisablesMCP(t *testing.T) {
	root := makePlugin(t, validManifest())
	// mcp.json 用 2.0.0 schema,与 plugin.json 的 1.0.0 不匹配。
	writeFile(t, root, "mcp.json", `{
  "$schema": "https://agent-plugins.org/schemas/2.0.0/mcp.schema.json",
  "mcpServers": {}
}`)
	p := Load(root, t.TempDir())
	if !p.MCPDisabled {
		t.Error("MCP should be disabled on version mismatch")
	}
}

func TestLoad_MCPEntryFailureIsolated(t *testing.T) {
	root := makePlugin(t, validManifest())
	// good 条目合法;bad 条目 cwd 逃逸 → 仅 bad 条目出错,good 条目正常。
	writeFile(t, root, "mcp.json", `{
  "$schema": "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json",
  "mcpServers": {
    "good": {"type": "stdio", "command": "node", "args": ["s.js"]},
    "bad":  {"type": "stdio", "command": "node", "cwd": "../../etc"}
  }
}`)
	p := Load(root, t.TempDir())
	if p.IsRejected() {
		t.Fatalf("unexpected rejection: %s", p.FatalError)
	}
	if p.MCPDisabled {
		t.Fatal("MCP should not be disabled for entry-level error")
	}
	if len(p.MCPServers) != 2 {
		t.Fatalf("expected 2 MCP entries, got %d", len(p.MCPServers))
	}
	badOK, goodOK := false, false
	for _, e := range p.MCPServers {
		if e.Name == "bad" && e.Error != "" {
			badOK = true
		}
		if e.Name == "good" && e.Error == "" {
			goodOK = true
		}
	}
	if !badOK {
		t.Error("bad entry should have an error")
	}
	if !goodOK {
		t.Error("good entry should have no error")
	}
}

func TestLoad_NoMCPConfig(t *testing.T) {
	root := makePlugin(t, validManifest())
	p := Load(root, t.TempDir())
	if p.IsRejected() {
		t.Fatalf("unexpected rejection: %s", p.FatalError)
	}
	if p.MCPDisabled {
		t.Error("MCP should not be disabled when mcp.json is simply absent")
	}
	if len(p.MCPServers) != 0 {
		t.Errorf("expected 0 MCP servers, got %d", len(p.MCPServers))
	}
}

func TestSchemaVersion(t *testing.T) {
	if v := schemaVersion(PluginSchemaURL); v != SpecVersion {
		t.Errorf("plugin schema version = %q, want %q", v, SpecVersion)
	}
	if v := schemaVersion(MCPSchemaURL); v != SpecVersion {
		t.Errorf("mcp schema version = %q, want %q", v, SpecVersion)
	}
	if v := schemaVersion("https://agent-plugins.org/schemas/2.0.0/mcp.schema.json"); v != "2.0.0" {
		t.Errorf("version = %q, want 2.0.0", v)
	}
}
