package main

import (
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/mcp"
)

// resetMCPAddFlags 还原 mcp add 的全局旗标，避免用例之间互相污染。
func resetMCPAddFlags(t *testing.T) {
	t.Helper()
	prev := []interface{}{mcpAddJSON, mcpAddFile, mcpAddCommand, mcpAddArgs, mcpAddEnv, mcpAddTransport, mcpAddURL}
	t.Cleanup(func() {
		mcpAddJSON = prev[0].(string)
		mcpAddFile = prev[1].(string)
		mcpAddCommand = prev[2].(string)
		mcpAddArgs = prev[3].([]string)
		mcpAddEnv = prev[4].([]string)
		mcpAddTransport = prev[5].(string)
		mcpAddURL = prev[6].(string)
	})
	mcpAddJSON, mcpAddFile, mcpAddCommand, mcpAddEnv, mcpAddTransport, mcpAddURL = "", "", "", nil, "", ""
	mcpAddArgs = nil
}

// 旗标写法与 JSON 写法必须收敛到同一份解析结果（传输方式推断/参数切分/环境变量）。
func TestCollectMCPAddServersFromFlags(t *testing.T) {
	resetMCPAddFlags(t)
	mcpAddCommand = "npx"
	mcpAddArgs = []string{"-y @modelcontextprotocol/server-filesystem", "/tmp"} // 整串 + 单参数混用
	mcpAddEnv = []string{"TOKEN=abc"}

	got, err := collectMCPAddServers([]string{"filesystem"})
	if err != nil {
		t.Fatalf("collectMCPAddServers error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "filesystem" {
		t.Fatalf("got %+v, want one server named filesystem", got)
	}
	want := mcp.ServerConfig{
		Command:   "npx",
		Args:      []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"},
		Env:       []string{"TOKEN=abc"},
		Transport: mcp.TransportStdio,
	}
	if got[0].Config.Command != want.Command ||
		len(got[0].Config.Args) != len(want.Args) ||
		got[0].Config.Transport != want.Transport {
		t.Fatalf("config = %+v, want %+v", got[0].Config, want)
	}
}

func TestCollectMCPAddServersFromJSON(t *testing.T) {
	resetMCPAddFlags(t)
	mcpAddJSON = `{"mcpServers": {
		"fs": {"command": "npx", "args": ["-y", "pkg"]},
		"web": {"type": "http", "url": "http://localhost:9/mcp"}
	}}`

	got, err := collectMCPAddServers(nil)
	if err != nil {
		t.Fatalf("collectMCPAddServers error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d servers, want 2: %+v", len(got), got)
	}
	if got[0].Name != "fs" || got[0].Config.Transport != mcp.TransportStdio {
		t.Errorf("fs = %+v", got[0])
	}
	// http 传输归一为 sse
	if got[1].Name != "web" || got[1].Config.Transport != mcp.TransportSSE {
		t.Errorf("web = %+v", got[1])
	}

	// 位置参数只能给单个服务器的 JSON 改名；多服务器时是歧义的，必须报错。
	if _, err := collectMCPAddServers([]string{"renamed"}); err == nil {
		t.Error("a name argument with a multi-server JSON should be rejected")
	}

	resetMCPAddFlags(t)
	mcpAddJSON = `{"mcpServers": {"fs": {"command": "npx"}}}`
	got, err = collectMCPAddServers([]string{"renamed"})
	if err != nil {
		t.Fatalf("collectMCPAddServers error: %v", err)
	}
	if got[0].Name != "renamed" {
		t.Errorf("name = %q, want the positional override", got[0].Name)
	}
}

func TestCollectMCPAddServersErrors(t *testing.T) {
	resetMCPAddFlags(t)

	if _, err := collectMCPAddServers(nil); err == nil || !strings.Contains(err.Error(), "server name is required") {
		t.Errorf("no name and no JSON: err = %v", err)
	}

	resetMCPAddFlags(t)
	mcpAddJSON = `{"mcpServers": {`
	if _, err := collectMCPAddServers(nil); err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("bad JSON: err = %v", err)
	}

	// 既没给 command 也没给 url：由解析器给出可行动的错误，而不是静默加一个空服务器
	resetMCPAddFlags(t)
	if _, err := collectMCPAddServers([]string{"empty"}); err == nil {
		t.Error("a server with neither command nor url should be rejected")
	}
}
