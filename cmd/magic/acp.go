package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/acp"
)

// cmd/magic/acp.go —— `magic acp` 子命令。
//
// 语义与旧版（已删除）的关键差异：**没有 connect/disconnect/list/ping**。
// ACP 走 stdio，连接的生命周期不可能超过宿主进程，所以"先 connect 再 call"
// 在 CLI 里永远不成立（旧版每条命令 new 一个 manager，list 永远是空的）。
// 现在每个命令都是"读配置 → 按需拉起 agent → 完成这一件事 → 退出"，
// 持久化的是配置（acp.json），不是连接。

var (
	acpConfigPath string
	acpTimeoutSec int
	acpReset      bool
	acpJSONOut    bool
)

var acpCmd = &cobra.Command{
	Use:   "acp",
	Short: "Run external ACP (Agent Client Protocol) agents",
	Long: `Drive external agents that speak the Agent Client Protocol over stdio.

Configured in ` + acp.ConfigFileName + ` under the magic home directory:

  {
    "agents": {
      "claude": {
        "command": "npx",
        "args": ["-y", "@zed-industries/claude-code-acp"],
        "workspace": "/path/to/project",
        "description": "Claude Code as an ACP agent"
      }
    }
  }

Every command connects on demand and exits when done: ACP is a stdio protocol,
so there is nothing to persist between invocations.`,
}

var acpAgentsCmd = &cobra.Command{
	Use:   "agents",
	Short: "List configured ACP agents",
	Run:   runACPAgents,
}

var acpPromptCmd = &cobra.Command{
	Use:   "prompt <agent> <prompt>",
	Short: "Send one prompt to an ACP agent",
	Long: `Send one prompt and stream the agent's output.

Use "-" as the prompt to read it from stdin. Press Ctrl+C to cancel the turn:
the client sends session/cancel and waits for the agent to end the turn.`,
	Args: cobra.MinimumNArgs(2),
	Run:  runACPPrompt,
}

var acpResetCmd = &cobra.Command{
	Use:   "reset <agent>",
	Short: "Start a fresh session for an agent",
	Args:  cobra.ExactArgs(1),
	Run:   runACPReset,
}

var acpInfoCmd = &cobra.Command{
	Use:   "info <agent>",
	Short: "Show negotiated capabilities of an agent",
	Args:  cobra.ExactArgs(1),
	Run:   runACPInfo,
}

var acpInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Write an example acp.json if none exists",
	Run:   runACPInit,
}

var acpConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Print the ACP config file path",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(resolvedConfigPath())
	},
}

func init() {
	rootCmd.AddCommand(acpCmd)

	acpCmd.PersistentFlags().StringVar(&acpConfigPath, "config", "", "Path to acp.json (default: $GO_MAGIC_ACP_CONFIG or <magic home>/acp.json)")
	acpCmd.PersistentFlags().BoolVar(&acpJSONOut, "json", false, "Print machine-readable JSON")

	acpPromptCmd.Flags().IntVar(&acpTimeoutSec, "timeout", 0, "Per-turn timeout in seconds (default: agent config, 600)")
	acpPromptCmd.Flags().BoolVar(&acpReset, "reset", false, "Start a fresh session before prompting")

	acpCmd.AddCommand(acpAgentsCmd, acpPromptCmd, acpResetCmd, acpInfoCmd, acpInitCmd, acpConfigCmd)
}

// resolvedConfigPath 返回 --config 指定的路径，否则按 magic home 解析。
func resolvedConfigPath() string {
	if acpConfigPath != "" {
		return acpConfigPath
	}
	return acp.DefaultConfigPath()
}

func newACPManager() *acp.Manager {
	return acp.NewManager(resolvedConfigPath())
}

func runACPAgents(cmd *cobra.Command, args []string) {
	mgr := newACPManager()
	defer mgr.Close()

	agents, err := mgr.Agents()
	if err != nil {
		failACP("read acp config: %v", err)
	}

	if acpJSONOut {
		printACPJSON(map[string]interface{}{
			"config": mgr.ConfigPath(),
			"agents": agents,
		})
		return
	}

	if len(agents) == 0 {
		fmt.Printf("No ACP agents configured (%s).\n", mgr.ConfigPath())
		fmt.Println("Run `magic acp init` to write an example config.")
		return
	}

	fmt.Printf("ACP agents (%d) — config: %s\n", len(agents), mgr.ConfigPath())
	for _, a := range agents {
		status := "enabled"
		if !a.Enabled {
			status = "disabled"
		}
		fmt.Printf("  %-16s [%s] %s\n", a.Name, status, a.Command)
		if a.Description != "" {
			fmt.Printf("  %-16s %s\n", "", a.Description)
		}
	}
}

