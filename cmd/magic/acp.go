package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/acp"
)

var (
	acpAgentID    string
	acpTransport  string
	acpAddress    string
	acpCommand    string
	acpArgs       []string
	acpHeaders    []string
	acpParams     string
)

var acpCmd = &cobra.Command{
	Use:   "acp",
	Short: "Manage ACP (Agent Communication Protocol) connections",
	Long: `Manage ACP connections for inter-agent communication.
ACP enables agents to discover and call each other's skills.`,
}

var acpServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start an ACP server",
	Long:  `Start an ACP server that exposes agent capabilities to other agents.`,
	Run:   runACPServe,
}

var acpConnectCmd = &cobra.Command{
	Use:   "connect <name> [address]",
	Short: "Connect to an ACP agent",
	Long:  `Connect to another ACP agent using the specified transport.`,
	Args:  cobra.RangeArgs(1, 2),
	Run:   runACPConnect,
}

var acpDisconnectCmd = &cobra.Command{
	Use:   "disconnect <name>",
	Short: "Disconnect from an ACP agent",
	Args:  cobra.ExactArgs(1),
	Run:   runACPDisconnect,
}

var acpListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all connected ACP agents",
	Run:   runACPList,
}

var acpCallCmd = &cobra.Command{
	Use:   "call <agent> <skill>",
	Short: "Call a skill on a connected agent",
	Long:  `Call a skill on a connected ACP agent with optional parameters.`,
	Args:  cobra.ExactArgs(2),
	Run:   runACPCall,
}

var acpSkillsCmd = &cobra.Command{
	Use:   "skills [agent]",
	Short: "List skills from connected agents",
	Run:   runACPSkills,
}

var acpPingCmd = &cobra.Command{
	Use:   "ping [agent]",
	Short: "Check connectivity to ACP agents",
	Run:   runACPPing,
}

func init() {
	rootCmd.AddCommand(acpCmd)

	// Serve subcommand flags
	acpServeCmd.Flags().StringVar(&acpAgentID, "agent-id", "", "Agent ID for the server")
	acpServeCmd.Flags().StringVar(&acpTransport, "transport", "stdio", "Transport type: stdio, http, sse")
	acpServeCmd.Flags().StringVar(&acpAddress, "address", ":8080", "Address/port for http/sse transport")

	// Connect subcommand flags
	acpConnectCmd.Flags().StringVar(&acpTransport, "transport", "http", "Transport type: http, sse, stdio")
	acpConnectCmd.Flags().StringVar(&acpAddress, "address", "", "Address for http/sse transport")
	acpConnectCmd.Flags().StringVar(&acpCommand, "command", "", "Command for stdio transport")
	acpConnectCmd.Flags().StringArrayVar(&acpArgs, "args", []string{}, "Arguments for stdio transport")
	acpConnectCmd.Flags().StringArrayVar(&acpHeaders, "header", []string{}, "HTTP headers (key=value)")

	// Call subcommand flags
	acpCallCmd.Flags().StringVar(&acpParams, "params", "", "JSON parameters for the skill call")

	// Add subcommands
	acpCmd.AddCommand(acpServeCmd)
	acpCmd.AddCommand(acpConnectCmd)
	acpCmd.AddCommand(acpDisconnectCmd)
	acpCmd.AddCommand(acpListCmd)
	acpCmd.AddCommand(acpCallCmd)
	acpCmd.AddCommand(acpSkillsCmd)
	acpCmd.AddCommand(acpPingCmd)
}

