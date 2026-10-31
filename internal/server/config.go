package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/magicwubiao/go-magic/internal/approval"
	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/pkg/catalog"
	appconfig "github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/log"
)

func (s *Server) handleConfigSchema(w http.ResponseWriter, r *http.Request) {
	// Build provider options dynamically from appconfig.ListProviders() so the schema
	// stays in sync with the full set of supported providers (currently 23).
	providerOptions := make([]string, 0, 32)
	for _, p := range appconfig.ListProviders() {
		providerOptions = append(providerOptions, p.Name)
	}

	schema := map[string]interface{}{
		"fields": map[string]interface{}{
			"provider": map[string]interface{}{
				"type":    "string",
				"default": "deepseek",
				"options": providerOptions,
			},
			"model": map[string]interface{}{
				"type":       "string",
				"default":    catalog.DefaultModel("deepseek"),
				"deprecated": true,
			},
			"cortex.enabled": map[string]interface{}{
				"type":     "boolean",
				"default":  true,
				"category": "cortex",
			},
			"cortex.skill_min_pattern_freq": map[string]interface{}{
				"type":     "number",
				"default":  3,
				"category": "cortex",
				"min":      1,
				"max":      10,
			},
			"secret_redaction": map[string]interface{}{
				"type":    "boolean",
				"default": true,
			},
			"gateway.enabled": map[string]interface{}{
				"type":    "boolean",
				"default": false,
			},
		},
		"category_order": []string{"general", "provider", "model", "tools", "cortex", "gateway"},
	}
	jsonResponse(w, schema)
}

// scanProfiles 列出所有分身。
//
// default 不是磁盘上的一个目录，而是"正在运行的这套数据"（<magicHome>/cortex），
// 所以永远先合成一张卡片。profiles/default 是早期版本建出来的空骨架，
// 这里刻意跳过：它既不参与运行，列出来还会出现两个 default，点进去编辑的
// 也不是真正生效的那份文件。
func (s *Server) scanProfiles() []map[string]interface{} {
	s.legacyProfileOnce.Do(s.cleanupLegacyDefaultProfile)

	defaultDir := appconfig.ProfileDir(s.magicHome, appconfig.DefaultProfileName)
	profiles := []map[string]interface{}{
		s.profileSummary(appconfig.DefaultProfileName, defaultDir, filepath.Join(s.magicHome, "skills")),
	}

	entries, err := os.ReadDir(s.profileRoot())
	if err != nil {
		// profiles/ 还没建出来是正常状态（只有 default）：不是错误。
		return profiles
	}

	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == appconfig.DefaultProfileName {
			continue
		}
		dir := filepath.Join(s.profileRoot(), entry.Name())
		profiles = append(profiles, s.profileSummary(entry.Name(), dir, filepath.Join(dir, "skills")))
	}

	return profiles
}

