package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/mcp"
	"github.com/magicwubiao/go-magic/internal/mcpbridge"
	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/pkg/config"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Manage MCP (Model Context Protocol) servers",
	Long: `Manage MCP servers for extended tool capabilities.
Supports both stdio and SSE transport protocols.`,
}

var mcpListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all connected MCP servers and their tools",
	Run:   runMCPList,
}

var mcpConnectCmd = &cobra.Command{
	Use:   "connect <server-name> <command> [args...]",
	Short: "Connect to an MCP server",
	Args:  cobra.MinimumNArgs(2),
	Run:   runMCPConnect,
}

var mcpDisconnectCmd = &cobra.Command{
	Use:   "disconnect <server-name>",
	Short: "Disconnect an MCP server",
	Args:  cobra.ExactArgs(1),
	Run:   runMCPDisconnect,
}

var mcpHealthCmd = &cobra.Command{
	Use:   "health [server-name]",
	Short: "Check health of MCP server(s)",
	Args:  cobra.RangeArgs(0, 1),
	Run:   runMCPHealth,
}

var mcpAddCmd = &cobra.Command{
	Use:   "add [server-name]",
	Short: "Add MCP server(s) to config.json and connect",
	Long: `Add one or more MCP servers, save them to config.json and connect.

JSON mode accepts the same shapes other MCP clients use (Claude Desktop / Cursor /
Cline), so a config snippet can be pasted as-is:

  magic mcp add --json '{"mcpServers": {"filesystem": {"command": "npx", "args": ["-y", "@modelcontextprotocol/server-filesystem", "/data"]}}}'
  cat mcp.json | magic mcp add --json -
  magic mcp add --file ./mcp.json

Flag mode adds a single server (name is positional):

  magic mcp add filesystem --command npx --args "-y @modelcontextprotocol/server-filesystem /data"
  magic mcp add github --transport sse --url http://localhost:3000/sse --env GITHUB_TOKEN=xxx`,
	Args: cobra.MaximumNArgs(1),
	Run:  runMCPAdd,
}

// mcp add 的旗标。JSON 与旗标两种写法共用同一条解析/校验路径（见 runMCPAdd）。
var (
	mcpAddJSON      string
	mcpAddFile      string
	mcpAddCommand   string
	mcpAddArgs      []string
	mcpAddEnv       []string
	mcpAddTransport string
	mcpAddURL       string
)

func init() {
	rootCmd.AddCommand(mcpCmd)
	mcpCmd.AddCommand(mcpListCmd)
	mcpCmd.AddCommand(mcpConnectCmd)
	mcpCmd.AddCommand(mcpDisconnectCmd)
	mcpCmd.AddCommand(mcpHealthCmd)
	mcpCmd.AddCommand(mcpAddCmd)

	mcpAddCmd.Flags().StringVar(&mcpAddJSON, "json", "", `JSON config for one or more servers ("-" reads stdin)`)
	mcpAddCmd.Flags().StringVar(&mcpAddFile, "file", "", "read the JSON config from a file")
	mcpAddCmd.Flags().StringVar(&mcpAddCommand, "command", "", "stdio: command to run (e.g. npx)")
	mcpAddCmd.Flags().StringArrayVar(&mcpAddArgs, "args", nil, "stdio: arguments (repeatable, or one quoted string)")
	mcpAddCmd.Flags().StringArrayVar(&mcpAddEnv, "env", nil, "stdio: environment variable, KEY=value (repeatable)")
	mcpAddCmd.Flags().StringVar(&mcpAddTransport, "transport", "", "transport: stdio or sse (default: inferred from --command/--url)")
	mcpAddCmd.Flags().StringVar(&mcpAddURL, "url", "", "sse: server URL")
}

func runMCPList(cmd *cobra.Command, args []string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	mgr := mcp.NewManager()

	// Load configured servers
	if cfg.MCP != nil && cfg.MCP.Servers != nil {
		loader := &mcp.ConfigLoader{}
		if err := loader.LoadFromConfig(mgr, cfg.MCP.Servers); err != nil {
			fmt.Printf("Warning: Some MCP servers failed to connect: %v\n", err)
		}
	}

	servers := mgr.ListServers()
	if len(servers) == 0 {
		fmt.Println("No MCP servers configured. Run 'magic mcp add <name>' to add one.")
		return
	}

	fmt.Println("Connected MCP servers:")
	for _, name := range servers {
		info, err := mgr.GetServerInfo(name)
		if err != nil {
			fmt.Printf("  - %s: error getting info\n", name)
			continue
		}

		fmt.Printf("  - %s (%s)\n", name, info["transport"])
		fmt.Printf("    Tools: %d\n", info["tool_count"])

		tools, _ := mgr.ListToolsByServer(name)
		for _, tool := range tools {
			fmt.Printf("      • %s: %s\n", tool.Name, tool.Description)
		}
	}
}

