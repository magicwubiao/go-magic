package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/mcp"
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
	Use:   "add <server-name>",
	Short: "Add MCP server configuration",
	Args:  cobra.ExactArgs(1),
	Run:   runMCPAdd,
}

func init() {
	rootCmd.AddCommand(mcpCmd)
	mcpCmd.AddCommand(mcpListCmd)
	mcpCmd.AddCommand(mcpConnectCmd)
	mcpCmd.AddCommand(mcpDisconnectCmd)
	mcpCmd.AddCommand(mcpHealthCmd)
	mcpCmd.AddCommand(mcpAddCmd)
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
	config := mcp.ServerConfig{
		Command:   command,
		Args:      serverArgs,
		Transport: "stdio",
	}

	if err := mgr.ConnectStdio(serverName, config); err != nil {
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
	cfg.MCP.Servers[serverName] = config

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

func runMCPAdd(cmd *cobra.Command, args []string) {
	serverName := args[0]

	fmt.Printf("Adding MCP server '%s'\n", serverName)
	fmt.Println("\nConfiguration options:")
	fmt.Println("  --command <command>   Command to run (e.g., npx, python)")
	fmt.Println("  --args <args>         Arguments (e.g., -y @modelcontextprotocol/server-filesystem /tmp)")
	fmt.Println("  --transport <type>    Transport type: stdio or sse (default: stdio)")
	fmt.Println("  --url <url>           URL for SSE transport")

	fmt.Println("\nExample:")
	fmt.Printf("  magic mcp connect %s npx -y @modelcontextprotocol/server-filesystem /tmp\n", serverName)
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
