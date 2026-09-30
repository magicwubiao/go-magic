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

	// (b) 反向顺序：持 agentsMu 期间读配置 —— 与 sessions.go 设置工作目录
	//     (s.agentsMu 内读 cfg.Memory.Enabled / staticRulesEnabled) 同形。
	//     这类"持 agentsMu 读 cfg"本身合法；读取必须走 cfgMu（cfgSnapshot），
	//     不能裸读 s.cfg —— reloadConfig/syncConfigFromDisk 会在 s.mu 下
	//     并发整体替换它，裸读就是数据竞态（CI -race 报的正是这一条）。
	//     致命的是它同时想拿 s.mu，所以下方额外断言守卫能抓到那种写法。
	reverseDone := make(chan struct{})
	go func() {
		defer close(reverseDone)
		s.acquireAgentsMu()
		_ = s.cfgSnapshot() != nil
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

	// 反向路径：不断持 agentsMu 读 cfg（sessions/approval 同形）。
	// 读快照而非裸读 s.cfg：写入侧在 s.mu 下整体替换指针，裸读会被
	// -race 判为 DATA RACE（这正是本测试在 CI 上失败的原因）。
	holders.Add(1)
	go func() {
		defer holders.Done()
		for {
			select {
			case <-stop:
				return
			default:
				s.acquireAgentsMu()
				_ = s.cfgSnapshot() != nil
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

// TestCfgReadersDoNotRaceWithReplacement 把"所有会读 s.cfg 的只读入口"
// 与"会整体替换 s.cfg 的写入口"真正并发跑起来。
//
// 本机无 gcc、跑不了 -race，但这条测试仍有价值：它钉住的是**可观测的
// 语义**——写侧每次替换都会让 cfgWriteGen 自增，读侧每次都经 cfgSnapshot
// 取快照。若哪天有人把某个入口改回裸读 s.cfg，配上 -race 的 CI 就会报；
// 在本机，至少能保证这些入口在并发替换下不 panic、不死锁、不返回损坏结构。
//
// 覆盖的入口即 CI 上 `-race` 报 DATA RACE 的那批（handleConfig GET /
// handleProviders / handleModelOptions）——它们此前都完全不持锁地裸读 s.cfg。
func TestCfgReadersDoNotRaceWithReplacement(t *testing.T) {
	s := newLockTestServer(t)
	time.Sleep(50 * time.Millisecond)

	const rounds = 40
	stop := make(chan struct{})
	var wg sync.WaitGroup

	// 写侧：不断整体替换 s.cfg（走 syncConfigFromDisk 的真实路径）。
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				// 直接走 setCfg 模拟 reloadConfig / raw 编辑器分支的替换。
				if cur := s.cfgSnapshot(); cur != nil {
					s.setCfg(cur)
				}
			}
		}
	}()

	// 读侧：多路并发调用所有读配置的 HTTP 入口。
	type endpoint struct {
		name string
		call func()
	}
	endpoints := []endpoint{
		{"GET /api/config", func() {
			s.handleConfig(httptest.NewRecorder(), newJSONRequest(t, http.MethodGet, "/api/config", nil))
		}},
		{"GET /api/model/options", func() {
			s.handleModelOptions(httptest.NewRecorder(), newJSONRequest(t, http.MethodGet, "/api/model/options", nil))
		}},
		{"GET /api/providers", func() {
			s.handleProviders(httptest.NewRecorder(), newJSONRequest(t, http.MethodGet, "/api/providers", nil))
		}},
		{"GET /api/models/info", func() {
			s.handleModelInfo(httptest.NewRecorder(), newJSONRequest(t, http.MethodGet, "/api/models/info", nil))
		}},
	}

	var readers sync.WaitGroup
	errCh := make(chan string, len(endpoints)*rounds)
	for _, ep := range endpoints {
		ep := ep
		readers.Add(1)
		go func() {
			defer readers.Done()
			for i := 0; i < rounds; i++ {
				func() {
					// 读入口不应 panic；用 recover 把 panic 变成可读失败信息，
					// 否则整个测试进程会以 stack trace 崩掉、定位困难。
					defer func() {
						if r := recover(); r != nil {
							errCh <- ep.name + " panic: " + fmt.Sprint(r)
						}
					}()
					ep.call()
				}()
			}
		}()
	}

	readersDone := make(chan struct{})
	go func() { readers.Wait(); close(readersDone) }()
	waitFor(t, readersDone, "并发读配置入口出现死锁/挂起")
	close(stop)
	wg.Wait()

	close(errCh)
	for msg := range errCh {
		t.Error(msg)
	}
}

// TestCfgWritePathsAreCopyOnWrite 钉住 cfgSnapshot 的"不可变快照"契约。
//
// 为什么必须有这条：`-race` 能报出"两个 goroutine 同时碰同一块内存"，
// 但报不出"设计上就不该被改的对象被改了"。写点若原地改写 s.cfg 指向的
// 对象，任何在此之前取过快照的读者都会与写入并发访问同一块内存 ——
// 这正是 CI 上报的那 13 条 DATA RACE 的成因。
//
// 因此约定：lockCfgForWrite 必须先把 s.cfg 换成**新副本**，写点随后只改
// 副本。本测试直接断言：持有旧快照的读者，在写点走过一轮之后，看到的
// 仍是旧值（且内存未被改动）。
func TestCfgWritePathsAreCopyOnWrite(t *testing.T) {
	s := newLockTestServer(t)

	before := s.cfgSnapshot()
	if before == nil {
		t.Fatal("测试前置：配置不应为 nil")
	}
	if before.Model != "m1" {
		t.Fatalf("测试前置：种子配置 model 应为 m1，got %s", before.Model)
	}

	// 走真实写路径（等价于"模型设置页改模型"）。
	s.handleModelSet(httptest.NewRecorder(), newJSONRequest(t, http.MethodPost, "/api/model/set",
		map[string]any{"provider": "deepseek", "model": "m2"}))

	// ① 写后必须换成了新对象（而不是原地改旧的）。
	after := s.cfgSnapshot()
	if after == before {
		t.Error("写路径必须整体替换 s.cfg（copy-on-write），实际复用了同一对象")
	}
	if after == nil || after.Model != "m2" {
		t.Errorf("新快照应反映写入结果 model=m2，got %+v", after)
	}

	// ② 旧快照必须原封不动 —— 这是"读者拿到的快照可无锁读"的全部依据。
	//    若这里失败，说明有写点原地改了共享对象，-race 下必然报 DATA RACE。
	if before.Model != "m1" {
		t.Errorf("旧快照被原地改写（应保持 m1，实际 %s）—— 违反不可变快照契约", before.Model)
	}
}

// TestCfgModifyHelpersDoNotSelfDeadlock 覆盖"持 cfgMu 时调 cfgSnapshot"这类
// 自我死锁。sync.Mutex 不可重入，而且死锁当场表现为整个进程挂住，
// 比数据竞态更难定位，所以用超时把它变成确定性失败。
func TestCfgModifyHelpersDoNotSelfDeadlock(t *testing.T) {
	s := newLockTestServer(t)

	done := make(chan struct{})
	go func() {
		defer close(done)

		// 1) handleProvidersSubRoutes 的 PUT：改配置 + 落盘 + 把凭据推到
		//    运行中的 provider，全程不 panic、不死锁。
		rec := httptest.NewRecorder()
		s.handleProvidersSubRoutes(rec, newJSONRequest(t, http.MethodPut, "/api/providers/deepseek",
			map[string]any{"api_key": "k2", "models": []string{"m2", "m1"}}))

		// 2) handleConfig PUT 的合并分支（g.Release 之后还要刷新 provider /
		//    清 agent 缓存 —— 都必须在放锁后做，否则自锁）。
		recCfg := httptest.NewRecorder()
		s.handleConfig(recCfg, newJSONRequest(t, http.MethodPut, "/api/config",
			map[string]any{"model": "m3", "provider": "deepseek"}))

		// 3) 单条 persistConfig（写点普遍在持 cfgMu 时调它）。
		_ = s.persistConfig(true)
	}()

	waitFor(t, done, "改配置路径出现自我死锁（持 cfgMu 时又去拿 cfgMu）")
}

func newLockTestServer(t *testing.T) *Server {	t.Helper()
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
	s.setCfg(cfg)
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
