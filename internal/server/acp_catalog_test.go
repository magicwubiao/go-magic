package server

// acp_catalog_test.go —— 模板目录接口。
//
// catalog 是纯只读数据，测试重点：模板本身必须能通过 Validate（写错参数
// 会让"一键接入"生成一个保存不生效的 agent）、探测逻辑正确、路由可达。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/magicwubiao/go-magic/internal/acp"
)

// TestACPTemplatesValid 确保每个内置模板预填出来的配置都能过 Validate——
// catalog 数据与校验器是两处维护，这里做一致性闭环。
func TestACPTemplatesValid(t *testing.T) {
	for _, tpl := range acp.ACPTemplates() {
		cfg := acp.AgentConfig{
			Command:     tpl.Command,
			Args:        tpl.Args,
			AutoApprove: tpl.AutoApprove,
		}
		if err := cfg.Validate(tpl.ID); err != nil {
			t.Errorf("template %s: built-in fields fail validation: %v", tpl.ID, err)
		}
		for _, k := range tpl.EnvKeys {
			if k.Name == "" {
				t.Errorf("template %s: empty env key name", tpl.ID)
			}
		}
	}
}

func TestACPCatalogEndpoint(t *testing.T) {
	s, _ := newACPServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/acp/catalog", nil)
	rec := httptest.NewRecorder()
	s.handleACPCatalog(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp acp.ACPCatalogResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Templates) < 2 {
		t.Errorf("expected at least 2 built-in templates, got %d", len(resp.Templates))
	}
	// 模板顺序即展示顺序，别在维护时无意识重排。
	if resp.Templates[0].ID != "claude-code" {
		t.Errorf("first template id = %q, want claude-code", resp.Templates[0].ID)
	}

	// 每个模板声明的依赖都要出现在探测结果里。
	found := make(map[string]bool, len(resp.Runtimes))
	for _, p := range resp.Runtimes {
		found[p.Binary] = true
		if p.Found && p.Path == "" {
			t.Errorf("probe %s: found=true but empty path", p.Binary)
		}
	}
	for _, tpl := range resp.Templates {
		for _, b := range tpl.NeedsRuntime {
			if !found[b] {
				t.Errorf("template %s needs runtime %q but probe missing", tpl.ID, b)
			}
		}
	}

	if resp.Platform == "" {
		t.Error("platform is empty")
	}
}

func TestACPCatalogMethodNotAllowed(t *testing.T) {
	s, _ := newACPServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/acp/catalog", nil)
	rec := httptest.NewRecorder()
	s.handleACPCatalog(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
