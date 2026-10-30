package agentplugin

import (
	"reflect"
	"testing"
)

// applyCredentials 的核心约定:用户凭据覆盖 mcp.json 同名 env、其余保留、
// 且绝不改写原 spec(重连要以 mcp.json 为基准重新合并)。
func TestApplyCredentials(t *testing.T) {
	base := MCPServerSpec{
		Type: TransportStdio,
		Env:  map[string]string{"DINGTALK_Client_ID": "", "DINGTALK_Client_Secret": "", "LOG_LEVEL": "debug"},
	}
	creds := map[string]string{
		"DINGTALK_Client_ID":     "app-123",
		"DINGTALK_Client_Secret": "sec-456",
		"EXTRA":                  "1", // mcp.json 里没声明的 key 也要带上
	}

	got, missing := applyCredentials(base, creds)
	if len(missing) != 0 {
		t.Fatalf("unexpected missing creds: %v", missing)
	}

	if got.Env["DINGTALK_Client_ID"] != "app-123" || got.Env["DINGTALK_Client_Secret"] != "sec-456" {
		t.Errorf("credentials not applied: %v", got.Env)
	}
	if got.Env["LOG_LEVEL"] != "debug" {
		t.Errorf("non-credential env lost: %v", got.Env)
	}
	if got.Env["EXTRA"] != "1" {
		t.Errorf("extra key dropped: %v", got.Env)
	}
	// 原 spec 不被改写:再次以空凭据展开应回到占位状态。
	if base.Env["DINGTALK_Client_ID"] != "" {
		t.Errorf("original spec mutated: %v", base.Env)
	}

	// 空凭据:原样返回。
	out, missing := applyCredentials(base, nil)
	if len(missing) != 0 {
		t.Errorf("missing should be empty, got %v", missing)
	}
	if !reflect.DeepEqual(out.Env, base.Env) {
		t.Errorf("nil credentials should keep env, got %+v", out.Env)
	}
}

// ${CRED:NAME} 是 args/headers 里的凭据引用(目录条目物化成插件包后才需要)。
func TestApplyCredentialsCredRefs(t *testing.T) {
	base := MCPServerSpec{
		Type:    TransportStdio,
		Command: "npx",
		Args:    []string{"-y", "lark-mcp", "-a", "${CRED:LARK_APP_ID}", "-s", "${CRED:LARK_APP_SECRET}"},
		Headers: map[string]string{"Authorization": "Bearer ${CRED:TOKEN}"},
	}

	// 缺凭据:原样保留占位，并报告缺失的 key（排序去重）。
	_, missing := applyCredentials(base, map[string]string{"TOKEN": "t"})
	want := []string{"LARK_APP_ID", "LARK_APP_SECRET"}
	if !reflect.DeepEqual(missing, want) {
		t.Fatalf("missing = %v, want %v", missing, want)
	}

	got, missing := applyCredentials(base, map[string]string{
		"LARK_APP_ID":     "app",
		"LARK_APP_SECRET": "sec",
		"TOKEN":           "tok",
	})
	if len(missing) != 0 {
		t.Fatalf("unexpected missing: %v", missing)
	}
	wantArgs := []string{"-y", "lark-mcp", "-a", "app", "-s", "sec"}
	if !reflect.DeepEqual(got.Args, wantArgs) {
		t.Errorf("args = %v, want %v", got.Args, wantArgs)
	}
	if got.Headers["Authorization"] != "Bearer tok" {
		t.Errorf("headers not resolved: %v", got.Headers)
	}
	// 共享切片必须被复制，否则会把凭据写回 mcp.json 的原始 spec。
	if base.Args[3] != "${CRED:LARK_APP_ID}" {
		t.Errorf("original args mutated: %v", base.Args)
	}
}

// SpecCredentialKeys 汇总三处凭据位置:env 空值、args 与 headers 里的引用。
func TestSpecCredentialKeys(t *testing.T) {
	spec := MCPServerSpec{
		Args: []string{"--url=${CRED:SITE_URL}"},
		Env:  map[string]string{"APP_TOKEN": "", "LOG": "debug"},
		Headers: map[string]string{
			"Authorization": "Bearer ${CRED:HEADER_TOKEN}",
		},
	}
	got := SpecCredentialKeys(spec)
	want := []string{"APP_TOKEN", "HEADER_TOKEN", "SITE_URL"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SpecCredentialKeys = %v, want %v", got, want)
	}
	if out := SpecCredentialKeys(MCPServerSpec{Env: map[string]string{"K": "v"}}); len(out) != 0 {
		t.Errorf("no placeholders expected, got %v", out)
	}
}

// emptyEnvKeys 只挑值为空的 key(mcp.json 凭据占位约定),且排序稳定。
func TestEmptyEnvKeys(t *testing.T) {
	env := map[string]string{
		"B_TOKEN": "",      // 占位,应入选
		"A_TOKEN": "",      // 占位,应入选且排在 B 前
		"LOG":     "debug", // 有值,不入选
		"SPACE":   "   ",   // 纯空白视同空,应入选
	}
	got := emptyEnvKeys(env)
	want := []string{"A_TOKEN", "B_TOKEN", "SPACE"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("emptyEnvKeys = %v, want %v", got, want)
	}
	if out := emptyEnvKeys(map[string]string{"K": "v"}); len(out) != 0 {
		t.Errorf("no placeholders expected, got %v", out)
	}
}
