package agentplugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/cliconn"
	"github.com/magicwubiao/go-magic/pkg/clicatalog"
	"github.com/magicwubiao/go-magic/pkg/connectors"
)

// isolatedHome 把 magic home 指到临时目录，避免测试写到真实的 ~/.magic。
func isolatedHome(t *testing.T) {
	t.Helper()
	t.Setenv("GO_MAGIC_HOME", t.TempDir())
}

// 目录里的每一条都必须能列出来，且插件名唯一、不违反 plugin 名规则。
func TestCatalogPacksCoverAllEntries(t *testing.T) {
	isolatedHome(t)

	all := CatalogPacks("")
	want := len(connectors.All()) + len(clicatalog.All())
	if len(all) != want {
		t.Fatalf("CatalogPacks(len) = %d, want %d (mcp %d + cli %d)",
			len(all), want, len(connectors.All()), len(clicatalog.All()))
	}

	seen := map[string]bool{}
	for _, p := range all {
		if seen[p.Name] {
			t.Errorf("duplicate plugin name %q", p.Name)
		}
		seen[p.Name] = true
		if err := validatePluginName(p.Name); err != nil {
			t.Errorf("pack %s/%s has invalid plugin name %q: %v", p.Kind, p.ID, p.Name, err)
		}
		if p.Kind != CatalogKindMCP && p.Kind != CatalogKindCLI {
			t.Errorf("pack %s/%s has unknown kind %q", p.Kind, p.ID, p.Kind)
		}
		if p.Installed {
			t.Errorf("pack %s should not be installed in a fresh home", p.Name)
		}
	}

	if n := len(CatalogPacks(CatalogKindMCP)); n != len(connectors.All()) {
		t.Errorf("mcp packs = %d, want %d", n, len(connectors.All()))
	}
	if n := len(CatalogPacks(CatalogKindCLI)); n != len(clicatalog.All()) {
		t.Errorf("cli packs = %d, want %d", n, len(clicatalog.All()))
	}
}

// 参数位凭据（飞书 App ID/Secret）必须转成 ${CRED:KEY}，否则物化后会丢凭据位置。
func TestCatalogPacksArgCredentials(t *testing.T) {
	isolatedHome(t)

	var lark *CatalogPack
	for _, p := range CatalogPacks(CatalogKindMCP) {
		if p.ID == "lark" {
			cp := p
			lark = &cp
			break
		}
	}
	if lark == nil {
		t.Fatal("lark pack missing")
	}
	if lark.Name != "mcp-lark" {
		t.Errorf("lark pack name = %q, want mcp-lark", lark.Name)
	}
	want := []string{"LARK_APP_ID", "LARK_APP_SECRET"}
	if strings.Join(lark.RequiredEnv, ",") != strings.Join(want, ",") {
		t.Errorf("lark required_env = %v, want %v", lark.RequiredEnv, want)
	}
	joined := strings.Join(lark.Args, " ")
	if !strings.Contains(joined, "${CRED:LARK_APP_ID}") {
		t.Errorf("lark args should reference credentials, got %v", lark.Args)
	}
	if strings.Contains(joined, "{{LARK_APP_ID}}") {
		t.Errorf("catalog placeholder left unconverted: %v", lark.Args)
	}
}

// 安装 MCP 条目：目录名 = 插件名，mcp.json 单条目，加载零错误。
func TestInstallCatalogPackMCP(t *testing.T) {
	isolatedHome(t)

	dir, name, required, err := InstallCatalogPack(CatalogKindMCP, "lark", nil)
	if err != nil {
		t.Fatalf("InstallCatalogPack: %v", err)
	}
	if name != "mcp-lark" || filepath.Base(dir) != "mcp-lark" {
		t.Fatalf("dir/name = %q/%q", dir, name)
	}
	if len(required) != 2 {
		t.Errorf("required = %v, want 2 keys", required)
	}

	// mcp.json 结构：单 server，类型 stdio，args 里是 ${CRED:}
	raw, err := os.ReadFile(filepath.Join(dir, mcpConfigName))
	if err != nil {
		t.Fatalf("read mcp.json: %v", err)
	}
	var doc MCPConfig
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse mcp.json: %v", err)
	}
	if len(doc.MCPServers) != 1 {
		t.Fatalf("mcpServers = %d, want 1", len(doc.MCPServers))
	}
	spec := doc.MCPServers["lark"]
	if spec.Type != TransportStdio || spec.Command != "npx" {
		t.Errorf("spec = %+v", spec)
	}
	if !strings.Contains(strings.Join(spec.Args, " "), "${CRED:LARK_APP_SECRET}") {
		t.Errorf("args = %v", spec.Args)
	}

	// 真实加载：无致命错误、无条目错误，凭据 key 能被识别。
	p := Load(dir, filepath.Join(dir, "data"))
	if err := verifyLoadedPack(p); err != nil {
		t.Fatalf("loaded pack invalid: %v", err)
	}
	if got := SpecCredentialKeys(p.MCPServers[0].Spec); len(got) != 2 {
		t.Errorf("loaded required keys = %v, want 2", got)
	}

	// 重复安装：必须拒绝（调用方据此返回 409）。
	if _, _, _, err := InstallCatalogPack(CatalogKindMCP, "lark", nil); err == nil {
		t.Error("second install should fail with already exists")
	}
}

