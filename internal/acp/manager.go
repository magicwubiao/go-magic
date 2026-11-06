package acp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// manager.go —— ACP agent 的进程内管理器。
//
// 一个 agent 名字对应一条长活连接（子进程 + session）。连接是懒建立的：
// 没人调用就不拉起子进程；进程死了（读到 EOF）就在下一次调用时重建。
//
// 这里刻意**不做**跨进程持久化：stdio 连接的生命周期不可能超过宿主进程，
// 旧版 CLI 每条命令 new 一个 Manager 再退出，于是 list/call 永远查不到东西。
// 现在的语义是"配置持久化在 acp.json，连接随宿主进程生死"。

// connectTimeout 是拉起子进程 + initialize + session/new 的总预算。
const connectTimeout = 90 * time.Second

// Manager 管理若干 ACP agent 连接。
type Manager struct {
	configPath string

	mu        sync.Mutex
	clients   map[string]*Client
	locks     map[string]*sync.Mutex
	overrides map[string]time.Duration // 单次进程内的超时覆盖（不写盘）
	closed    bool
}

// NewManager 创建管理器。configPath 为空时用 DefaultConfigPath()。
func NewManager(configPath string) *Manager {
	if configPath == "" {
		configPath = DefaultConfigPath()
	}
	return &Manager{
		configPath: configPath,
		clients:    make(map[string]*Client),
		locks:      make(map[string]*sync.Mutex),
		overrides:  make(map[string]time.Duration),
	}
}

// SetTimeoutOverride 覆盖某个 agent 的单轮超时（仅本进程，不落盘）。
// CLI 的 --timeout 用它；已建立的连接不受影响（下次重建才生效）。
func (m *Manager) SetTimeoutOverride(name string, d time.Duration) {
	if d <= 0 {
		return
	}
	m.mu.Lock()
	m.overrides[name] = d
	m.mu.Unlock()
}

var (
	defaultOnce sync.Once
	defaultMgr  *Manager
)

// Default 返回进程级单例，供工具层使用。
func Default() *Manager {
	defaultOnce.Do(func() { defaultMgr = NewManager("") })
	return defaultMgr
}

// ConfigPath 返回配置文件路径。
func (m *Manager) ConfigPath() string { return m.configPath }

// Config 读取当前磁盘配置（不缓存：用户在编辑 acp.json 后应立刻生效）。
func (m *Manager) Config() (*Config, error) { return LoadConfig(m.configPath) }

// AgentSummary 是给工具/CLI 展示的 agent 概览。
type AgentSummary struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Command     string `json:"command"`
	Enabled     bool   `json:"enabled"`
	Connected   bool   `json:"connected"`
	Workspace   string `json:"workspace,omitempty"`
}

// Agents 列出配置中的 agent。
func (m *Manager) Agents() ([]AgentSummary, error) {
	cfg, err := m.Config()
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(cfg.Agents))
	for name := range cfg.Agents {
		names = append(names, name)
	}
	sort.Strings(names)

	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]AgentSummary, 0, len(names))
	for _, name := range names {
		agent := cfg.Agents[name]
		c, connected := m.clients[name]
		if connected && c != nil && !c.alive() {
			connected = false
		}
		out = append(out, AgentSummary{
			Name:        name,
			Description: agent.Description,
			Command:     agent.Command,
			Enabled:     agent.EnabledValue(),
			Connected:   connected,
			Workspace:   agent.Workspace,
		})
	}
	return out, nil
}

// lockFor 返回某个 agent 的连接锁，保证并发调用只拉起一个子进程。
func (m *Manager) lockFor(name string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	mu, ok := m.locks[name]
	if !ok {
		mu = &sync.Mutex{}
		m.locks[name] = mu
	}
	return mu
}