func runACPPrompt(cmd *cobra.Command, args []string) {
	agent := args[0]
	prompt := strings.Join(args[1:], " ")
	if strings.TrimSpace(prompt) == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			failACP("read stdin: %v", err)
		}
		prompt = string(data)
	}
	if strings.TrimSpace(prompt) == "" {
		failACP("empty prompt")
	}

	mgr := newACPManager()
	defer mgr.Close()

	// Ctrl+C 走 ACP 自己的取消流程，而不是直接杀进程：
	// 规范要求 client 发 session/cancel 并等 agent 以 cancelled 收尾。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if acpTimeoutSec > 0 {
		mgr.SetTimeoutOverride(agent, time.Duration(acpTimeoutSec)*time.Second)
	}

	if acpReset {
		if err := mgr.ResetSession(ctx, agent); err != nil {
			failACP("reset session: %v", err)
		}
	}

	var printedTool string
	outcome, err := mgr.Prompt(ctx, agent, []acp.ContentBlock{acp.TextBlock(prompt)}, func(u acp.Update) {
		switch {
		case u.Kind == acp.UpdateAgentMessageChunk && u.Text != "":
			fmt.Print(u.Text)
		case u.ToolCall != nil:
			// 工具调用只报一次变化，避免同一条 call 的多次 update 刷屏。
			line := describeToolCall(u.ToolCall)
			if line != printedTool {
				fmt.Fprintf(os.Stderr, "\n· %s\n", line)
				printedTool = line
			}
		}
	})
	if err != nil {
		failACP("prompt failed: %v", err)
	}

	if acpJSONOut {
		printACPJSON(map[string]interface{}{
			"agent":       agent,
			"stop_reason": outcome.StopReason,
			"text":        outcome.Text,
			"thought":     outcome.Thought,
			"tool_calls":  outcome.ToolCalls,
			"cancelled":   outcome.Cancelled,
			"updates":     outcome.Updates,
		})
		return
	}

	if outcome.Text != "" {
		fmt.Println()
	}
	fmt.Fprintf(os.Stderr, "\n[stop: %s", outcome.StopReason)
	if len(outcome.ToolCalls) > 0 {
		fmt.Fprintf(os.Stderr, ", %d tool calls", len(outcome.ToolCalls))
	}
	fmt.Fprintln(os.Stderr, "]")

	if outcome.Cancelled {
		os.Exit(130)
	}
}

func runACPReset(cmd *cobra.Command, args []string) {
	mgr := newACPManager()
	defer mgr.Close()

	if err := mgr.ResetSession(context.Background(), args[0]); err != nil {
		failACP("reset session: %v", err)
	}
	if acpJSONOut {
		printACPJSON(map[string]bool{"reset": true})
		return
	}
	fmt.Printf("Session reset for agent '%s'.\n", args[0])
}

func runACPInfo(cmd *cobra.Command, args []string) {
	mgr := newACPManager()
	defer mgr.Close()

	ctx := context.Background()
	client, err := mgr.Client(ctx, args[0])
	if err != nil {
		failACP("connect: %v", err)
	}

	info := client.Info()
	if acpJSONOut {
		printACPJSON(map[string]interface{}{
			"agent":    args[0],
			"info":     info,
			"session":  client.SessionID(),
			"roots":    client.Roots(),
			"protocol": acp.ProtocolVersion,
		})
		return
	}

	name := args[0]
	version := "unknown"
	if info.AgentInfo != nil {
		if info.AgentInfo.Title != "" {
			name = info.AgentInfo.Title
		} else if info.AgentInfo.Name != "" {
			name = info.AgentInfo.Name
		}
		version = info.AgentInfo.Version
	}

	fmt.Printf("Agent:    %s (%s)\n", name, version)
	fmt.Printf("Protocol: v%d\n", info.ProtocolVersion)
	fmt.Printf("Session:  %s\n", client.SessionID())
	fmt.Printf("Roots:    %s\n", strings.Join(client.Roots(), ", "))
	caps := info.AgentCapabilities
	fmt.Printf("Capabilities: loadSession=%t image=%t audio=%t embeddedContext=%t mcp(http=%t sse=%t)\n",
		caps.LoadSession, caps.PromptCapabilities.Image, caps.PromptCapabilities.Audio,
		caps.PromptCapabilities.EmbeddedContext, caps.MCPCapabilities.HTTP, caps.MCPCapabilities.SSE)
	if len(info.AuthMethods) > 0 {
		methods := make([]string, 0, len(info.AuthMethods))
		for _, m := range info.AuthMethods {
			methods = append(methods, m.ID)
		}
		fmt.Printf("Auth:     %s\n", strings.Join(methods, ", "))
	}
}

func runACPInit(cmd *cobra.Command, args []string) {
	path := resolvedConfigPath()

	if _, err := os.Stat(path); err == nil {
		fmt.Printf("Config already exists: %s\n", path)
		return
	}

	example := &acp.Config{Agents: map[string]acp.AgentConfig{
		"claude": {
			Command:     "npx",
			Args:        []string{"-y", "@zed-industries/claude-code-acp"},
			Workspace:   defaultWorkspace(),
			Description: "Claude Code via ACP. Edit command/args to match the agent you use.",
			AutoApprove: []string{acp.ToolKindRead, acp.ToolKindSearch},
		},
	}}
	if err := acp.SaveConfig(path, example); err != nil {
		failACP("write %s: %v", path, err)
	}
	fmt.Printf("Wrote example config: %s\n", path)
	fmt.Println("Edit it (at minimum set a real agent command), then run `magic acp agents`.")
}

// defaultWorkspace 返回当前目录，作为示例配置的 workspace。
func defaultWorkspace() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

func describeToolCall(tc *acp.ToolCallInfo) string {
	label := tc.Title
	if label == "" {
		label = tc.Name
	}
	if label == "" {
		label = tc.ID
	}
	if tc.Status != "" {
		return fmt.Sprintf("%s [%s]", label, tc.Status)
	}
	return label
}

func printACPJSON(v interface{}) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		failACP("marshal output: %v", err)
	}
	fmt.Println(string(data))
}

func failACP(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
