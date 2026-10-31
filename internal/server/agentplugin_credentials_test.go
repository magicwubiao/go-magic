package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/magicwubiao/go-magic/internal/agentplugin"
	"github.com/magicwubiao/go-magic/internal/cliconn"
	appconfig "github.com/magicwubiao/go-magic/pkg/config"
)

// 目录装出来的插件，凭据表单必须带"去哪拿"的直链：前端据此把
// "去获取"渲染在输入框旁边，用户点一下就到厂商的 token 页面。
// 手写插件没有目录元数据时退化为纯 key，不能因此报错或漏项。
func TestPluginCredentialsExposeCredentialFields(t *testing.T) {
	t.Setenv("GO_MAGIC_HOME", t.TempDir())

	catalog := &agentplugin.Plugin{
		Manifest: &agentplugin.Manifest{Name: "mcp-github"},
		MCPServers: []agentplugin.MCPEntryStatus{{
			Name: "github",
			Spec: agentplugin.MCPServerSpec{
				Type:    agentplugin.TransportStdio,
				Command: "npx",
				Env:     map[string]string{"GITHUB_PERSONAL_ACCESS_TOKEN": ""},
			},
		}},
		Extensions: map[string]any{
			"io.github.magicwubiao.catalog": map[string]any{"kind": "mcp", "id": "github"},
		},
	}
	handwritten := &agentplugin.Plugin{
		Manifest: &agentplugin.Manifest{Name: "custom"},
		MCPServers: []agentplugin.MCPEntryStatus{{
			Name: "svc",
			Spec: agentplugin.MCPServerSpec{
				Type:    agentplugin.TransportStdio,
				Command: "x",
				Env:     map[string]string{"MY_TOKEN": ""},
			},
		}},
	}
	plugins := map[string]*agentplugin.ManagedPlugin{
		"mcp-github": {Plugin: catalog},
		"custom":     {Plugin: handwritten},
	}
	s := &Server{cfg: &appconfig.Config{}, agentPlugins: plugins}

	fieldsOf := func(name string) map[string][]agentplugin.CredentialField {
		t.Helper()
		rec := httptest.NewRecorder()
		s.handlePluginCredentials(rec, httptest.NewRequest(http.MethodGet, "/", nil), name)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: code = %d body=%s", name, rec.Code, rec.Body.String())
		}
		var body struct {
			Fields map[string][]agentplugin.CredentialField `json:"fields"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: decode: %v (%s)", name, err, rec.Body.String())
		}
		return body.Fields
	}

	got := fieldsOf("mcp-github")["github"]
	if len(got) != 1 {
		t.Fatalf("catalog fields = %+v", got)
	}
	if got[0].Label == "" || got[0].HelpURL != "https://github.com/settings/tokens" {
		t.Fatalf("catalog field lost its metadata: %+v", got[0])
	}

	if plain := fieldsOf("custom")["svc"]; len(plain) != 1 || plain[0].Key != "MY_TOKEN" {
		t.Fatalf("handwritten fields = %+v", plain)
	}

	// blocker 也要带同样的元数据：启用前的引导弹窗直接给"去获取凭据"。
	blockers := s.pluginBlockers("mcp-github", plugins["mcp-github"])
	if len(blockers) != 1 || blockers[0].Kind != "auth" {
		t.Fatalf("blockers = %+v", blockers)
	}
	if len(blockers[0].Fields) != 1 || blockers[0].Fields[0].HelpURL == "" {
		t.Fatalf("blocker lost help url: %+v", blockers[0].Fields)
	}
}

// 凭据表单要标出"哪些分组能一键授权"：能走 OAuth 的分组不再劝用户去网页
// 搬 token，前端在弹窗顶部直接给授权按钮。
func TestPluginCredentialsExposeOAuthGroups(t *testing.T) {
	t.Setenv("GO_MAGIC_HOME", t.TempDir())

	ghOAuth := &cliconn.OAuthSpec{
		DeviceAuthorizationEndpoint: "https://example.com/device",
		TokenEndpoint:               "https://example.com/token",
		ClientID:                    "cid",
		TokenEnv:                    "GH_TOKEN",
	}
	plugin := &agentplugin.Plugin{
		Manifest: &agentplugin.Manifest{Name: "mixed"},
		MCPServers: []agentplugin.MCPEntryStatus{{
			Name: "svc",
			Spec: agentplugin.MCPServerSpec{
				Type:    agentplugin.TransportStdio,
				Command: "x",
				Env:     map[string]string{"SVC_TOKEN": ""},
			},
		}},
		CLIConnectors: []agentplugin.CLIConnectorStatus{
			{
				Name: "gh",
				Connector: cliconn.Connector{
					Command: "gh",
					Env:     []string{"GH_TOKEN="},
					OAuth:   ghOAuth,
				},
			},
			{
				// 声明了 oauth 但没配 client_id：设备码流程起不来，
				// 不能算"可授权"，否则按钮点下去只会报错。
				Name: "glab",
				Connector: cliconn.Connector{
					Command: "glab",
					Env:     []string{"GITLAB_TOKEN="},
					OAuth:   &cliconn.OAuthSpec{TokenEnv: "GITLAB_TOKEN"},
				},
			},
		},
	}
	plugins := map[string]*agentplugin.ManagedPlugin{"mixed": {Plugin: plugin}}
	s := &Server{cfg: &appconfig.Config{}, agentPlugins: plugins}

	rec := httptest.NewRecorder()
	s.handlePluginCredentials(rec, httptest.NewRequest(http.MethodGet, "/", nil), "mixed")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		OAuth map[string]struct {
			Available bool   `json:"available"`
			State     string `json:"state"`
		} `json:"oauth"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	if !body.OAuth["cli:gh"].Available {
		t.Fatalf("cli:gh should be oauth-capable: %+v", body.OAuth)
	}
	if _, ok := body.OAuth["cli:glab"]; ok {
		t.Fatalf("unconfigured oauth must not be offered: %+v", body.OAuth)
	}
	if _, ok := body.OAuth["svc"]; ok {
		t.Fatalf("plain MCP entry must not be marked oauth: %+v", body.OAuth)
	}
}