// client 取得（必要时建立）某个 agent 的连接。
func (m *Manager) client(ctx context.Context, name string) (*Client, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, errPeerClosed
	}
	if c, ok := m.clients[name]; ok {
		if c.alive() {
			m.mu.Unlock()
			return c, nil
		}
		// 进程已经死了：关掉残骸，下面重建。
		delete(m.clients, name)
		go func(c *Client) { _ = c.Close() }(c)
	}
	m.mu.Unlock()

	mu := m.lockFor(name)
	mu.Lock()
	defer mu.Unlock()

	// 双检：拿锁期间别的协程可能已经建好了。
	m.mu.Lock()
	if c, ok := m.clients[name]; ok && c.alive() {
		m.mu.Unlock()
		return c, nil
	}
	m.mu.Unlock()

	cfg, err := m.Config()
	if err != nil {
		return nil, err
	}
	agent, ok := cfg.Agents[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoAgent, name)
	}
	if !agent.EnabledValue() {
		return nil, fmt.Errorf("acp agent %q is disabled", name)
	}
	if err := agent.Validate(name); err != nil {
		return nil, err
	}

	opts, err := m.clientOptions(name, agent)
	if err != nil {
		return nil, err
	}

	connectCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	client, err := Connect(connectCtx, opts)
	if err != nil {
		return nil, fmt.Errorf("connect acp agent %q: %w", name, err)
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		_ = client.Close()
		return nil, errPeerClosed
	}
	m.clients[name] = client
	m.mu.Unlock()
	return client, nil
}

func (m *Manager) clientOptions(name string, agent AgentConfig) (ClientOptions, error) {
	workspace := expandHome(agent.Workspace)
	if workspace == "" {
		wd, err := os.Getwd()
		if err != nil {
			return ClientOptions{}, fmt.Errorf("resolve workspace: %w", err)
		}
		workspace = wd
	}
	workspace, err := filepath.Abs(workspace)
	if err != nil {
		return ClientOptions{}, fmt.Errorf("resolve workspace: %w", err)
	}

	dirs := make([]string, 0, len(agent.AdditionalDirs))
	for _, d := range agent.AdditionalDirs {
		d = expandHome(d)
		abs, err := filepath.Abs(d)
		if err != nil {
			return ClientOptions{}, fmt.Errorf("resolve additional dir %s: %w", d, err)
		}
		dirs = append(dirs, abs)
	}

	return ClientOptions{
		Name:           name,
		Cwd:            workspace,
		Command:        agent.Command,
		Args:           agent.Args,
		Env:            agent.Env,
		WorkDir:        expandHome(agent.Cwd),
		Timeout:        m.timeoutFor(name, agent),
		AllowWrite:     agent.AllowWrite,
		AdditionalDirs: dirs,
		Permission:     agent.PermissionPolicy(),
		ClientName:     "magic",
		ClientVersion:  "1.0",
	}, nil
}

func (m *Manager) timeoutFor(name string, agent AgentConfig) time.Duration {
	m.mu.Lock()
	override, ok := m.overrides[name]
	m.mu.Unlock()
	if ok {
		return override
	}
	return agent.Timeout()
}

// Prompt 向指定 agent 发一轮消息。
func (m *Manager) Prompt(ctx context.Context, name string, blocks []ContentBlock, onUpdate func(Update)) (*PromptOutcome, error) {
	client, err := m.client(ctx, name)
	if err != nil {
		return nil, err
	}

	outcome, err := client.Prompt(ctx, blocks, onUpdate)
	if err != nil && !client.alive() {
		// 连接已死，丢弃残骸，下一次调用重建。
		m.drop(name, client)
	}
	return outcome, err
}

// ResetSession 重置某个 agent 的会话上下文。
func (m *Manager) ResetSession(ctx context.Context, name string) error {
	client, err := m.client(ctx, name)
	if err != nil {
		return err
	}
	resetCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	if err := client.ResetSession(resetCtx); err != nil {
		if !client.alive() {
			m.drop(name, client)
		}
		return err
	}
	return nil
}

// Cancel 取消某个 agent 正在进行的轮次。
func (m *Manager) Cancel(name string) error {
	m.mu.Lock()
	client, ok := m.clients[name]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrNoAgent, name)
	}
	return client.Cancel()
}

// Client 返回底层连接（诊断用）。
func (m *Manager) Client(ctx context.Context, name string) (*Client, error) {
	return m.client(ctx, name)
}

func (m *Manager) drop(name string, client *Client) {
	m.mu.Lock()
	if cur, ok := m.clients[name]; ok && cur == client {
		delete(m.clients, name)
	}
	m.mu.Unlock()

	// 关闭放到锁外：Close 会等子进程退出（最多 5s），
	// 持锁关闭会把其它 agent 的调用一起堵住。
	go func() { _ = client.Close() }()
}

// Close 关闭所有连接。
func (m *Manager) Close() error {
	m.mu.Lock()
	m.closed = true
	clients := m.clients
	m.clients = make(map[string]*Client)
	m.mu.Unlock()

	var errs []error
	for name, client := range clients {
		if err := client.Close(); err != nil {
			errs = append(errs, fmt.Errorf("agent %s: %w", name, err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
