package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 回归：模型供应商页点"保存"后整个后端卡死（直到重启进程）。
//
// 链路：前端 configStore.saveProvider 先 GET /api/providers/{name} 探存在性，
// 再 PUT/POST 保存。而 handleProvidersSubRoutes 入口就 lockCfgWriteGuard()
// （s.mu + cfgMu），GET 分支里又调 s.cfgSnapshot() —— 后者要再拿 cfgMu，
// sync.RWMutex 不可重入 ⇒ 自我死锁：该 goroutine 永久卡住，s.mu 也不再释放，
// 于是后续所有请求（含 /api/config、聊天）一起排队挂死。
//
// POST（新建供应商）与 DELETE（删除）分支同形，因此添加/删除供应商同样会卡死。
//
// 修法：锁窗口内一律直接读 s.cfg（lockCfgForWrite 已换成写点私有副本，
// 与 PUT / test / fetch-models 三个分支的既有写法一致）。
func TestProvidersGetDoesNotSelfDeadlock(t *testing.T) {
	s := newLockTestServer(t)

	done := make(chan struct{})
	var rec *httptest.ResponseRecorder
	go func() {
		defer close(done)
		rec = httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/providers/deepseek", nil)
		s.handleProvidersSubRoutes(rec, req)
	}()
	waitFor(t, done, "GET /api/providers/{name} 卡死（cfgMu 自我死锁）")

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/providers/deepseek = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var info struct {
		Name    string   `json:"name"`
		BaseURL string   `json:"base_url"`
		APIKey  string   `json:"api_key"`
		Models  []string `json:"models"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatalf("decode GET response: %v (body=%s)", err, rec.Body.String())
	}
	if info.Name != "deepseek" || len(info.Models) != 2 {
		t.Errorf("GET 返回内容不对: %+v", info)
	}
	if info.APIKey == "" || info.APIKey == "k1" {
		t.Errorf("api_key 应打码返回，实际 %q", info.APIKey)
	}
}

// POST（新建供应商）与 DELETE（删除）也必须不卡死，且真的改到配置。
func TestProvidersCreateAndDeleteDoNotSelfDeadlock(t *testing.T) {
	s := newLockTestServer(t)

	// --- POST 新建 ---
	done := make(chan struct{})
	var rec *httptest.ResponseRecorder
	go func() {
		defer close(done)
		rec = httptest.NewRecorder()
		req := newJSONRequest(t, http.MethodPost, "/api/providers/custom1", map[string]any{
			"name":     "custom1",
			"base_url": "https://example.com/v1",
			"api_key":  "sk-new",
			"models":   []string{"cm1"},
		})
		s.handleProvidersSubRoutes(rec, req)
	}()
	waitFor(t, done, "POST /api/providers/{name} 卡死（cfgMu 自我死锁）")

	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	cfg := s.cfgSnapshot()
	if cfg == nil || cfg.Providers["custom1"].APIKey != "sk-new" {
		t.Fatalf("POST 未写入配置: %+v", cfg)
	}

	// --- DELETE 删除 ---
	done2 := make(chan struct{})
	var rec2 *httptest.ResponseRecorder
	go func() {
		defer close(done2)
		rec2 = httptest.NewRecorder()
		s.handleProvidersSubRoutes(rec2, httptest.NewRequest(http.MethodDelete, "/api/providers/custom1", nil))
	}()
	waitFor(t, done2, "DELETE /api/providers/{name} 卡死（cfgMu 自我死锁）")

	if rec2.Code != http.StatusOK {
		t.Fatalf("DELETE = %d, want 200 (body=%s)", rec2.Code, rec2.Body.String())
	}
	if _, exists := s.cfgSnapshot().Providers["custom1"]; exists {
		t.Error("DELETE 未从配置里移除该供应商")
	}
}

// 前端保存流程的端到端形态：GET 探存在 → PUT 保存 → 再 GET 复核。
// 死锁会让第一个 GET 就挂住，因此这里同样是"保存卡死"的直接回归。
func TestProviderSaveFlowCompletes(t *testing.T) {
	s := newLockTestServer(t)

	done := make(chan struct{})
	go func() {
		defer close(done)

		// (1) GET 探存在性
		rec := httptest.NewRecorder()
		s.handleProvidersSubRoutes(rec, httptest.NewRequest(http.MethodGet, "/api/providers/deepseek", nil))
		if rec.Code != http.StatusOK {
			t.Errorf("probe GET = %d", rec.Code)
			return
		}

		// (2) PUT 保存（新增一个模型并置为当前模型）
		rec2 := httptest.NewRecorder()
		s.handleProvidersSubRoutes(rec2, newJSONRequest(t, http.MethodPut, "/api/providers/deepseek", map[string]any{
			"models": []string{"m3", "m1", "m2"},
		}))
		if rec2.Code != http.StatusOK {
			t.Errorf("PUT = %d (body=%s)", rec2.Code, rec2.Body.String())
			return
		}

		// (3) GET 复核
		rec3 := httptest.NewRecorder()
		s.handleProvidersSubRoutes(rec3, httptest.NewRequest(http.MethodGet, "/api/providers/deepseek", nil))
		if rec3.Code != http.StatusOK {
			t.Errorf("verify GET = %d", rec3.Code)
			return
		}
	}()
	waitFor(t, done, "供应商保存流程卡死（cfgMu 自我死锁）")

	cfg := s.cfgSnapshot()
	if cfg == nil {
		t.Fatal("cfg nil")
	}
	if got := cfg.Providers["deepseek"].Models; len(got) != 3 || got[0] != "m3" {
		t.Errorf("保存后模型列表未生效: %v", got)
	}
	if cfg.Provider != "deepseek" || cfg.Model != "m3" {
		t.Errorf("当前模型未更新: provider=%s model=%s", cfg.Provider, cfg.Model)
	}
}