// profileSummary 汇总单个分身用于列表展示的信息。
//
// model/provider 仍恒为 nil：一个分身实际用哪个模型取决于自身 .env 与全局
// config.json 的组合，列表接口不去猜（猜错比留空更糟），前端按空值隐藏。
//
// 其余字段都是"磁盘上真实存在的东西"：人设/画像是否已写、画像里的姓名与角色、
// 凭据文件路径。面板据此判断"这个分身是空壳还是已经配好了"，而不是只看技能数。
func (s *Server) profileSummary(name, dir, skillsDir string) map[string]interface{} {
	var userName, userRole string
	if doc, err := readUserJSON(filepath.Join(dir, "USER.json")); err == nil && doc != nil {
		prefValue := func(key string) string {
			if p := doc.Preferences[key]; p != nil {
				return p.Value
			}
			return ""
		}
		userName, userRole = prefValue("Name"), prefValue("Role")
	}

	userMD := filepath.Join(dir, "USER.md")
	envPath := appconfig.EnvFilePath(s.magicHome, name)

	return map[string]interface{}{
		"name":      name,
		"path":      dir,
		"is_active": name == s.activeProfile(),
		// is_pending：已写进 config.json，但当前进程还在用旧分身（重启才生效）。
		// 没有它，面板会把"已选定"错标成"当前使用"，而人设/凭据其实还没换。
		"is_pending":  name == s.pendingProfile(),
		"is_default":  name == appconfig.DefaultProfileName,
		"model":       nil,
		"provider":    nil,
		"has_env":     fileExists(envPath),
		"env_path":    envPath,
		"skill_count": countDirEntries(skillsDir),
		// 旧版本把文件名写成小写，读取时一并兜住（与 handleProfileUser 一致）。
		"has_soul":  fileExists(readableSoulPath(filepath.Join(dir, "SOUL.md"))),
		"has_user":  fileExists(userMD) || fileExists(legacyLowerName(userMD)) || fileExists(filepath.Join(dir, "USER.json")),
		"user_name": userName,
		"user_role": userRole,
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func countDirEntries(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	return len(entries)
}

func (s *Server) handleConfigByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/config/")

	// Handle sub-routes
	switch id {
	case "defaults":
		s.handleConfigDefaults(w, r)
		return
	case "raw":
		s.handleConfigRaw(w, r)
		return
	case "schema":
		s.handleConfigSchema(w, r)
		return
	}

	// 子路由各自处理（raw/schema/defaults 不依赖内存快照）
	s.syncConfigFromDisk()

	cfg := s.cfgSnapshot()
	if cfg == nil {
		jsonResponse(w, map[string]interface{}{"id": id, "value": ""})
		return
	}

	// Get specific config value
	cfgMap := make(map[string]interface{})
	data, _ := json.Marshal(cfg)
	json.Unmarshal(data, &cfgMap)

	if val, ok := cfgMap[id]; ok {
		jsonResponse(w, map[string]interface{}{"id": id, "value": val})
		return
	}
	jsonResponse(w, map[string]interface{}{"id": id, "value": nil})
}

// parseUserMD 解析 USER.md（人类可读视图）为面板表单结构。
//
// 只在 USER.json 缺失时作为回退路径使用：JSON 才是权威存储。
func (s *Server) parseUserMD(content string) map[string]interface{} {
	data := map[string]interface{}{
		"name":                "",
		"role":                "",
		"communication_style": "",
		"code_style":          "",
		"tech_stack":          []string{},
		"interests":           []string{},
	}

	lines := strings.Split(content, "\n")
	currentSection := ""

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "## ") {
			currentSection = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "## ")))
			continue
		}
		// "## All Preferences" 是展示用副本（"- K: V (source: ...)"），
		// 解析回表单会被 "(source: ...)" 尾巴污染。
		if currentSection == "all preferences" || currentSection == "notes" {
			continue
		}
		if !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "*") {
			continue
		}

		item := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "-"), "*"))
		if item == "" || item == "[Not set]" {
			continue
		}

		switch currentSection {
		case "tech stack", "interests":
			// 列表型小节是纯 "- 值"，没有冒号；模板占位行
			// "- Languages: [Not set]" 要跳过。
			if idx := strings.Index(item, ":"); idx > 0 {
				v := strings.TrimSpace(item[idx+1:])
				if v == "" || v == "[Not set]" {
					continue
				}
			}
			key := "tech_stack"
			if currentSection == "interests" {
				key = "interests"
			}
			if list, ok := data[key].([]string); ok {
				data[key] = append(list, item)
			}
			continue
		}

		idx := strings.Index(item, ":")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(item[:idx])
		value := strings.TrimSpace(item[idx+1:])
		if value == "" || value == "[Not set]" {
			continue
		}

		switch currentSection {
		case "about":
			if strings.EqualFold(key, "Name") {
				data["name"] = value
			} else if strings.EqualFold(key, "Role") {
				data["role"] = value
			}
		case "preferences":
			if strings.EqualFold(key, "Communication style") {
				data["communication_style"] = value
			} else if strings.EqualFold(key, "Code style") {
				data["code_style"] = value
			}
		}
	}

	return data
}

func (s *Server) handleSettingByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/settings/")

	// 分身的增删改查只有 /api/profiles 一套。这里曾经有一份并行的
	// /api/settings/profiles/* 实现，其中 create/delete 只回 {"created":true}
	// 而完全不落盘 —— 调用方拿到 200 就以为成功了。现在直接 404 + 指向正确入口。
	if strings.HasPrefix(id, "profiles") {
		http.Error(w, "profiles API moved to /api/profiles", http.StatusNotFound)
		return
	}

	jsonResponse(w, map[string]interface{}{"id": id, "value": ""})
}

// persistConfig atomically writes s.cfg to config.json (tmp + rename, 0600).
//
// When preserveGateway is true the gateway section is refreshed from disk
// first: the gateway process writes QR-login credentials (wecom bot_id/secret,
// wechat_ilink token) straight into config.json, and the server's in-memory
// copy may be stale — a full overwrite used to revert those credentials
// ("配置被还原/丢失" root cause). Sections the server itself just modified
// (and already merged on top of a fresh disk read) must pass false.
func (s *Server) persistConfig(preserveGateway bool) error {
	// 反向路径守卫：若调用方正持 agentsMu，这里再去读 s.cfg/拿 s.mu 就构成
	// s.agentsMu → s.mu 的反序，与 syncConfigFromDisk 的 s.mu → s.agentsMu
	// 撞成 AB-BA 死锁。见 lockorder.go。
	s.checkBeforeServerMu("persistConfig")
	// 注意 s.configMu 只保护**文件写入**，不保护 s.cfg/s.configMtime。
	//
	// 这里刻意不碰 s.mu 与 cfgMu：调用方常已持 s.mu（handleModelSet、
	// tools/profiles 的 enable/disable、privacy 热重载都这么调），而
	// sync.RWMutex 不可重入 —— 在持锁路径里再拿一次就是自我死锁。
	// cfgMu 同理：绝大多数写点都持 cfgMu 后才落盘（lockCfgForWrite /
	// setPluginDisabled / syncApprovalToMainConfig …），这里再拿一次必自锁。
	//
	// 因此本函数**直接读 s.cfg 的字段值，但绝不解引用其内容**（不遍历、
	// 不原地改写）——只把它交给 json.Marshal 序列化。"是否有人在改这个
	// 对象"由调用方保证：改配置的调用方必须持 cfgMu，且按 copy-on-write
	// 约定改的是私有副本。这样本函数既不引入新锁，也不与任何写点竞态。
	s.configMu.Lock()
	defer s.configMu.Unlock()
	cfg := s.cfg
	if cfg == nil {
		return nil
	}
	// Empty s.magicHome must never degrade to a relative ./config.json (that
	// would write into the process CWD — including `go test` runs, which is
	// exactly how test-run leftovers used to clobber the real config).
	magicHome := s.magicHome
	if magicHome == "" {
		magicHome = appconfig.GetMagicHome()
	}
	// preserveGateway：网关进程会把扫码登录凭据直接写进 config.json，内存
	// 副本可能是旧的，全量覆盖会把凭据冲掉。这里先在**副本**上合并磁盘上
	// 更新的 gateway 段，绝不改 s.cfg 指向的对象（那是共享快照，就地改
	// 会与无锁读者竞态）。
	toWrite := cfg
	if preserveGateway {
		if fresh, err := appconfig.Load(); err == nil && fresh != nil {
			merged := cloneConfig(cfg)
			merged.Gateway = fresh.Gateway
			toWrite = merged
		}
	}
	// 拆分文件先写（mcp.json / cli.json），config.json 序列化时剥离 mcp/cli 段
	// ——与 pkg/config.Save 完全同一约定（先拆分后主文件，崩溃安全顺序见那边注释）。
	if err := toWrite.WriteMCPCLIFiles(magicHome); err != nil {
		return err
	}
	data, err := json.MarshalIndent(toWrite.WithoutMCPCLI(), "", "  ")
	if err != nil {
		return err
	}
	configPath := filepath.Join(magicHome, "config.json")
	tmpPath := configPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, configPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	// 自己刚写盘成功，刷新 mtime 基线，避免下一次 syncConfigFromDisk
	// 把这笔写入误判成外部修改而白白重建 provider。
	s.setConfigMtimeFromDisk()
	return nil
}

