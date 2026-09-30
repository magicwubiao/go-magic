package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	appconfig "github.com/magicwubiao/go-magic/pkg/config"
)

// 回归：chat 页切换模型后（POST /api/model/set），再打开模型供应商页
// （GET /api/model/options、GET /api/config）整个后端卡死，直到进程重启。
//
// 两个独立缺陷叠加：
//  1. **自我死锁**：handleModelSet 持 s.mu 时调 persistConfig，后者经
//     markConfigMtime 再拿一次 s.mu —— sync.RWMutex 不可重入。
//     症状极具误导性：s.mu 被一个"看起来只是在写文件"的调用持住，
//     而 agentsMu 空闲，容易误判成别处死锁。
//  2. **AB-BA 死锁**：handleModelSet 持 s.mu 时调 syncConfigFromDisk，
//     后者要拿 s.agentsMu；反向路径（approval/sessions）持 agentsMu 再读
//     s.cfg。两条相反顺序叠起来，此后所有请求永久挂起。
//
// 修复：syncConfigFromDisk 的 mtime 基线改用 atomic（完全不碰 s.mu）、
// 清 agent 放到释放 s.mu 之后；handleModelSet 先 sync 再进临界区；
// persistConfig 不再要求 s.mu。
//
// 下面的测试在"复现缺陷"时会由 lockorder 断言直接 panic，而不再需要靠
// 调度交错碰运气——见 TestLockOrderGuardCatchesInvertedOrder。
func TestModelSetDoesNotDeadlockWithAgentsMu(t *testing.T) {
	s := newLockTestServer(t)
	// 让 syncConfigFromDisk 真的走进"重载 + 清空 agent"分支（mtime 前进）。
	time.Sleep(50 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		defer close(done)
		// (a) chat 页切换模型 → 全量切换供应商分支。
		s.handleModelSet(
			httptest.NewRecorder(),
			newJSONRequest(t, http.MethodPost, "/api/model/set", map[string]any{
				"provider": "deepseek",
				"model":    "m2",
			}),
		)
	}()

	// (b) 反向顺序：持 agentsMu 期间读 s.cfg —— 与 sessions.go 设置工作目录
	//     (s.agentsMu 内读 s.cfg.Memory.Enabled / staticRulesEnabled) 同形。
	//     这类"持 agentsMu 读 cfg"本身合法；致命的是它同时想拿 s.mu，
	//     所以下方额外断言守卫能抓到那种写法。
	reverseDone := make(chan struct{})
	go func() {
		defer close(reverseDone)
		s.acquireAgentsMu()
		_ = s.cfg != nil && s.cfg.Provider != ""
		s.releaseAgentsMu()
	}()

	waitFor(t, done, "handleModelSet 被死锁")
	waitFor(t, reverseDone, "持 agentsMu 的反向路径被死锁")
}

// 守卫必须能抓到反序写法，否则"把 bug 悄悄写回去"会以永久死锁的形式
// 在线上复现，而单元测试全绿。两条断言各覆盖一半的反序。
func TestLockOrderGuardCatchesInvertedOrder(t *testing.T) {
	t.Run("持 agentsMu 再拿 s.mu", func(t *testing.T) {
		s := newLockTestServer(t)
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("持 agentsMu 再拿 s.mu 应触发 lockorder panic，实际没有")
			}
			if msg, ok := r.(string); ok && !strings.Contains(msg, "lockorder") {
				t.Fatalf("panic 信息应标明 lockorder 违规，got: %s", msg)
			}
		}()

		s.acquireAgentsMu()
		defer s.releaseAgentsMu()
		s.acquireServerMu("test")
	})

	t.Run("持 s.mu 调 syncConfigFromDisk", func(t *testing.T) {
		s := newLockTestServer(t)
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("持 s.mu 调 syncConfigFromDisk 应触发 lockorder panic，实际没有")
			}
			if msg, ok := r.(string); ok && !strings.Contains(msg, "lockorder") {
				t.Fatalf("panic 信息应标明 lockorder 违规，got: %s", msg)
			}
		}()

		s.acquireServerMu("test")
		defer s.releaseServerMu()
		// 这就是 handleModelSet 曾经的写法（修复前会永久死锁）。
		s.syncConfigFromDisk()
	})
}

