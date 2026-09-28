package mcp

import (
	"reflect"
	"strings"
	"testing"
)

// 外部生态的写法："粘贴一段 JSON 就能加服务器" 的事实标准形态。
func TestParseServersJSONMCPDesktopShape(t *testing.T) {
	data := []byte(`{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/data"],
      "env": {
        "TOKEN": "abc",
        "REGION": "cn-north"
      }
    }
  }
}`)

	got, err := ParseServersJSON(data)
	if err != nil {
		t.Fatalf("ParseServersJSON error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d servers, want 1", len(got))
	}
	if got[0].Name != "filesystem" {
		t.Errorf("name = %q, want filesystem", got[0].Name)
	}
	want := ServerConfig{
		Command:   "npx",
		Args:      []string{"-y", "@modelcontextprotocol/server-filesystem", "/data"},
		Env:       []string{"REGION=cn-north", "TOKEN=abc"}, // 对象形态按 key 排序，保证落盘稳定
		Transport: TransportStdio,                           // 没写 transport，按 command 推断
	}
	if !reflect.DeepEqual(got[0].Config, want) {
		t.Errorf("config = %+v, want %+v", got[0].Config, want)
	}
}

func TestParseServersJSONVariants(t *testing.T) {
	cases := []struct {
		name string
		json string
		want []NamedServerConfig
	}{
		{
			name: "本项目 config.json 的 servers 段",
			json: `{"servers": {"fs": {"command": "npx", "args": ["-y", "pkg"], "transport": "stdio"}}}`,
			want: []NamedServerConfig{{Name: "fs", Config: ServerConfig{Command: "npx", Args: []string{"-y", "pkg"}, Transport: TransportStdio}}},
		},
		{
			name: "从 config.json 剪下来的 mcp 片段",
			json: `{"mcp": {"servers": {"fs": {"command": "node", "transport": "stdio"}}}}`,
			want: []NamedServerConfig{{Name: "fs", Config: ServerConfig{Command: "node", Transport: TransportStdio}}},
		},
		{
			name: "裸的 name → 配置 map",
			json: `{"zeta": {"url": "http://localhost:3001/sse", "transport": "sse"}, "alpha": {"command": "python", "transport": "stdio"}}`,
			want: []NamedServerConfig{
				{Name: "alpha", Config: ServerConfig{Command: "python", Transport: TransportStdio}},
				{Name: "zeta", Config: ServerConfig{URL: "http://localhost:3001/sse", Transport: TransportSSE}},
			},
		},
		{
			name: "单个服务器自带 name",
			json: `{"name": "web", "url": "http://localhost:8080/mcp", "type": "http"}`,
			want: []NamedServerConfig{{Name: "web", Config: ServerConfig{URL: "http://localhost:8080/mcp", Transport: TransportSSE}}},
		},
		{
			name: "数组形态",
			json: `[{"name": "a", "command": "npx", "type": "stdio"}, {"name": "b", "command": "uvx", "type": "stdio"}]`,
			want: []NamedServerConfig{
				{Name: "a", Config: ServerConfig{Command: "npx", Transport: TransportStdio}},
				{Name: "b", Config: ServerConfig{Command: "uvx", Transport: TransportStdio}},
			},
		},
		{
			name: "command 写成数组 + args 写成一整串 + env 写成数组",
			json: `{"x": {"command": ["npx", "-y", "pkg"], "args": "-y @foo/bar --root \"/tmp/my dir\"", "env": ["KEY=value"], "transport": "stdio"}}`,
			want: []NamedServerConfig{{Name: "x", Config: ServerConfig{
				Command:   "npx",
				Args:      []string{"-y", "pkg", "-y", "@foo/bar", "--root", "/tmp/my dir"},
				Env:       []string{"KEY=value"},
				Transport: TransportStdio,
			}}},
		},
		{
			name: "baseUrl 别名 + streamable-http 归一为 sse",
			json: `{"remote": {"baseUrl": "https://example.com/mcp", "type": "streamable-http"}}`,
			want: []NamedServerConfig{{Name: "remote", Config: ServerConfig{URL: "https://example.com/mcp", Transport: TransportSSE}}},
		},
		{
			name: "disabled 条目被跳过",
			json: `{"mcpServers": {"off": {"command": "npx", "disabled": true}, "on": {"command": "npx"}}}`,
			want: []NamedServerConfig{{Name: "on", Config: ServerConfig{Command: "npx", Transport: TransportStdio}}},
		},
		{
			name: "jsonc: 注释与尾随逗号",
			json: "{\n  // 说明\n  \"servers\": {\n    \"fs\": {\"command\": \"npx\", /* 行内 */ \"transport\": \"stdio\",},\n  },\n}",
			want: []NamedServerConfig{{Name: "fs", Config: ServerConfig{Command: "npx", Transport: TransportStdio}}},
		},
		{
			name: "env 里的数字与布尔值",
			json: `{"x": {"command": "npx", "env": {"PORT": 8080, "DEBUG": true}}}`,
			want: []NamedServerConfig{{Name: "x", Config: ServerConfig{Command: "npx", Env: []string{"DEBUG=true", "PORT=8080"}, Transport: TransportStdio}}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseServersJSON([]byte(tc.json))
			if err != nil {
				t.Fatalf("ParseServersJSON error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got  %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestParseServersJSONErrors(t *testing.T) {
	cases := []struct {
		name    string
		json    string
		wantSub string
	}{
		{"空输入", "   ", "empty"},
		{"非法 JSON", `{"servers": `, "invalid JSON"},
		{"缺 name 的单对象", `{"command": "npx", "type": "stdio"}`, `server name is missing`},
		{"数组条目缺 name", `[{"command": "npx"}]`, `entry #1 has no "name"`},
		{"非法服务器名", `{"mcpServers": {"../evil": {"command": "npx"}}}`, "invalid server name"},
		{"重复服务器名", `[{"name": "a", "command": "npx"}, {"name": "a", "command": "uvx"}]`, "duplicate server name"},
		{"不支持的传输方式", `{"x": {"command": "npx", "type": "websocket"}}`, "unsupported transport"},
		{"stdio 缺 command", `{"x": {"transport": "stdio", "args": ["-y"]}}`, `stdio transport requires "command"`},
		{"sse 缺 url", `{"x": {"transport": "sse"}}`, `sse transport requires "url"`},
		{"没有可用条目", `{"mcpServers": {"off": {"command": "npx", "disabled": true}}}`, "all MCP servers in the JSON are disabled"},
		{"env 数组缺等号", `{"x": {"command": "npx", "env": ["NOPE"]}}`, `"KEY=value" form`},
		{"mcpServers 不是对象", `{"mcpServers": ["npx"]}`, "must be an object"},
		{"顶层是标量", `"npx"`, "expected a JSON object or an array"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseServersJSON([]byte(tc.json))
			if err == nil {
				t.Fatalf("expected an error containing %q, got nil", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error = %v, want it to contain %q", err, tc.wantSub)
			}
		})
	}
}

// ParseServerConfigJSON 用于 PUT /api/mcp/servers/{name}：body 可以不写名字
// （名字来自 URL），但多服务器请求必须被拒绝，否则用户会以为整段 JSON 都写进去了。
func TestParseServerConfigJSON(t *testing.T) {
	// 表单提交的形态：没有 name，用 URL 上的名字补齐。
	name, cfg, err := ParseServerConfigJSON([]byte(`{"command": "npx", "args": ["-y", "pkg"], "transport": "stdio"}`), "filesystem")
	if err != nil {
		t.Fatalf("ParseServerConfigJSON error: %v", err)
	}
	if name != "filesystem" || cfg.Command != "npx" || cfg.Transport != TransportStdio {
		t.Fatalf("got (%q, %+v)", name, cfg)
	}

	// body 里显式写了别的名字：原样返回，由调用方决定怎么处理。
	name, _, err = ParseServerConfigJSON([]byte(`{"name": "other", "command": "npx"}`), "filesystem")
	if err != nil {
		t.Fatalf("ParseServerConfigJSON error: %v", err)
	}
	if name != "other" {
		t.Fatalf("name = %q, want other", name)
	}

	// 容器形态且只有一个服务器：允许。
	name, _, err = ParseServerConfigJSON([]byte(`{"mcpServers": {"fs": {"command": "npx"}}}`), "filesystem")
	if err != nil || name != "fs" {
		t.Fatalf("container form: name = %q, err = %v", name, err)
	}

	_, _, err = ParseServerConfigJSON([]byte(`{"mcpServers": {"a": {"command": "npx"}, "b": {"command": "uvx"}}}`), "x")
	if err == nil || !strings.Contains(err.Error(), "expected exactly one MCP server, got 2") {
		t.Fatalf("error = %v, want a multi-server rejection", err)
	}

	// 既没名字也没给 fallback：必须报错而不是造一个匿名的服务器。
	if _, _, err := ParseServerConfigJSON([]byte(`{"command": "npx"}`), ""); err == nil {
		t.Fatal("expected a missing-name error")
	}
	// 非对象 body
	if _, _, err := ParseServerConfigJSON([]byte(`[1, 2]`), "x"); err == nil {
		t.Fatal("expected a non-object error")
	}
}

func TestNormalizeTransport(t *testing.T) {
	cases := map[string]string{
		"":                "",
		"stdio":           TransportStdio,
		"STDIO":           TransportStdio,
		"sse":             TransportSSE,
		"http":            TransportSSE,
		"streamable-http": TransportSSE,
		"streamable_http": TransportSSE,
		"streamableHTTP":  TransportSSE,
		"  sse  ":         TransportSSE,
	}
	for in, want := range cases {
		got, err := NormalizeTransport(in)
		if err != nil {
			t.Errorf("NormalizeTransport(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("NormalizeTransport(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := NormalizeTransport("websocket"); err == nil {
		t.Error("NormalizeTransport(\"websocket\") should fail")
	}
}

func TestSplitCommandLine(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"-y pkg /data", []string{"-y", "pkg", "/data"}},
		{`--root "/tmp/my dir"`, []string{"--root", "/tmp/my dir"}},
		{`--name 'a b'`, []string{"--name", "a b"}},
		{`--url "https://x/y?z=1 2"`, []string{"--url", "https://x/y?z=1 2"}},
		// 双引号里的反斜杠不参与转义，Windows 路径原样保留
		{`"C:\Program Files\node\node.exe" -v`, []string{`C:\Program Files\node\node.exe`, "-v"}},
		{`a\ b`, []string{"a b"}}, // 引号外的转义空格
	}

	for _, tc := range cases {
		got := SplitCommandLine(tc.in)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("SplitCommandLine(%q) = %#v, want %#v", tc.in, got, tc.want)
		}
	}
}

func TestEnvRoundTrip(t *testing.T) {
	m := map[string]string{"B": "2", "A": "1"}
	if got, want := EnvMapToSlice(m), []string{"A=1", "B=2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("EnvMapToSlice = %v, want %v", got, want)
	}
	if got := EnvMapToSlice(nil); got != nil {
		t.Errorf("EnvMapToSlice(nil) = %v, want nil", got)
	}
	if got, want := EnvSliceToMap([]string{"A=1", "B=2", "junk"}), m; !reflect.DeepEqual(got, want) {
		t.Errorf("EnvSliceToMap = %v, want %v", got, want)
	}
	if got := EnvSliceToMap([]string{"junk"}); got != nil {
		t.Errorf("EnvSliceToMap(no '=') = %v, want nil", got)
	}
	// 值里含 '=' 时只按第一个 '=' 切分
	if got := EnvSliceToMap([]string{"TOKEN=a=b"}); got["TOKEN"] != "a=b" {
		t.Errorf(`EnvSliceToMap("TOKEN=a=b") = %v`, got)
	}
}