// setConfigMtimeFromDisk 把 configMtime 基线刷成三个配置文件（config.json /
// mcp.json / cli.json）中**最新**的修改时间（见 markConfigMtime）。configMtime
// 只被 syncConfigFromDisk 的 mtime 比较读取，那个比较本身容忍陈旧值（最坏情况
// 是多重载一次配置），因此这里用 atomic 读写即可，不需要去碰 s.mu —— 拿 s.mu
// 会在 "persistConfig 的调用方已持 s.mu"时自我死锁。
func (s *Server) setConfigMtimeFromDisk() {
	if m := s.maxConfigMtime(); m > 0 {
		s.configMtimeNs.Store(m)
	}
}

// maxConfigMtime 返回主配置与两个拆分配置文件中最新的 mtime（UnixNano）；
// 一个都不存在时返回 0。拆分文件纳入监测的原因：用户/外部工具可能直接手改
// mcp.json / cli.json，只盯 config.json 会永远看不到这类变更。
func (s *Server) maxConfigMtime() int64 {
	var max int64
	for _, p := range []string{
		s.configPath(),
		s.splitConfigPath(appconfig.MCPConfigFileName),
		s.splitConfigPath(appconfig.CLIConfigFileName),
	} {
		if info, err := os.Stat(p); err == nil {
			if m := info.ModTime().UnixNano(); m > max {
				max = m
			}
		}
	}
	return max
}

// reloadConfig re-reads config.json into s.cfg so changes written by other
// processes (gateway QR login credentials, CLI config edits) are picked up
// before the server merges and saves its own changes.
func (s *Server) reloadConfig() {
	if fresh, err := appconfig.Load(); err == nil && fresh != nil {
		s.acquireServerMu("")
		s.setCfg(fresh)
		s.releaseServerMu()
		s.markConfigMtime()
	}
}

// configPath 返回当前生效 profile 的 config.json 路径。
func (s *Server) configPath() string {
	return filepath.Join(s.configDir(), "config.json")
}

// splitConfigPath 返回 mcp.json / cli.json 的路径（与 config.json 同目录）。
func (s *Server) splitConfigPath(name string) string {
	return filepath.Join(s.configDir(), name)
}

func (s *Server) configDir() string {
	magicHome := s.magicHome
	if magicHome == "" {
		magicHome = appconfig.GetMagicHome()
	}
	return magicHome
}

// markConfigMtime 把 configMtime 基线刷成磁盘上 config.json 的当前修改时间。
// 在启动加载、reloadConfig 和 persistConfig 成功后调用，使"我们自己刚写过
// 的文件"不会在下一次 syncConfigFromDisk 里被误判为外部变更。
func (s *Server) markConfigMtime() { s.setConfigMtimeFromDisk() }

