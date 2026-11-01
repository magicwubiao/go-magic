package server

// 插件凭据的加密存储。
//
// 背景：插件凭据（API key / token）此前明文写进 config.json 的
// agent_plugins.credentials。这与本仓库自己在 OAuth 上立的规矩直接矛盾——
// "密钥不进 config.json：那个文件用户会手工编辑/备份/分享"。同一个用户
// 把 config.json 发到 issue 里求助时，OAuth token 是安全的，插件 key 却是
// 明文；而项目里明明已经有一套 AES-GCM 的 internal/secrets.Store 全库零调用。
//
// 现在的落盘形态：
//   - <magicHome>/plugin_secrets/secrets.enc   （0600，AES-GCM 加密后）
//   - <magicHome>/.masterkey                   （0600，secrets 包自动生成）
//
// 兼容：老版本写进 config.json 的明文凭据在启动时**迁移**进来并清空原字段
// （见 migrateLegacyPluginCredentials）。迁移是幂等的一次性动作。

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/magicwubiao/go-magic/internal/secrets"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// pluginCredKeyPrefix 是全部插件凭据 key 的前缀（secrets.Store 用一个
// 扁平的 name → value 表，靠前缀区分归属）。
const pluginCredKeyPrefix = "agentplugin/"

// pluginCredProvider 是 secrets.Store 里的 provider 标签，便于整体列举。
const pluginCredProvider = "agentplugin"

// pluginCredentialStore 是插件凭据的读写门面。
//
// 内部把"插件名 + env key"编码成扁平 key（agentplugin/<plugin>/<KEY>），
// 一次读写就是一组 Get/Set/Delete。刻意不做缓存：凭据的读取频率是
// "每次连接/每次注册"，key 数量是个位数，读加密文件的成本可以忽略，
// 而缓存会引入"改了却读到旧值"的风险。
type pluginCredentialStore struct {
	mu    sync.Mutex
	store *secrets.Store
}

// newPluginCredentialStore 打开（或首次创建）凭据存储。
// 失败时返回一个空壳（store == nil），调用方据此退化成"读不到任何凭据"，
// 而不是让整个 server 起不来——凭据表坏掉不该阻断会话。
func newPluginCredentialStore(dir string) *pluginCredentialStore {
	st, err := secrets.NewStore(dir)
	if err != nil {
		log.Warnf("[AgentPlugin] credential store at %s is unavailable, plugin credentials will not persist: %v", dir, err)
		return &pluginCredentialStore{}
	}
	return &pluginCredentialStore{store: st}
}

// key 把 (插件, env key) 编码成存储键。
func pluginCredKey(plugin, envKey string) string {
	return pluginCredKeyPrefix + plugin + "/" + envKey
}

// Load 返回某插件的全部凭据（key → 明文值）。
func (s *pluginCredentialStore) Load(plugin string) map[string]string {
	if s == nil || s.store == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	prefix := pluginCredKeyPrefix + plugin + "/"
	out := map[string]string{}
	for _, name := range s.store.ListByProvider(pluginCredProvider) {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		v, err := s.store.Get(name)
		if err != nil {
			continue // 单条解密失败不该让整组凭据消失
		}
		out[strings.TrimPrefix(name, prefix)] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Save 用整组值覆盖某插件的凭据（空值/空表 = 删除）。
func (s *pluginCredentialStore) Save(plugin string, creds map[string]string) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("credential store is not available")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	prefix := pluginCredKeyPrefix + plugin + "/"
	existing := map[string]bool{}
	for _, name := range s.store.ListByProvider(pluginCredProvider) {
		if strings.HasPrefix(name, prefix) {
			existing[strings.TrimPrefix(name, prefix)] = true
		}
	}

	for key, value := range creds {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if strings.TrimSpace(value) == "" {
			continue
		}
		if err := s.store.Set(pluginCredKey(plugin, key), pluginCredProvider, value, nil); err != nil {
			return fmt.Errorf("store credential %s: %w", key, err)
		}
		delete(existing, key)
	}
	// 剩下的是"这次被清空的 key"（用户删掉了它）：从存储里移除，
	// 否则界面清空后重开又冒出来。
	for key := range existing {
		_ = s.store.Delete(pluginCredKey(plugin, key))
	}
	return nil
}

// Delete 删除某插件的全部凭据（卸载时调用）。
func (s *pluginCredentialStore) Delete(plugin string) {
	if s == nil || s.store == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	prefix := pluginCredKeyPrefix + plugin + "/"
	for _, name := range s.store.ListByProvider(pluginCredProvider) {
		if strings.HasPrefix(name, prefix) {
			_ = s.store.Delete(name)
		}
	}
}

// Plugins 返回存储里有凭据的插件名（供"跨插件复用同名凭据"使用）。
func (s *pluginCredentialStore) Plugins() []string {
	if s == nil || s.store == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]bool{}
	for _, name := range s.store.ListByProvider(pluginCredProvider) {
		rest := strings.TrimPrefix(name, pluginCredKeyPrefix)
		if idx := strings.LastIndex(rest, "/"); idx > 0 {
			seen[rest[:idx]] = true
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// migrateLegacyPluginCredentials 把 config.json 里的明文插件凭据搬进加密存储。
//
// 只在"配置里确实还有明文"时动作：迁移完成后把 agent_plugins.credentials
// 从内存与磁盘上一并清空（否则下次启动又会认为需要迁移）。
// 迁移失败时保留明文不动——宁可暂时不安全，也不能让用户重填一遍 key。
func (s *Server) migrateLegacyPluginCredentials() {
	if s.pluginCreds == nil || s.pluginCreds.store == nil || s.cfg == nil {
		return
	}
	legacy := s.cfg.AgentPlugins.Credentials
	if len(legacy) == 0 {
		return
	}

	migrated := 0
	for plugin, creds := range legacy {
		if len(creds) == 0 {
			continue
		}
		// 已有加密凭据时不覆盖（用户可能已经在新存储里改过值）。
		if cur := s.pluginCreds.Load(plugin); len(cur) > 0 {
			continue
		}
		if err := s.pluginCreds.Save(plugin, creds); err != nil {
			log.Warnf("[AgentPlugin] failed to migrate plaintext credentials for %q: %v", plugin, err)
			continue
		}
		migrated++
	}
	if migrated == 0 {
		return
	}

	s.lockCfgForWrite()
	s.cfg.AgentPlugins.Credentials = nil
	s.unlockCfgForWrite()
	if err := s.persistConfig(true); err != nil {
		log.Warnf("[AgentPlugin] migrated %d plugin credential set(s) but failed to rewrite config.json: %v", migrated, err)
		return
	}
	log.Infof("[AgentPlugin] migrated plaintext credentials for %d plugin(s) into the encrypted store", migrated)
}
