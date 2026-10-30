package policy

import (
	"strings"
	"testing"
)

func TestDisabledPolicyAllowsEverything(t *testing.T) {
	var nilPolicy *Policy
	if err := nilPolicy.CheckMCP("x", "npx", ""); err != nil {
		t.Fatalf("nil policy must allow: %v", err)
	}
	p := &Policy{} // enforce=false
	if err := p.CheckMCP("x", "anything", "https://evil.example.com/mcp"); err != nil {
		t.Fatalf("non-enforcing policy must allow: %v", err)
	}
	if err := p.CheckCLI("x", "rm"); err != nil {
		t.Fatalf("non-enforcing policy must allow CLI: %v", err)
	}
}

func TestEnforceWithEmptyListDeniesAll(t *testing.T) {
	p := &Policy{Enforce: true}
	// 空名单是"全禁"而不是"不限制"——这是这层最容易搞反的地方。
	if err := p.CheckMCP("x", "npx", ""); err == nil {
		t.Fatal("empty allowlist must deny")
	} else if !strings.Contains(err.Error(), "no command allowlist") {
		t.Fatalf("error should say why: %v", err)
	}
	if err := p.CheckCLI("x", "gh"); err == nil {
		t.Fatal("empty cli allowlist must deny")
	}
}

func TestMCPHostAllowlist(t *testing.T) {
	p := &Policy{Enforce: true, MCPAllowedHosts: []string{"*.example.com", "api.other.io"}}

	cases := []struct {
		url  string
		want bool
	}{
		{"https://mcp.example.com/v1", true},
		{"https://a.b.example.com/mcp", true},
		{"https://example.com/mcp", false}, // 通配符只覆盖子域，不含裸域
		{"https://api.other.io/sse", true},
		{"https://evil.io/mcp", false},
		{"https://example.com.evil.io/mcp", false},
	}
	for _, c := range cases {
		err := p.CheckMCP("srv", "", c.url)
		if c.want && err != nil {
			t.Errorf("%s should be allowed: %v", c.url, err)
		}
		if !c.want && err == nil {
			t.Errorf("%s should be denied", c.url)
		}
	}
}

func TestMCPURLWithoutHostIsDenied(t *testing.T) {
	p := &Policy{Enforce: true, MCPAllowedHosts: []string{"*"}}
	if err := p.CheckMCP("srv", "", "not-a-url"); err == nil {
		t.Fatal("unparseable url must be denied even with wildcard")
	}
}

func TestMCPCommandAllowlist(t *testing.T) {
	p := &Policy{Enforce: true, MCPAllowedCommands: []string{"npx", "/usr/local/bin/my-server"}}
	// 命令与 host 是两张名单：只放行命令时，远程 URL 一律被拒。
	if err := p.CheckMCP("srv", "/opt/homebrew/bin/npx", ""); err != nil {
		t.Fatalf("basename match should pass: %v", err)
	}
	if err := p.CheckMCP("srv", "my-server", ""); err != nil {
		t.Fatalf("full-path entry should match by basename: %v", err)
	}
	if err := p.CheckMCP("srv", "curl", ""); err == nil {
		t.Fatal("unlisted command must be denied")
	}
	if err := p.CheckMCP("srv", "", "https://mcp.example.com"); err == nil {
		t.Fatal("host allowlist is empty, remote url must be denied")
	}
}

func TestCLIAllowlist(t *testing.T) {
	p := &Policy{Enforce: true, CLIAllowedCommands: []string{"gh", "git"}}
	if err := p.CheckCLI("gh", "gh"); err != nil {
		t.Fatalf("gh should pass: %v", err)
	}
	if err := p.CheckCLI("sh", "bash"); err == nil {
		t.Fatal("bash must be denied")
	}
}

func TestWildcardAllowsEverything(t *testing.T) {
	p := &Policy{Enforce: true, MCPAllowedHosts: []string{"*"}, MCPAllowedCommands: []string{"*"}, CLIAllowedCommands: []string{"*"}}
	if err := p.CheckMCP("s", "anything", "https://any.host/x"); err != nil {
		t.Fatalf("* must allow hosts: %v", err)
	}
	if err := p.CheckCLI("s", "anything"); err != nil {
		t.Fatalf("* must allow commands: %v", err)
	}
}