// syncConfigFromDisk 让内存配置快照跟上外部对 config.json 的直接修改。
//
// 之前配置只在启动时 Load 一次，之后只有 GET/PUT /api/config 会顺带
// reload —— 用户手改 config.json 后，chat 页依赖的 /api/model/options、
// /api/providers 等只读接口读的一直是启动时的旧快照，页面永远看不到更新。
// 这里用 mtime 做廉价变更检测：磁盘比已知基线新才重读，并在配置真的
// 变化后重建 provider、清空缓存 agent（与 handleModelSet 全量切换同一条
// 路径），保证新的 key/模型/baseURL 对后续对话立即生效。
//
// 加锁顺序契约：**s.mu → s.agentsMu**，任何地方都不得反序。
// 调用方也**不得在持 s.mu 时调用本函数**（handleModelSet 曾这么干，
// 于是立刻退化成 s.mu → s.agentsMu 的反序持有，与 approvals/sessions
// 的 s.agentsMu → s.cfg 相撞，把整个后端锁死）。
func (s *Server) syncConfigFromDisk() {
	// 契约断言：调用方**不得持 s.mu**。
	//
	// 这里要拿 s.agentsMu（见函数末尾的 clearAgents），若调用方已持 s.mu，
	// 本 goroutine 就走成 s.mu → s.agentsMu 的反向持有；只要另一条路径
	// 恰好持 agentsMu 又想拿 s.mu（approval/sessions 的 s.cfg 读），
	// 立即 AB-BA 死锁，线上表现为整个后端卡死。这正是 handleModelSet 曾经
	// 踩中的坑（chat 页切完模型、打开模型供应商页即死）。
	// 见 model_switch_deadlock_test.go。
	s.assertNotHoldingServerMu("syncConfigFromDisk")

	// mtime 取三个配置文件（config.json / mcp.json / cli.json）的最大值：
	// 外部工具可能只改拆分文件（例如脚本直接写 mcp.json 加一个 server），
	// 只盯主文件会漏掉这类变更。
	current := s.maxConfigMtime()
	if current == 0 {
		return
	}
	if current <= s.configMtimeNs.Load() {
		return
	}

	// 用 CAS 抢占"由我来重载"的权利：并发请求只留一个真正重载，其余直接返回。
	// 旧实现用 RLock 读 + Lock 判重，两次加锁之间没有任何互斥，语义等价但
	// 多绕两圈；换成 atomic 后这段完全不碰 s.mu，也就不会在"调用方已持
	// s.mu"时自我死锁。
	prev := s.configMtimeNs.Swap(current)
	if prev == current {
		return
	}

	fresh, err := appconfig.Load()
	if err != nil || fresh == nil {
		return
	}
	s.acquireServerMu("")
	s.setCfg(fresh)
	if fresh.Provider != "" {
		s.provider = createProvider(fresh)
		s.refreshConvertConfig()
	}
	s.releaseServerMu()

	// 必须在释放 s.mu 之后调用：s.mu → s.agentsMu 的顺序契约。
	s.clearAgents()
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPut {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		// The frontend sends approval settings here. Apply them to the
		// approval section instead of blindly overwriting the whole
		// config.json with the in-memory copy — that used to revert
		// credentials the gateway process had written (QR login).
		//
		// 整段改写 + persistConfig 都在 s.mu + cfgMu 内：这里原地改的是
		// 配置对象自身，且 persistConfig 序列化整份 s.cfg，必须与其它写点
		// 串行。cfgMu 与 s.mu 互不相干（setCfg 走 cfgMu），只拿 s.mu 会被
		// CI 的 -race 判为 DATA RACE；cfgMu 是叶子锁，可持 s.mu 再拿。
		s.acquireServerMu("")
		defer s.releaseServerMu()
		s.lockCfgForWrite()
		defer s.unlockCfgForWrite()
		// lockCfgForWrite 保证 s.cfg 非 nil（nil 时会补 DefaultConfig）。
		if s.cfg.Approval == nil {
			s.cfg.Approval = appconfig.DefaultApprovalConfig()
		}
		if v, ok := req["strategy"].(string); ok && v != "" {
			s.cfg.Approval.Strategy = v
		}
		if v, ok := req["timeout_strategy"].(string); ok && v != "" {
			s.cfg.Approval.TimeoutStrategy = v
		}
		if v, ok := req["trust_threshold"].(float64); ok && v > 0 {
			s.cfg.Approval.TrustThreshold = int(v)
		}
		if v, ok := req["enable_learning"].(bool); ok {
			s.cfg.Approval.EnableLearning = v
		}
		if v, ok := req["cli_confirm"].(bool); ok {
			s.cfg.Approval.EnableCLIConfirm = v
		}
		if mgr := s.getApprovalManager(); mgr != nil {
			mgr.SetStrategy(approval.Strategy(s.cfg.Approval.Strategy))
			mgr.SetTrustThreshold(s.cfg.Approval.TrustThreshold)
			mgr.SetEnableLearning(s.cfg.Approval.EnableLearning)
			mgr.SetEnableCLIConfirm(s.cfg.Approval.EnableCLIConfirm)
			if s.cfg.Approval.ApprovalTimeout > 0 {
				mgr.SetApprovalTimeout(s.cfg.Approval.ApprovalTimeout)
			}
		}
		if err := s.persistConfig(true); err != nil {
			http.Error(w, "failed to save config: "+err.Error(), 500)
			return
		}
		jsonResponse(w, map[string]bool{"ok": true})
		return
	}
	jsonResponse(w, map[string]interface{}{
		"general": map[string]interface{}{
			"language": "en",
			"timezone": "UTC",
		},
		"appearance": map[string]interface{}{
			"theme":    "dark",
			"fontSize": 14,
		},
	})
}