// 同一场景的端到端形态：切完模型后紧接着打开模型供应商页（两个只读接口），
// 三者串行也必须能全部返回。修复前 handleModelSet 自身就会挂住。
func TestModelSetThenProviderPageResponds(t *testing.T) {
	s := newLockTestServer(t)
	time.Sleep(50 * time.Millisecond)

	done := make(chan struct{})
	var errs []string
	go func() {
		defer close(done)
		s.handleModelSet(
			httptest.NewRecorder(),
			newJSONRequest(t, http.MethodPost, "/api/model/set", map[string]any{
				"provider": "deepseek",
				"model":    "m2",
			}),
		)
		// 模型供应商页 onMounted：先 GET /api/config，再 GET /api/model/options。
		configRec := httptest.NewRecorder()
		s.handleConfig(configRec, newJSONRequest(t, http.MethodGet, "/api/config", nil))
		if configRec.Code != http.StatusOK {
			errs = append(errs, fmt.Sprintf("GET /api/config status=%d", configRec.Code))
		}

		optRec := httptest.NewRecorder()
		s.handleModelOptions(optRec, newJSONRequest(t, http.MethodGet, "/api/model/options", nil))
		if optRec.Code != http.StatusOK {
			errs = append(errs, fmt.Sprintf("GET /api/model/options status=%d", optRec.Code))
		}
		var body struct {
			Provider string `json:"provider"`
			Model    string `json:"model"`
		}
		if err := json.Unmarshal(optRec.Body.Bytes(), &body); err != nil {
			errs = append(errs, fmt.Sprintf("model/options 响应不是合法 JSON: %v", err))
		} else if body.Provider != "deepseek" || body.Model != "m2" {
			errs = append(errs, fmt.Sprintf(
				"切换后 /api/model/options 应回显新值，got %s/%s", body.Provider, body.Model))
		}
	}()

	waitFor(t, done, "切换模型后打开供应商页被死锁")
	for _, e := range errs {
		t.Error(e)
	}
}

// 清空 agent 缓存必须经 agentsMu：并发读 agent（getOrCreateAgent）的同时
// 裸写 map 会触发 fatal "concurrent map writes"。
func TestClearAgentsIsConcurrencySafe(t *testing.T) {
	s := newLockTestServer(t)

	stop := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			select {
			case <-stop:
				return
			default:
				_ = s.lookupAgent("session-x")
			}
		}
	}()

	for i := 0; i < 200; i++ {
		s.clearAgents()
	}
	close(stop)
	waitFor(t, readerDone, "并发读 agent 卡住")
}

// 压力形态：并发跑"切模型 / 供应商页只读 / 持 agentsMu 读 cfg"，
// 反复多轮。修复前这里会随机挂死（本机无 gcc，-race 不可用，
// 只能靠真实并发把死锁逼出来）。
func TestModelSwitchUnderConcurrentLoad(t *testing.T) {
	s := newLockTestServer(t)
	time.Sleep(50 * time.Millisecond)

	const rounds = 30
	stop := make(chan struct{})
	var holders sync.WaitGroup

	// 反向路径：不断持 agentsMu 读 cfg（sessions/approval 同形）
	holders.Add(1)
	go func() {
		defer holders.Done()
		for {
			select {
			case <-stop:
				return
			default:
				s.acquireAgentsMu()
				_ = s.cfg != nil
				s.releaseAgentsMu()
			}
		}
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < rounds; i++ {
			s.handleModelSet(httptest.NewRecorder(), newJSONRequest(t, http.MethodPost, "/api/model/set",
				map[string]any{"provider": "deepseek", "model": "m2"}))

			s.handleModelOptions(httptest.NewRecorder(), newJSONRequest(t, http.MethodGet, "/api/model/options", nil))
			s.handleProviders(httptest.NewRecorder(), newJSONRequest(t, http.MethodGet, "/api/providers", nil))
			s.handleConfig(httptest.NewRecorder(), newJSONRequest(t, http.MethodGet, "/api/config", nil))
		}
	}()

	waitFor(t, done, "并发压力下出现死锁")
	close(stop)
	holders.Wait()
}

func newLockTestServer(t *testing.T) *Server {
	t.Helper()
	// 打开加锁顺序断言：任何"持 agentsMu 再拿 s.mu"的反序都会立即 panic，
	// 而不是在特定交错下悄悄变成永久死锁。
	EnableStrictLockOrder(true)
	t.Cleanup(func() { EnableStrictLockOrder(false) })

	tmp := t.TempDir()
	t.Setenv("GO_MAGIC_HOME", tmp)

	cfgPath := filepath.Join(tmp, "config.json")
	seed := `{"provider":"deepseek","model":"m1","providers":{"deepseek":{"api_key":"k1","models":["m1","m2"]}}}`
	if err := os.WriteFile(cfgPath, []byte(seed), 0600); err != nil {
		t.Fatal(err)
	}

	s := &Server{magicHome: tmp}
	cfg, err := appconfig.Load()
	if err != nil {
		t.Fatal(err)
	}
	s.cfg = cfg
	s.markConfigMtime()
	return s
}

func waitFor(t *testing.T, done <-chan struct{}, msg string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		panic("deadlock: " + msg + "（超过 10s 未完成）")
	}
}

func newJSONRequest(t *testing.T, method, target string, body any) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, target, &buf)
	req.Header.Set("Content-Type", "application/json")
	return req
}
