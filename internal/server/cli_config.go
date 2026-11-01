package server

// CLI 连接器在服务端的**配置侧**（没有 HTTP 管理面）。
//
// 历史：这里曾经有一整套 /api/cli/connectors CRUD + /api/cli/parse +
// /api/cli/test，配套连接器页的「CLI」页签。那条路已经移除——CLI 连接器
// 现在只有两个入口：
//  1. 目录卡片（pkg/clicatalog → Agent Plugin，走 /api/agent-plugins/*）；
//  2. 配置文件里的 cli 段（cli.json / config.json），给 TUI 与团队分发用。
//
// 之所以保留配置侧：`magic` 启动的会话、以及拷过来的 cli.json 仍然要生效。
// 移除的是"在 Web UI 里手写连接器"，不是"读取已存在的连接器定义"。

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/magicwubiao/go-magic/internal/cliconn"
	"github.com/magicwubiao/go-magic/internal/cliconnbridge"
	"github.com/magicwubiao/go-magic/pkg/log"
)

const maxCLIConnectorBody = 512 << 10

// applyCLIConnectors 用当前配置重建 Manager 并把工具同步进注册表。
//
// 整体替换而不是增量改：CLI 定义之间没有共享状态，重建成本是"拷一个 map"，
// 换来的是"任何一次改动后注册表必然等于配置"，不会漏掉被删/被禁用的工具。
func (s *Server) applyCLIConnectors() {
	if s == nil || s.toolReg == nil {
		return
	}
	defs := s.cliConnectorConfigs()

	// 与保存并发时，cliMgr 的赋值与注册表同步必须整体串行：否则 A 先把新工具
	// 注册进去、B 再用旧定义覆盖 cliMgr，注册表与内存定义就会长期不一致。
	// 策略在持锁前取：cfgSnapshot 走 cfgMu，持 s.mu 再进它是反序。
	pol := s.policySnapshot()
	s.acquireServerMu("applyCLIConnectors")
	mgr, count := cliconnbridge.BuildAndSync(defs, s.toolReg, s.activeProfile(), pol)
	s.cliMgr = mgr
	s.releaseServerMu()

	log.Infof("[CLI] connectors synced: %d connectors, %d tools", len(defs), count)
}

// initCLIConnectors 在启动阶段加载 config.json 的 cli 段。
//
// 与 MCP 不同，这里没有握手/子进程，纯内存注册，因此同步执行即可。
func (s *Server) initCLIConnectors() {
	if s == nil || s.toolReg == nil {
		return
	}
	defs := s.cliConnectorConfigs()
	if len(defs) == 0 {
		return
	}
	s.applyCLIConnectors()
}

// cliConnectorConfigs 返回配置里的连接器定义（副本）。
func (s *Server) cliConnectorConfigs() map[string]cliconn.Connector {
	out := make(map[string]cliconn.Connector)
	cfg := s.cfgSnapshot()
	if cfg == nil || cfg.Cli == nil {
		return out
	}
	for name, c := range cfg.Cli.Connectors {
		out[name] = c
	}
	// 声明了 OAuth 的连接器在执行期按需取 token（见 cli_oauth.go）。
	// 放在这里而不是持久化时写死：token 会过期/刷新，配置里只存端点。
	s.withCLIOAuthTokenSource(out)
	return out
}

// decodeCLIBody 读取并解析 JSON 请求体（登录会话接口在用）。
func decodeCLIBody(r *http.Request, dst interface{}) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxCLIConnectorBody+1))
	if err != nil {
		return fmt.Errorf("failed to read request body: %w", err)
	}
	if len(body) > maxCLIConnectorBody {
		return fmt.Errorf("request body too large (max %d bytes)", maxCLIConnectorBody)
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return fmt.Errorf("request body is empty")
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}