func (s *Server) handleConfigRaw(w http.ResponseWriter, r *http.Request) {
	// file=mcp / file=cli 操作拆分文件（mcp.json / cli.json）；缺省是主配置
	// config.json。mcp / cli 两段已从主配置拆出（persistConfig 会剥离旧段），
	// 原始编辑器必须能单独编辑拆分文件，否则手改配置的用户找不到入口。
	switch r.URL.Query().Get("file") {
	case "mcp":
		s.handleSplitConfigRaw(w, r, appconfig.MCPConfigFileName)
		return
	case "cli":
		s.handleSplitConfigRaw(w, r, appconfig.CLIConfigFileName)
		return
	case "", "config":
	default:
		http.Error(w, "unknown file; expected config, mcp or cli", 400)
		return
	}
	configPath := s.configPath()

	switch r.Method {
	case "GET":
		data, err := os.ReadFile(configPath)
		if err != nil {
			jsonResponse(w, map[string]interface{}{"yaml": "{}"})
			return
		}
		jsonResponse(w, map[string]interface{}{"yaml": string(data)})
	case "PUT":
		s.handleConfigRawPut(w, r, configPath)
	default:
		http.Error(w, "method not allowed", 405)
	}
}

// handleSplitConfigRaw 是 mcp.json / cli.json 的原始读写入口。
// 写入后走 appconfig.Load 全量重载（Load 以拆分文件为权威），后续与主配置
// 的 PUT 完全一致：重建 provider、刷新工具过滤、清空 agent 缓存。
func (s *Server) handleSplitConfigRaw(w http.ResponseWriter, r *http.Request, name string) {
	path := s.splitConfigPath(name)
	switch r.Method {
	case "GET":
		data, err := os.ReadFile(path)
		if err != nil {
			// 文件不存在 = 还没配过：返回空对象而不是 404，编辑器直接可写。
			jsonResponse(w, map[string]interface{}{"yaml": "{}"})
			return
		}
		jsonResponse(w, map[string]interface{}{"yaml": string(data)})
	case "PUT":
		s.handleConfigRawPut(w, r, path)
	default:
		http.Error(w, "method not allowed", 405)
	}
}