func runMCPConnect(cmd *cobra.Command, args []string) {
	serverName := args[0]
	command := args[1]
	serverArgs := args[2:]

	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	mgr := mcp.NewManager()

	// Try to connect
	serverCfg := mcp.ServerConfig{
		Command:   command,
		Args:      serverArgs,
		Transport: "stdio",
	}

	if err := mgr.ConnectStdio(serverName, serverCfg); err != nil {
		fmt.Printf("Failed to connect: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully connected to MCP server '%s'\n", serverName)

	// Save to config
	if cfg.MCP == nil {
		cfg.MCP = &config.MCPConfig{}
	}
	if cfg.MCP.Servers == nil {
		cfg.MCP.Servers = make(map[string]mcp.ServerConfig)
	}
	cfg.MCP.Servers[serverName] = serverCfg

	if err := cfg.Save(); err != nil {
		fmt.Printf("Warning: Failed to save config: %v\n", err)
	} else {
		fmt.Println("Configuration saved.")
	}
}

func runMCPDisconnect(cmd *cobra.Command, args []string) {
	serverName := args[0]

	mgr := mcp.NewManager()

	if err := mgr.Disconnect(serverName); err != nil {
		fmt.Printf("Failed to disconnect: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Disconnected MCP server '%s'\n", serverName)

	// Remove from config
	cfg, err := config.Load()
	if err == nil && cfg.MCP != nil && cfg.MCP.Servers != nil {
		delete(cfg.MCP.Servers, serverName)
		cfg.Save()
	}
}

func runMCPHealth(cmd *cobra.Command, args []string) {
	mgr := mcp.NewManager()

	cfg, err := config.Load()
	if err == nil && cfg.MCP != nil && cfg.MCP.Servers != nil {
		loader := &mcp.ConfigLoader{}
		loader.LoadFromConfig(mgr, cfg.MCP.Servers)
	}

	servers := mgr.ListServers()
	if len(servers) == 0 {
		fmt.Println("No MCP servers connected.")
		return
	}

	for _, name := range servers {
		if len(args) > 0 && args[0] != name {
			continue
		}

		if err := mgr.HealthCheck(name); err != nil {
			fmt.Printf("❌ %s: %v\n", name, err)
		} else {
			fmt.Printf("✅ %s: healthy\n", name)
		}
	}
}

// collectMCPAddServers 把 --json / --file / 旗标三种输入统一成待添加的服务器列表。
//
// --json / --file 支持一次加多个（外部配置片段天然是多服务器的）；带位置参数
// 时只允许 JSON 里恰好有一个服务器，位置参数作为它的名字（方便改名导入）。
func collectMCPAddServers(args []string) ([]mcp.NamedServerConfig, error) {
	if mcpAddJSON != "" || mcpAddFile != "" {
		raw, err := readMCPAddJSON()
		if err != nil {
			return nil, err
		}
		servers, err := mcp.ParseServersJSON(raw)
		if err != nil {
			return nil, err
		}
		if len(args) == 0 {
			return servers, nil
		}
		if len(servers) != 1 {
			return nil, fmt.Errorf("the JSON holds %d servers, so a server name argument is ambiguous; drop it", len(servers))
		}
		servers[0].Name = strings.TrimSpace(args[0])
		return servers, nil
	}

	if len(args) == 0 {
		return nil, fmt.Errorf("a server name is required (or pass --json / --file)")
	}

	// --args 的每个取值都按 shell 引号规则再切一次：`--args "-y pkg"` 是用户最自然的
	// 写法，而 JSON 数组里的元素是字面量（不切分），所以切分只能在这一层做。
	flagArgs := make([]string, 0, len(mcpAddArgs))
	for _, a := range mcpAddArgs {
		flagArgs = append(flagArgs, mcp.SplitCommandLine(a)...)
	}

	// 旗标写法也走 JSON 解析器：命令/URL 推断传输方式、env 形状、参数切分
	// 三处规则只保留一份实现。
	payload := map[string]interface{}{
		"name":      args[0],
		"transport": mcpAddTransport,
		"command":   mcpAddCommand,
		"args":      flagArgs,
		"env":       mcpAddEnv,
		"url":       mcpAddURL,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return mcp.ParseServersJSON(body)
}

func readMCPAddJSON() ([]byte, error) {
	if mcpAddFile != "" {
		data, err := os.ReadFile(config.ExpandHome(mcpAddFile))
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", mcpAddFile, err)
		}
		return data, nil
	}
	if mcpAddJSON == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("failed to read stdin: %w", err)
		}
		return data, nil
	}
	return []byte(mcpAddJSON), nil
}

