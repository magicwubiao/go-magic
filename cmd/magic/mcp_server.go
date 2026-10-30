package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/agent"
	"github.com/magicwubiao/go-magic/internal/agent/hooks"
	"github.com/magicwubiao/go-magic/internal/mcpserver"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/internal/skills"
	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// `magic mcp-server`：把本机 agent 反向暴露成一个 MCP server。
//
// 与 `magic mcp list/add`（管理"我连别人"）方向相反：这里让别人的 agent
// （Claude Desktop、IDE、另一个 go-magic）把"我这台机器上的 agent"当成一个工具调用。
//
// 三条铁律：
//  1. **stdout 只走协议**。日志/提示一律切到 stderr，一行杂讯就会让客户端解析失败。
//  2. **审批默认拒绝**。stdio 通道上没有"人"可问，需要确认的工具默认拒绝，
//     调用方显式传 approval=allow 才放行（fail-closed，和网关同一条原则）。
//  3. **工具注册表只有一份**。所有会话共享，避免每个会话各连一遍 MCP/CLI 连接器。

var (
	mcpServerCwd      string
	mcpServerApproval string
	mcpServerTimeout  int
)

var mcpServerCmd = &cobra.Command{
	Use:   "mcp-server",
	Short: "Run go-magic as an MCP server over stdio (expose the agent as a tool)",
	Long: `Run go-magic as an MCP server over stdio.

The agent itself becomes the tool surface: a client calls "magic" with a prompt,
gets the final answer plus a thread_id, and continues with "magic_reply".
Tools that normally need human confirmation are refused unless the caller passes
approval="allow" (there is nobody to ask on a stdio pipe).

Example client config:
  {"mcpServers": {"magic": {"command": "magic", "args": ["mcp-server", "--cwd", "/path/to/project"]}}}`,
	RunE: runMCPServer,
}

func init() {
	rootCmd.AddCommand(mcpServerCmd)
	mcpServerCmd.Flags().StringVar(&mcpServerCwd, "cwd", "", "working directory for the agent (default: current directory)")
	mcpServerCmd.Flags().StringVar(&mcpServerApproval, "approval", "deny", "default approval policy: deny | allow (callers can override per call)")
	mcpServerCmd.Flags().IntVar(&mcpServerTimeout, "timeout", 1800, "per-call timeout in seconds")
}

func runMCPServer(cmd *cobra.Command, args []string) error {
	// 必须在任何工具/连接器初始化之前切走：pkg/log 默认写 stdout。
	log.SetOutput(os.Stderr)

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	workDir := strings.TrimSpace(mcpServerCwd)
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	if abs, err := filepath.Abs(workDir); err == nil {
		workDir = abs
	}

	backend, err := newMCPBackend(cfg, workDir, strings.EqualFold(mcpServerApproval, "allow"))
	if err != nil {
		return err
	}
	defer backend.Close()

	srv := mcpserver.New(backend, os.Stdin, os.Stdout)
	if mcpServerTimeout > 0 {
		srv.SetCallTimeout(time.Duration(mcpServerTimeout) * time.Second)
	}
	log.Infof("[MCP-SERVER] serving on stdio (cwd=%s, approval=%s)", workDir, mcpServerApproval)
	return srv.Serve(context.Background())
}

// mcpServerBackend 用一份工具注册表 + 每条会话一个 agent 实现 mcpserver.Backend。
type mcpServerBackend struct {
	provider       provider.Provider
	registry       *tool.Registry
	tools          []map[string]interface{}
	systemPrompt   string
	cleanup        func()
	allowByDefault bool

	mu      sync.Mutex
	threads map[string]*mcpThread
}

// mcpThread 是一条独立会话。
//
// agent 实例不共享：历史、工具调用中间态都挂在 agent 上，
// 共享它等于让两个客户端互相串话。
type mcpThread struct {
	id    string
	agent *agent.Agent

	mu            sync.Mutex // 串行化同一会话的多个请求
	allowApproval bool
}

func (t *mcpThread) setAllowApproval(v bool) {
	t.mu.Lock()
	t.allowApproval = v
	t.mu.Unlock()
}

func (t *mcpThread) approvalAllowed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.allowApproval
}

