package agentplugin

import (
	"reflect"
	"testing"
)

// withCredentials 的核心约定:用户凭据覆盖 mcp.json 同名 env、其余保留、
// 且绝不改写原 spec(重连要以 mcp.json 为基准重新合并)。
func TestWithCredentials(t *testing.T) {
	base := MCPServerSpec{
		Type: TransportStdio,
		Env:  map[string]string{"DINGTALK_Client_ID": "", "DINGTALK_Client_Secret": "", "LOG_LEVEL": "debug"},
	}
	creds := map[string]string{
		"DINGTALK_Client_ID":     "app-123",
		"DINGTALK_Client_Secret": "sec-456",
		"EXTRA":                  "1", // mcp.json 里没声明的 key 也要带上
	}

	got := withCredentials(base, creds)

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
	if out := withCredentials(base, nil); !reflect.DeepEqual(out, base) {
		t.Errorf("nil credentials should return spec unchanged, got %+v", out)
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
