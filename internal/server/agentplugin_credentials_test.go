package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/magicwubiao/go-magic/internal/agentplugin"
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