func newMCPBackend(cfg *config.Config, workDir string, allowByDefault bool) (*mcpServerBackend, error) {
	prov, err := config.CreateProvider(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider: %w", err)
	}

	registry := tool.NewRegistry()
	registry.SetStrictCommandList(cfg.Tools.StrictCommandList)
	registry.RegisterAll(workDir)
	registry.SetToolsetFilter(cfg.Tools.Enabled, cfg.Tools.Disabled)

	if skillMgr, err := skills.NewManager(); err == nil {
		registry.RegisterSkillTool(skillMgr)
	} else {
		log.Warnf("[MCP-SERVER] skills unavailable: %v", err)
	}

	// 与 chat / web 走同一条桥接：连接器在两个入口的行为必须一致，
	// 否则"同一个工具在 chat 里能用、在这个 server 里不能用"会变成长期坑。
	cleanupMCP := bridgeConfiguredMCPTools(registry, cfg)
	if n := bridgeConfiguredCLITools(registry, cfg); n > 0 {
		log.Infof("[MCP-SERVER] %d CLI connector tool(s) available", n)
	}

	// cfg.WorkingDir 只用于提示词/工具基目录；这里以 --cwd 为准重建一份，
	// 避免 server 进程的 cwd 与配置里的 working_dir 各说各话。
	cfgCopy := *cfg
	cfgCopy.WorkingDir = workDir

	return &mcpServerBackend{
		provider:       prov,
		registry:       registry,
		tools:          getToolsSchema(registry),
		systemPrompt:   buildSystemPrompt(&cfgCopy, false),
		cleanup:        cleanupMCP,
		allowByDefault: allowByDefault,
		threads:        make(map[string]*mcpThread),
	}, nil
}

// NewThread 创建一个新会话。
func (b *mcpServerBackend) NewThread(ctx context.Context, opts mcpserver.ThreadOptions) (string, error) {
	id := mcpserver.NewThreadID()
	t := &mcpThread{id: id, allowApproval: opts.AllowApproval}

	// 审批策略绑定在会话上：hook 通过指针读当前值，
	// 因此同一会话后续调用可以逐次改变策略，而不用重建 agent。
	hook := &mcpApprovalHook{thread: t}
	t.agent = agent.NewEnhancedAgent(b.provider, b.registry, b.tools, b.systemPrompt,
		agent.WithHooks(hooks.HookRegistration{
			Name:   "mcp-server-approval",
			Source: hooks.HookSourceProcess,
			Hook:   hook,
		}),
	)

	b.mu.Lock()
	b.threads[id] = t
	b.mu.Unlock()
	return id, nil
}

// Reply 在既有会话里跑一轮对话。
func (b *mcpServerBackend) Reply(ctx context.Context, threadID, prompt string) (string, error) {
	b.mu.Lock()
	t, ok := b.threads[threadID]
	b.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("thread %q not found (start a new one with the magic tool)", threadID)
	}

	// 同一会话串行：agent 的历史是可变状态，两个并发回合会互相覆盖。
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.agent.RunConversation(ctx, prompt)
}

// Close 断开连接器（stdio server 通常跑在客户端进程生命周期里）。
func (b *mcpServerBackend) Close() {
	if b.cleanup != nil {
		b.cleanup()
	}
}

// mcpApprovalHook 把"要不要人工确认"翻译成 MCP 场景下的决策。
//
// 只在工具自报 RequiresApproval 时才介入：其余工具照常放行，
// 保持与 CLI/TUI 一致的语义（不审批的工具在哪儿都不审批）。
type mcpApprovalHook struct {
	thread *mcpThread
}

func (h *mcpApprovalHook) Name() string { return "mcp-server-approval" }

func (h *mcpApprovalHook) BeforeTool(ctx context.Context, call *hooks.ToolCallHookRequest) (*hooks.ToolCallHookRequest, hooks.HookDecision, error) {
	if !call.RequiresApproval {
		return call, hooks.HookDecision{Action: hooks.HookActionContinue}, nil
	}
	if h.thread.approvalAllowed() {
		return call, hooks.HookDecision{Action: hooks.HookActionContinue}, nil
	}
	return call, hooks.HookDecision{
		Action: hooks.HookActionReject,
		Reason: fmt.Sprintf("%s requires human confirmation, and this MCP server has no one to ask; "+
			"retry with approval=\"allow\" if this action is expected", call.ApprovalSummary),
	}, nil
}

func (h *mcpApprovalHook) AfterTool(ctx context.Context, res *hooks.ToolResultHookResponse) (*hooks.ToolResultHookResponse, hooks.HookDecision, error) {
	return res, hooks.HookDecision{Action: hooks.HookActionContinue}, nil
}