func runMCPAdd(cmd *cobra.Command, args []string) {
	servers, err := collectMCPAddServers(args)
	if err != nil {
		fmt.Printf("Invalid MCP server config: %v\n", err)
		os.Exit(1)
	}

	// Load 在没有 config.json 时返回 (默认配置, ErrNoConfig)：首次使用时
	// "添加一个 MCP 服务器"不应该因为"还没有配置文件"而失败 —— 返回的默认配置
	// 本身可写、可保存。
	cfg, err := config.Load()
	if err != nil && !errors.Is(err, config.ErrNoConfig) {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}
	if cfg == nil {
		fmt.Println("Failed to load config")
		os.Exit(1)
	}
	if errors.Is(err, config.ErrNoConfig) {
		fmt.Printf("No config file yet; creating %s\n", filepath.Join(config.GetMagicHome(), "config.json"))
	}
	if cfg.MCP == nil {
		cfg.MCP = &config.MCPConfig{}
	}
	if cfg.MCP.Servers == nil {
		cfg.MCP.Servers = make(map[string]mcp.ServerConfig)
	}
	for _, s := range servers {
		cfg.MCP.Servers[s.Name] = s.Config
	}
	if err := cfg.Save(); err != nil {
		fmt.Printf("Failed to save config: %v\n", err)
		os.Exit(1)
	}

	// 落盘在前、连接在后：配置是用户真正要留下的东西，连接失败（npx 没装、
	// URL 写错）不应该把它一起丢掉。
	mgr := mcp.NewManager()
	defer mgr.DisconnectAll()

	failed := 0
	for _, s := range servers {
		if err := connectMCPServerWithConfig(mgr, s.Name, s.Config); err != nil {
			fmt.Printf("❌ %s: failed to connect: %v\n", s.Name, err)
			failed++
			continue
		}
		tools, _ := mgr.ListToolsByServer(s.Name)
		fmt.Printf("✅ %s (%s): connected, %d tool(s)\n", s.Name, s.Config.Transport, len(tools))
		for _, tool := range tools {
			fmt.Printf("     • %s: %s\n", tool.Name, tool.Description)
		}
	}

	fmt.Printf("\nSaved %d server(s) to %s\n", len(servers), filepath.Join(config.GetMagicHome(), "config.json"))
	if failed > 0 {
		// 配置已经写盘，但至少有一个连不上：退出码非 0，同时说清"配置没丢"。
		fmt.Printf("Config was saved; %d server(s) could not be connected.\n", failed)
		os.Exit(1)
	}
	fmt.Println("Run 'magic mcp list' to see them.")
}

func connectMCPServerWithConfig(mgr *mcp.Manager, name string, cfg mcp.ServerConfig) error {
	if cfg.Transport == mcp.TransportSSE {
		return mgr.ConnectSSE(name, cfg)
	}
	return mgr.ConnectStdio(name, cfg)
}

// bridgeConfiguredMCPTools 把 config 中配置的独立 MCP server 连接起来，并将其
// 发现的工具以 mcp_<server>_<tool> 名称注册进 reg（与 internal/server 的
// initStandaloneMCP 行为一致）。返回的清理函数用于断开所有 MCP server；调用方
// 应在入口退出前 defer 它。reg/cfg 为空或未配置任何 MCP server 时返回 no-op。
func bridgeConfiguredMCPTools(reg *tool.Registry, cfg *config.Config) func() {
	if reg == nil || cfg == nil || cfg.MCP == nil || len(cfg.MCP.Servers) == 0 {
		return func() {}
	}
	mgr := mcp.NewManager()
	if err := mcpbridge.ConnectAndSync(mgr, reg, cfg.MCP.Servers); err != nil {
		fmt.Fprintf(os.Stderr, "[MCP] standalone MCP servers partially failed: %v\n", err)
	}
	return func() { mgr.DisconnectAll() }
}

func initMCPFromConfig(mgr *mcp.Manager) {
	cfg, err := config.Load()
	if err != nil {
		return
	}

	if cfg.MCP != nil && cfg.MCP.Servers != nil {
		loader := &mcp.ConfigLoader{}
		loader.LoadFromConfig(mgr, cfg.MCP.Servers)
	}
}

// PrintMCPServers prints MCP servers in JSON format
func printMCPServersJSON(mgr *mcp.Manager) {
	servers := mgr.ListServers()
	result := make(map[string]interface{})

	for _, name := range servers {
		info, _ := mgr.GetServerInfo(name)
		tools, _ := mgr.ListToolsByServer(name)
		info["tools"] = tools
		result[name] = info
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(data))
}