// handleConfigRawPut 校验并原子写入一个配置 JSON 文件，然后全量重载。
func (s *Server) handleConfigRawPut(w http.ResponseWriter, r *http.Request, configPath string) {
	var req struct {
		JsonText string `json:"json_text"`
		YamlText string `json:"yaml_text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	// Support both json_text and yaml_text (frontend uses yaml_text)
	content := req.JsonText
	if content == "" {
		content = req.YamlText
	}
	// Validate JSON
	var parsed interface{}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		http.Error(w, "invalid JSON", 400)
		return
	}
	// Write atomically
	tmpPath := configPath + ".tmp"
	os.WriteFile(tmpPath, []byte(content), 0644)
	os.Rename(tmpPath, configPath)
	// Reload config
	newCfg, err := appconfig.Load()
	if err == nil {
		s.acquireServerMu("")
		s.setCfg(newCfg)
		s.provider = createProvider(newCfg)
		// 原始编辑器可以直接改 tools.enabled/disabled：同步重建暴露过滤，
		// 否则要等重启（下一次 setCfg 之外的路径不会带上它）。
		if s.toolReg != nil {
			s.toolReg.SetToolsetFilter(newCfg.Tools.Enabled, newCfg.Tools.Disabled)
		}
		// Fresh provider starts with a nil ConvertCfg, which means "no
		// vision at all" until the next agent is built. Install the
		// derived policy right away.
		s.refreshConvertConfig()
		s.releaseServerMu()
		// 清空 agent 缓存走 agentsMu（见 clearAgents）——不能在 s.mu 里做：
		// syncConfigFromDisk 的加锁顺序是 s.mu -> s.agentsMu，而
		// approvals/sessions 路径是 s.agentsMu -> s.cfg，反序即死锁。
		s.clearAgents()
	}
	jsonResponse(w, map[string]interface{}{"ok": true})
}

// readEnvFile 读取 .env（键值对）。解析细节统一在 appconfig.LoadEnvFile，
// 这里只负责把错误降级为空表 —— 面板列表页读不到文件时应当显示空，而不是 500。
func (s *Server) readEnvFile(path string) map[string]string {
	vars, err := appconfig.LoadEnvFile(path)
	if err != nil {
		log.Warnf("[env] failed to read %s: %v", path, err)
		return map[string]string{}
	}
	return vars
}

// writeEnvFile 写回 .env。保留原有顺序与注释，原子写入且权限 0600
// （文件里是明文凭据）。错误必须返回给调用方：写盘失败却回 200 会让用户
// 以为 key 已经保存，下次重启才发现丢失。
func (s *Server) writeEnvFile(path string, vars map[string]string) error {
	return appconfig.WriteEnvFile(path, vars)
}

// envFilePath 返回**活动分身**的 .env 路径。
//
// 之前这里硬编码 magicHome/.env，于是无论切到哪个分身，面板改的都是
// default 的凭据：在 work 分身里填的 key 永远不生效，而 default 的 key
// 会被悄悄覆盖。分身的其它数据（SOUL/USER/skills）都已按活动分身裁决，
// 凭据也必须一致。
func (s *Server) envFilePath() string {
	path := appconfig.EnvFilePath(s.magicHome, s.activeProfile())
	if path == "" {
		// 活动分身名非法（手改 config.json 写进 "../.." 之类）：
		// 退回 default 的路径，绝不拿非法名字拼路径。
		return appconfig.EnvFilePath(s.magicHome, appconfig.DefaultProfileName)
	}
	return path
}

func (s *Server) handleEnv(w http.ResponseWriter, r *http.Request) {
	envPath := s.envFilePath()

	switch r.Method {
	case "GET":
		envVars := s.readEnvFile(envPath)
		envResponse := make(map[string]interface{})
		for key, value := range envVars {
			info := map[string]interface{}{
				"is_set":         value != "",
				"redacted_value": "",
				"description":    "",
				"url":            nil,
				"category":       "",
				"is_password":    false,
				"tools":          []string{},
				"advanced":       false,
			}
			if value != "" {
				info["redacted_value"] = "****"
			}
			// 来源与可改性：面板必须能区分".env 提供"和"外部环境提供"。
			// 后者即使显示为已设置，改 .env 也不会生效（真实环境变量优先），
			// 不标出来的话用户会反复重填同一个 key 并怀疑保存失败。
			source := appconfig.EnvSourceOf(key)
			info["source"] = string(source)
			info["editable"] = source != appconfig.EnvSourceReal
			// 由外部环境提供时，.env 里的值只是"未生效的影子"，不能声称 is_set，
			// 否则面板会告诉用户"已配置"，而工具侧读到的是另一份值。
			if source == appconfig.EnvSourceReal {
				info["is_set"] = true
				info["redacted_value"] = "****"
				info["note"] = "Provided by the process environment; the value in .env is not in effect"
			}
			// Enrich known keys with description and category
			switch key {
			case "DEEPSEEK_API_KEY":
				info["description"] = "API key for DeepSeek provider"
				info["category"] = "provider"
				info["is_password"] = true
				info["tools"] = []string{"chat"}
			case "OPENAI_API_KEY":
				info["description"] = "API key for OpenAI provider"
				info["category"] = "provider"
				info["is_password"] = true
				info["tools"] = []string{"chat"}
			case "ANTHROPIC_API_KEY":
				info["description"] = "API key for Anthropic provider"
				info["category"] = "provider"
				info["is_password"] = true
				info["tools"] = []string{"chat"}
			case "GOOGLE_API_KEY":
				info["description"] = "API key for Google AI provider"
				info["category"] = "provider"
				info["is_password"] = true
				info["tools"] = []string{"chat"}
			case "OPENROUTER_API_KEY":
				info["description"] = "API key for OpenRouter provider"
				info["category"] = "provider"
				info["is_password"] = true
				info["tools"] = []string{"chat"}
			case "GROQ_API_KEY":
				info["description"] = "API key for Groq provider"
				info["category"] = "provider"
				info["is_password"] = true
				info["tools"] = []string{"chat"}
			case "TOGETHER_API_KEY":
				info["description"] = "API key for Together AI provider"
				info["category"] = "provider"
				info["is_password"] = true
				info["tools"] = []string{"chat"}
			case "FIRECRAWL_API_KEY":
				info["description"] = "API key for Firecrawl web scraping"
				info["category"] = "tool"
				info["is_password"] = true
				info["tools"] = []string{"web"}
			case "TAVILY_API_KEY":
				info["description"] = "API key for Tavily search"
				info["category"] = "tool"
				info["is_password"] = true
				info["tools"] = []string{"web"}
			case "EXA_API_KEY":
				info["description"] = "API key for Exa search"
				info["category"] = "tool"
				info["is_password"] = true
				info["tools"] = []string{"web"}
			case "SERPAPI_API_KEY":
				info["description"] = "API key for SerpAPI search"
				info["category"] = "tool"
				info["is_password"] = true
				info["tools"] = []string{"web"}
			case "GITHUB_TOKEN":
				info["description"] = "GitHub personal access token"
				info["category"] = "tool"
				info["is_password"] = true
				info["tools"] = []string{"git"}
			case "GO_MAGIC_HOME":
				info["description"] = "Path to the Magic home directory"
				info["category"] = "general"
				info["is_password"] = false
				info["tools"] = []string{}
			}
			envResponse[key] = info
		}
		jsonResponse(w, envResponse)
	case "POST", "PUT":
		var req struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		key := strings.TrimSpace(req.Key)
		if !validEnvKey(key) {
			http.Error(w, "invalid env key: use letters, digits and underscore, not starting with a digit", 400)
			return
		}

		// 先落盘再改进程环境：写失败时不能留下"内存里生效、磁盘上没有"的
		// 分裂状态（重启后 key 又消失，用户完全无法理解）。
		envVars := s.readEnvFile(envPath)
		envVars[key] = req.Value
		if err := s.writeEnvFile(envPath, envVars); err != nil {
			http.Error(w, "failed to save env file: "+err.Error(), 500)
			return
		}

		// 立即注入，无需重启。
		if !appconfig.SetEnvVarInstant(key, req.Value) {
			// 真实环境变量已经提供了这个 key：文件写了，但这一份不会生效。
			jsonResponse(w, map[string]interface{}{
				"ok":               true,
				"applied":          false,
				"note":             "This key is provided by the process environment; the saved value will not take effect until it is unset",
				"restart_required": false,
			})
			return
		}
		invalidateEnvCaches()
		jsonResponse(w, map[string]interface{}{"ok": true, "applied": true, "restart_required": false})
	case "DELETE":
		var req struct {
			Key string `json:"key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		key := strings.TrimSpace(req.Key)
		if key == "" {
			http.Error(w, "invalid env key", 400)
			return
		}

		envVars := s.readEnvFile(envPath)
		delete(envVars, key)
		if err := s.writeEnvFile(envPath, envVars); err != nil {
			http.Error(w, "failed to save env file: "+err.Error(), 500)
			return
		}
		// 立刻撤销（仅当这个值是我们注入的；外部环境变量不归面板管）。
		removed := appconfig.UnsetEnvVarInstant(key)
		invalidateEnvCaches()
		jsonResponse(w, map[string]interface{}{"ok": true, "applied": removed})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		// Always re-read from file to pick up changes made by gateway process (e.g. QR login token)
		s.reloadConfig()
		// 经 cfgSnapshot 取快照：reloadConfig 会在 s.mu 下整体替换 s.cfg，
		// 这里若无保护地裸读指针就是数据竞态（-race 直接报）。
		cfg := s.cfgSnapshot()
		if cfg == nil {
			jsonResponse(w, map[string]interface{}{})
			return
		}
		jsonResponse(w, cfg)
	case "PUT":
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request: "+err.Error(), 400)
			return
		}
		// Get config payload - support both {config: {...}} and direct config object
		var configData map[string]interface{}
		if cfg, ok := req["config"].(map[string]interface{}); ok {
			configData = cfg
		} else {
			configData = req
		}

		// Handle dot-notation keys (e.g. "memory.enabled") by expanding into nested objects
		expanded := expandDotKeys(configData)

		// Re-read config.json before merging: the gateway process may have
		// written new QR credentials (wecom bot_id/secret, ilink token) after
		// our in-memory copy was loaded; merging into a stale copy and saving
		// would revert them ("配置被还原" root cause).
		s.reloadConfig()

		// 整段合并必须在 s.mu + cfgMu 内完成：这里是**唯一**原地改写配置
		// 对象的分支（其余写点都是整体替换指针）。s.mu 管内容字段的逻辑
		// 串行化，cfgMu 管与 setCfg 的互斥（两把锁互不相干，只拿 s.mu 会被
		// CI 的 -race 判为 DATA RACE）。cfgMu 是叶子锁，可持 s.mu 再拿。
		g := s.lockCfgWriteGuard()
		// Merge into config
		// lockCfgWriteGuard 已保证 s.cfg 非 nil 且是本写点私有的副本。
		cfg := s.cfg
		data, _ := json.Marshal(expanded)
		if err := json.Unmarshal(data, cfg); err != nil {
			g.Release()
			http.Error(w, "failed to merge config: "+err.Error(), 500)
			return
		}
		// 路径类配置在写入前统一展开 `~`：前端 / 脚本可能直接 POST 字面量
		// `~/.magic/...`，不展开就会在进程 CWD 下建出名为 `~` 的目录，
		// 而且会被持久化进 config.json 一直错下去。
		cfg.WorkingDir = appconfig.ExpandHome(cfg.WorkingDir)
		if cfg.BrowserProfileDir != nil {
			dir := appconfig.ExpandHome(*cfg.BrowserProfileDir)
			cfg.BrowserProfileDir = &dir
		}
		// Save (atomically; gateway section was refreshed above and merged,
		// so do NOT preserve it from disk again here)
		if err := s.persistConfig(false); err != nil {
			g.Release()
			http.Error(w, "failed to save config: "+err.Error(), 500)
			return
		}

		// Check which config sections changed and hot-reload accordingly
		hotReloadKeys := []string{"provider", "model", "api_key", "base_url", "secret_redaction", "profile", "working_dir"}
		needsProviderReload := false

		for key := range expanded {
			for _, reloadKey := range hotReloadKeys {
				if key == reloadKey {
					needsProviderReload = true
					break
				}
			}
		}

		// Hot-reload provider if provider-related config changed.
		//
		// refreshConvertConfig / clearAgents 会去读配置快照（cfgSnapshot
		// 拿 cfgMu）和 agentsMu —— 此刻 cfgMu 还握在 g 手上，必须在
		// 释放之后再做。Release 幂等。
		if needsProviderReload {
			s.provider = createProvider(cfg)
		}
		needsClearAgents := needsProviderReload
		g.Release()
		if needsProviderReload {
			// Same as the raw-editor path: the rebuilt provider needs the
			// current conversion/vision policy installed on it.
			s.refreshConvertConfig()
		}

		// Hot-reload approval config if approval section changed
		if _, ok := expanded["approval"]; ok && s.approvalMgr != nil {
			if ac := cfg.Approval; ac != nil {
				// 校验 strategy 合法性，空字符串保持不变
				if ac.Strategy != "" {
					switch ac.Strategy {
					case "manual", "auto", "smart", "whitelist":
						s.approvalMgr.SetStrategy(approval.Strategy(ac.Strategy))
					}
				}
				// 使用 SetXxx 方法，避免修改 GetConfig() 返回的局部拷贝
				if ac.TrustThreshold > 0 {
					s.approvalMgr.SetTrustThreshold(ac.TrustThreshold)
				}
				s.approvalMgr.SetEnableLearning(ac.EnableLearning)
				s.approvalMgr.SetEnableCLIConfirm(ac.EnableCLIConfirm)
				if ac.ApprovalTimeout > 0 {
					s.approvalMgr.SetApprovalTimeout(ac.ApprovalTimeout)
				}
			}
		}

		// Hot-reload toolset filter if the tools section changed
		// （tools.enabled/disabled 控制暴露给模型的工具清单）
		if _, ok := expanded["tools"]; ok && s.toolReg != nil {
			s.toolReg.SetToolsetFilter(cfg.Tools.Enabled, cfg.Tools.Disabled)
		}

		// Hot-reload privacy/PII config: clear agent cache so new sessions pick up the new redactor
		if _, ok := expanded["privacy"]; ok {
			// clearAgents 要拿 agentsMu，而 cfgMu 是叶子锁 —— 必须先放锁。
			// g.Release 幂等且是"全放"，后续段落不再需要写锁（只读快照）。
			g.Release()
			needsClearAgents = true
		}
		// 重建 provider 之后同样要清 agent 缓存（缓存的 agent 持有旧
		// provider 引用）。统一在放锁后做一次，避免重复清空。
		if needsClearAgents {
			s.clearAgents()
		}

		// 以下段落只**读**配置：用一次快照，避免裸读 s.cfg（此处已无锁，
		// 裸读会与 setCfg 竞态）。g.Release 幂等，重复调用安全。
		cfgSnap := s.cfgSnapshot()

		// Bot Mode: history_window / inject_bot_protocol apply to the running
		// manager immediately; toggling enabled requires a full restart, which
		// the UI surfaces as a warning banner (bot_mode.enabled is read by
		// initBotMode only when the manager is (re)created).
		if _, ok := expanded["bot_mode"]; ok && s.botManager != nil && cfgSnap != nil {
			s.botManager.ReloadConfig(cfgSnap)
		}

		// Persistent browser profile dir: apply immediately. A running browser
		// keeps its current profile; when idle (no tabs) we tear it down so the
		// next use starts fresh with the new dir.
		if _, ok := expanded["browser_profile_dir"]; ok && cfgSnap != nil {
			bm := tool.GetBrowserManager()
			// 两侧都取"生效值"（GetBrowserProfileDir 已展开 `~` 并补默认目录）：
			// ProfileDir() 存的是展开后的绝对路径，直接比原始配置会在每次保存时
			// 误判成"变了"而白关一次浏览器。
			if dir := cfgSnap.GetBrowserProfileDir(); bm.ProfileDir() != dir {
				if bm.TabCount() == 0 {
					bm.Close()
				}
				bm.SetProfileDir(dir)
			}
		}

		// Browser headless toggle: same treatment as the profile dir above.
		// Headless-ness is baked into Chrome's launch flags, so a running
		// browser cannot change mode in place -- it has to be relaunched. We
		// only tear it down when idle (no tabs); with tabs open the new setting
		// takes effect on the next restart, and we say so instead of silently
		// dropping live sessions.
		if _, ok := expanded["browser_headless"]; ok && cfgSnap != nil {
			bm := tool.GetBrowserManager()
			// 比较的是 HeadlessEffective()（已把环境变量优先级算进去）而不是
			// *BrowserHeadless：设了 BROWSER_HEADLESS 时后者恒不等于生效值，
			// 每次保存都会白关一次浏览器。
			//
			// 注意 bool 的"零值陷阱"：旧的比较逻辑用 != 判断"变了"，在 bool 上
			// 同样成立——但环境变量会覆盖配置，所以必须比生效值。
			if want, _ := cfgSnap.GetBrowserHeadlessWithSource(); bm.HeadlessEffective() != want {
				if bm.TabCount() == 0 {
					bm.Close()
				}
				// HeadlessEffective() 会随配置变化自动重算，无需额外的 setter
				// （记录的是"上一次真正用来启动 Chrome 的值"）。
				bm.NoteHeadlessApplied(want)
			}
		}

		// Return updated config
		respCfg := s.cfgSnapshot()
		g.Release()
		if respCfg == nil {
			jsonResponse(w, map[string]interface{}{})
			return
		}
		jsonResponse(w, respCfg)
	default:
		http.Error(w, "method not allowed", 405)
	}
}

// validEnvKey 校验环境变量名。
//
// 必须拦：空串、含 `=`、含换行。前两个会让写出的文件解析不回来，换行更糟——
// 一个 "A=1\nB=2" 的 key 能凭空往 .env 里塞入任意变量（面板只有一个输入框，
// 但请求体是用户可控的）。
func validEnvKey(key string) bool {
	if key == "" || len(key) > 128 {
		return false
	}
	for i, r := range key {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r == '_':
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// invalidateEnvCaches 丢弃那些"探测一次就永久缓存"的环境变量判定。
//
// internal/tool 里的 visionBackendConfigured / voiceAPIKeyConfigured 用
// sync.Once 缓存结果，避免每次列工具都读盘。代价是：面板刚填好
// VISION_API_KEY 时，工具列表还是旧的（vision_analyze 不出现），用户以为
// 没生效。这里显式重置缓存，让"保存后立即可用"是真的。
func invalidateEnvCaches() {
	tool.ResetEnvProbes()
}

func (s *Server) handleConfigDefaults(w http.ResponseWriter, r *http.Request) {
	defaults := appconfig.DefaultConfig()
	jsonResponse(w, defaults)
}
