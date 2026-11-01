package server

import (
	"testing"

	"github.com/magicwubiao/go-magic/internal/mcp"
	"github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/policy"
)

// TestPluginMCPPolicyEnforced 回归：policy 早就把"装插件"写进威胁模型，
// 但插件 MCP 条目此前**一次都没走过** CheckMCP——enforce=true 时装一个
// 插件就能运行被禁命令、连被禁主机。
func TestPluginMCPPolicyEnforced(t *testing.T) {
	s := &Server{cfg: &config.Config{Policy: policy.Policy{
		Enforce:            true,
		MCPAllowedCommands: []string{"npx"},
		MCPAllowedHosts:    []string{"api.example.com"},
	}}}

	if err := s.pluginMCPPolicy("agentplugin:demo/srv", mcp.ServerConfig{Command: "npx"}); err != nil {
		t.Fatalf("allowed command rejected: %v", err)
	}
	if err := s.pluginMCPPolicy("agentplugin:demo/srv", mcp.ServerConfig{Command: "bash"}); err == nil {
		t.Fatal("blocked command was allowed through the plugin entry")
	}
	if err := s.pluginMCPPolicy("agentplugin:demo/srv", mcp.ServerConfig{URL: "https://api.example.com/mcp"}); err != nil {
		t.Fatalf("allowed host rejected: %v", err)
	}
	if err := s.pluginMCPPolicy("agentplugin:demo/srv", mcp.ServerConfig{URL: "https://evil.example.net/mcp"}); err == nil {
		t.Fatal("blocked host was allowed through the plugin entry")
	}
}

// TestPluginCLIPolicyEnforced 同一道锁要盖住 cli.json 里的 command：
// 插件是第三方内容，命令由作者决定。
func TestPluginCLIPolicyEnforced(t *testing.T) {
	s := &Server{cfg: &config.Config{Policy: policy.Policy{
		Enforce:            true,
		CLIAllowedCommands: []string{"gh"},
	}}}
	check := s.cliRegisterOptions().AllowCommand
	if check == nil {
		t.Fatal("CLI registration must carry an admission check")
	}
	if err := check("gh", "gh"); err != nil {
		t.Fatalf("allowed CLI command rejected: %v", err)
	}
	if err := check("bash", "bash"); err == nil {
		t.Fatal("blocked CLI command was allowed through the plugin entry")
	}
}

// TestPluginMCPPolicyNoEnforce 未开启 enforce 时一律放行（单机自用不该被挡）。
func TestPluginMCPPolicyNoEnforce(t *testing.T) {
	s := &Server{cfg: &config.Config{Policy: policy.Policy{}}}
	if err := s.pluginMCPPolicy("x", mcp.ServerConfig{Command: "anything"}); err != nil {
		t.Fatalf("policy should be a no-op when enforce=false: %v", err)
	}
}