// 安装 CLI 条目：cli.json 单连接器（目录现役条目均无凭据占位，
// Requires → env 占位的映射由 TestBuildCLIConnectorEnvPlaceholders 单独覆盖）。
func TestInstallCatalogPackCLI(t *testing.T) {
	isolatedHome(t)

	dir, name, _, err := InstallCatalogPack(CatalogKindCLI, "jq", nil)
	if err != nil {
		t.Fatalf("InstallCatalogPack: %v", err)
	}
	if name != "cli-jq" {
		t.Fatalf("name = %q, want cli-jq", name)
	}
	if _, err := os.Stat(filepath.Join(dir, cliConfigName)); err != nil {
		t.Fatalf("cli.json missing: %v", err)
	}

	p := Load(dir, filepath.Join(dir, "data"))
	if err := verifyLoadedPack(p); err != nil {
		t.Fatalf("loaded pack invalid: %v", err)
	}
	if len(p.CLIConnectors) != 1 {
		t.Fatalf("cli connectors = %d, want 1", len(p.CLIConnectors))
	}
	conn := p.CLIConnectors[0]
	if conn.Connector.Command != "jq" {
		t.Errorf("command = %q", conn.Connector.Command)
	}
	if keys := envPlaceholderKeys(conn.Connector.Env); len(keys) != 0 {
		t.Errorf("expected no credential placeholders, got env %v", conn.Connector.Env)
	}
}

// Requires → 空 env 占位的映射：目录里的条目都是零凭据的，用合成条目验证。
func TestBuildCLIConnectorEnvPlaceholders(t *testing.T) {
	e := clicatalog.Entry{
		ID:          "fake",
		DefaultName: "fake",
		Connector: cliconn.Connector{
			Command:     "fake",
			Description: "fake",
			Tools: []cliconn.ToolSpec{{
				Name:        "ping",
				Description: "ping",
				Args:        []string{"ping"},
			}},
		},
		Requires: []clicatalog.Requirement{
			{Key: "FAKE_TOKEN", Label: "token", Secret: true},
			{Key: "FAKE_REGION", Label: "region"},
		},
	}
	conn := buildCLIConnector(e)
	if keys := envPlaceholderKeys(conn.Env); len(keys) != 2 || keys[0] != "FAKE_REGION" || keys[1] != "FAKE_TOKEN" {
		t.Errorf("envPlaceholderKeys(%v) = %v, want [FAKE_REGION FAKE_TOKEN]", conn.Env, keys)
	}
}

// 参数覆盖：postgres/sqlite 这类条目的路径/连接串必须能在安装时替换。
func TestInstallCatalogPackArgsOverride(t *testing.T) {
	isolatedHome(t)

	args := []string{"-y", "@modelcontextprotocol/server-postgres", "postgresql://db.example.com/prod"}
	dir, _, _, err := InstallCatalogPack(CatalogKindMCP, "postgres", args)
	if err != nil {
		t.Fatalf("InstallCatalogPack: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, mcpConfigName))
	if err != nil {
		t.Fatalf("read mcp.json: %v", err)
	}
	var doc MCPConfig
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse mcp.json: %v", err)
	}
	spec := doc.MCPServers["postgres"]
	if len(spec.Args) != 3 || spec.Args[2] != "postgresql://db.example.com/prod" {
		t.Errorf("args override not applied: %v", spec.Args)
	}
}

// {{KEY}} → ${CRED:KEY} 的转换只认合法参数名，普通文本原样保留。
func TestCatalogParamToCredRef(t *testing.T) {
	cases := map[string]string{
		"--jira-url={{JIRA_URL}}": "--jira-url=${CRED:JIRA_URL}",
		"{{A}}{{B}}":              "${CRED:A}${CRED:B}",
		"plain-arg":               "plain-arg",
		"weird{{not a param}}":    "weird{{not a param}}",
		"braces {{}}":             "braces {{}}",
	}
	for in, want := range cases {
		if got := catalogParamToCredRef(in); got != want {
			t.Errorf("catalogParamToCredRef(%q) = %q, want %q", in, got, want)
		}
	}
}

// 插件名归一化：目录 ID 可能带大写/下划线，plugin 名只允许小写字母数字点连字符。
func TestSanitizePackSegment(t *testing.T) {
	cases := map[string]string{
		"gh":           "gh",
		"AWS_CLI":      "aws-cli",
		"brave-search": "brave-search",
		"a__b":         "a-b",
		"---x---":      "x",
		"中文":           "pack",
	}
	for in, want := range cases {
		if got := sanitizePackSegment(in); got != want {
			t.Errorf("sanitizePackSegment(%q) = %q, want %q", in, got, want)
		}
	}
}