func runACPServe(cmd *cobra.Command, args []string) {
	agentID := acpAgentID
	if agentID == "" {
		agentID = getDefaultAgentID()
	}

	info := acp.AgentInfo{
		ID:      agentID,
		Name:    agentID,
		Version: acp.ProtocolVersion,
		Capabilities: []string{
			"skill/call",
			"skill/list",
			"message/send",
			"memory/share",
			"agent/info",
		},
	}

	var transport acp.Transport
	var err error

	switch acpTransport {
	case "stdio":
		// For stdio, we'll use stdin/stdout
		transport, err = createStdioTransport()
		if err != nil {
			fmt.Printf("Failed to create stdio transport: %v\n", err)
			os.Exit(1)
		}
	case "http":
		// HTTP server would need to be implemented separately
		fmt.Println("HTTP transport for serve not yet implemented")
		os.Exit(1)
	case "sse":
		fmt.Println("SSE transport for serve not yet implemented")
		os.Exit(1)
	default:
		fmt.Printf("Unknown transport type: %s\n", acpTransport)
		os.Exit(1)
	}

	manager := acp.NewManager()
	server, err := manager.StartServer("local", agentID, info, transport)
	if err != nil {
		fmt.Printf("Failed to start server: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("ACP server started: %s\n", agentID)
	fmt.Printf("Transport: %s\n", acpTransport)
	fmt.Println("Press Ctrl+C to stop")

	// Wait for context cancellation
	select {}
	_ = server
}

func runACPConnect(cmd *cobra.Command, args []string) {
	name := args[0]
	manager := acp.NewManager()

	// Parse headers
	headers := parseHeaders(acpHeaders)

	var address string
	if len(args) > 1 {
		address = args[1]
	} else if acpAddress != "" {
		address = acpAddress
	}

	var err error
	switch acpTransport {
	case "http":
		if address == "" {
			fmt.Println("Error: address required for HTTP transport")
			os.Exit(1)
		}
		err = manager.ConnectHTTP(name, name, address, headers)
	case "sse":
		if address == "" {
			fmt.Println("Error: address required for SSE transport")
			os.Exit(1)
		}
		err = manager.ConnectSSE(name, name, address, headers)
	case "stdio":
		if acpCommand == "" {
			fmt.Println("Error: command required for stdio transport")
			os.Exit(1)
		}
		err = manager.ConnectStdio(name, name, acpCommand, acpArgs, nil)
	default:
		fmt.Printf("Unknown transport type: %s\n", acpTransport)
		os.Exit(1)
	}

	if err != nil {
		fmt.Printf("Failed to connect: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Connected to agent '%s'\n", name)

	// Get agent info
	client, _ := manager.GetClient(name)
	info, _ := client.GetAgentInfo(cmd.Context())
	if info != nil {
		fmt.Printf("  Agent: %s (v%s)\n", info.Name, info.Version)
		fmt.Printf("  Capabilities: %v\n", info.Capabilities)
	}
}

func runACPDisconnect(cmd *cobra.Command, args []string) {
	name := args[0]
	manager := acp.NewManager()

	if err := manager.Disconnect(name); err != nil {
		fmt.Printf("Failed to disconnect: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Disconnected from agent '%s'\n", name)
}

func runACPList(cmd *cobra.Command, args []string) {
	manager := acp.NewManager()

	agents := manager.ListConnectedAgents()
	if len(agents) == 0 {
		fmt.Println("No ACP agents connected.")
		fmt.Println("Use 'magic acp connect <name> <address>' to connect.")
		return
	}

	fmt.Printf("Connected ACP agents (%d):\n", len(agents))
	for _, agent := range agents {
		fmt.Printf("  - %s (%s)\n", agent.Name, agent.ID)
		fmt.Printf("    Version: %s\n", agent.Version)
		if len(agent.Capabilities) > 0 {
			fmt.Printf("    Capabilities: %v\n", agent.Capabilities)
		}
	}
}

func runACPCall(cmd *cobra.Command, args []string) {
	agentName := args[0]
	skillName := args[1]
	manager := acp.NewManager()

	// Parse params if provided
	var params map[string]interface{}
	if acpParams != "" {
		if err := json.Unmarshal([]byte(acpParams), &params); err != nil {
			fmt.Printf("Invalid params JSON: %v\n", err)
			os.Exit(1)
		}
	}

	result, err := manager.CallSkill(cmd.Context(), agentName, skillName, params)
	if err != nil {
		fmt.Printf("Skill call failed: %v\n", err)
		os.Exit(1)
	}

	// Print result
	if result != nil {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
	} else {
		fmt.Println("Skill executed successfully (no output)")
	}
}

func runACPSkills(cmd *cobra.Command, args []string) {
	manager := acp.NewManager()

	if len(args) > 0 {
		// List skills from specific agent
		agentName := args[0]
		client, err := manager.GetClient(agentName)
		if err != nil {
			fmt.Printf("Agent '%s' not found: %v\n", agentName, err)
			os.Exit(1)
		}

		skills, err := client.ListSkills(cmd.Context())
		if err != nil {
			fmt.Printf("Failed to list skills: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Skills from '%s' (%d):\n", agentName, len(skills))
		for _, skill := range skills {
			fmt.Printf("  - %s: %s\n", skill.Name, skill.Description)
		}
	} else {
		// List all skills
		skills := manager.ListAllSkills()
		if len(skills) == 0 {
			fmt.Println("No skills available.")
			return
		}

		fmt.Printf("All available skills (%d):\n", len(skills))
		for _, skill := range skills {
			source := skill.Source
			if source == "" {
				source = "unknown"
			}
			fmt.Printf("  - %s [%s]: %s\n", skill.Name, source, skill.Description)
		}
	}
}

func runACPPing(cmd *cobra.Command, args []string) {
	manager := acp.NewManager()

	if len(args) > 0 {
		// Ping specific agent
		agentName := args[0]
		if err := manager.Ping(cmd.Context(), agentName); err != nil {
			fmt.Printf("❌ %s: %v\n", agentName, err)
			os.Exit(1)
		}
		fmt.Printf("✅ %s: healthy\n", agentName)
	} else {
		// Ping all agents
		health := manager.HealthCheck()
		anyUnhealthy := false

		for name, ok := range health {
			if ok {
				fmt.Printf("✅ %s: healthy\n", name)
			} else {
				fmt.Printf("❌ %s: unhealthy\n", name)
				anyUnhealthy = true
			}
		}

		if len(health) == 0 {
			fmt.Println("No agents to ping.")
		}

		if anyUnhealthy {
			os.Exit(1)
		}
	}
}

// Helper functions

func parseHeaders(headers []string) map[string]string {
	result := make(map[string]string)
	for _, h := range headers {
		for i := 0; i < len(h); i++ {
			if h[i] == '=' {
				result[h[:i]] = h[i+1:]
				break
			}
		}
	}
	return result
}

func getDefaultAgentID() string {
	// Could use hostname or other identifier
	return "agent"
}

func createStdioTransport() (acp.Transport, error) {
	// In a real implementation, this would create a subprocess
	// For now, return an error indicating it needs implementation
	return nil, fmt.Errorf("stdio transport for serve not implemented in CLI")
}
