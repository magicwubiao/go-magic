package cliconn

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// stubTokenSource 是执行期注入 token 的最小实现。
type stubTokenSource struct {
	tok string
	err error
}

func (s stubTokenSource) EnsureAccessToken() (string, error) { return s.tok, s.err }

func TestOAuthSpecValidate(t *testing.T) {
	valid := &OAuthSpec{
		DeviceAuthorizationEndpoint: "https://example.com/device",
		TokenEndpoint:               "https://example.com/token",
		ClientID:                    "cid",
		TokenEnv:                    "GH_TOKEN",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if !valid.Configured() {
		t.Fatal("Configured() = false for a complete spec")
	}

	cases := []struct {
		name string
		spec OAuthSpec
		want string
	}{
		{"no client id", OAuthSpec{DeviceAuthorizationEndpoint: "https://e.com/d", TokenEndpoint: "https://e.com/t", TokenEnv: "T"}, "client_id"},
		{"no token env", OAuthSpec{DeviceAuthorizationEndpoint: "https://e.com/d", TokenEndpoint: "https://e.com/t", ClientID: "c"}, "token_env"},
		{"bad token env", OAuthSpec{DeviceAuthorizationEndpoint: "https://e.com/d", TokenEndpoint: "https://e.com/t", ClientID: "c", TokenEnv: "1BAD"}, "token_env"},
		{"plain http remote", OAuthSpec{DeviceAuthorizationEndpoint: "http://e.com/d", TokenEndpoint: "https://e.com/t", ClientID: "c", TokenEnv: "T"}, "https"},
	}
	for _, tc := range cases {
		err := tc.spec.Validate()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err = %v, want mention of %q", tc.name, err, tc.want)
		}
	}

	// 本机调试地址允许 http：否则自建授权服务器没法接。
	local := &OAuthSpec{DeviceAuthorizationEndpoint: "http://127.0.0.1:9000/device", TokenEndpoint: "http://localhost:9000/token", ClientID: "c", TokenEnv: "T"}
	if err := local.Validate(); err != nil {
		t.Fatalf("loopback http rejected: %v", err)
	}
}

func TestConnectorValidateRejectsBrokenOAuth(t *testing.T) {
	conn := Connector{
		Command: "demo",
		OAuth:   &OAuthSpec{TokenEnv: "T"}, // 端点与 client_id 全缺
		Tools:   []ToolSpec{{Name: "x", Description: "d", Args: []string{"x"}}},
	}
	if err := conn.Validate(); err == nil {
		t.Fatal("expected validation error for incomplete oauth block")
	}
}

// TestRunInjectsOAuthToken 验证执行期把 token 合进子进程环境：
// CLI 只认环境变量，注入失败等于每次都 401。
func TestRunInjectsOAuthToken(t *testing.T) {
	conn := helperConnector("echo_env")
	conn.Env = append(conn.Env, "CLICONN_HELPER_ENV=OAUTH_TOKEN")
	conn.OAuth = &OAuthSpec{TokenEnv: "OAUTH_TOKEN", Tokens: stubTokenSource{tok: "tok-abc"}}

	res, err := Run(context.Background(), conn, ToolSpec{Name: "x", Description: "d", Args: []string{"run"}}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(res.Stdout) != "tok-abc" {
		t.Fatalf("stdout = %q, want the injected token", res.Stdout)
	}
}

// TestRunOAuthTokenOverridesEnv：配置里残留的旧 token 必须被覆盖，
// 否则用户会拿着过期凭据反复 401 却找不到原因。
func TestRunOAuthTokenOverridesEnv(t *testing.T) {
	conn := helperConnector("echo_env")
	conn.Env = append(conn.Env, "CLICONN_HELPER_ENV=OAUTH_TOKEN", "OAUTH_TOKEN=stale")
	conn.OAuth = &OAuthSpec{TokenEnv: "OAUTH_TOKEN", Tokens: stubTokenSource{tok: "fresh"}}

	res, err := Run(context.Background(), conn, ToolSpec{Name: "x", Description: "d", Args: []string{"run"}}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(res.Stdout) != "fresh" {
		t.Fatalf("stdout = %q, want the fresh token", res.Stdout)
	}
}

// TestRunOAuthTokenVisibleToArgTemplates：{{env:NAME}} 与子进程环境取同一份值，
// 否则把 token 放进参数位的连接器（Bearer 头之类）会拿到空串。
func TestRunOAuthTokenVisibleToArgTemplates(t *testing.T) {
	conn := helperConnector("echo_args")
	conn.Env = []string{"OAUTH_TOKEN=", "CLICONN_HELPER=1", "CLICONN_HELPER_MODE=echo_args"}
	conn.OAuth = &OAuthSpec{TokenEnv: "OAUTH_TOKEN", Tokens: stubTokenSource{tok: "tok-xyz"}}

	spec := ToolSpec{Name: "x", Description: "d", Args: []string{"--token={{env:OAUTH_TOKEN}}"}}
	res, err := Run(context.Background(), conn, spec, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.Stdout, "--token=tok-xyz") {
		t.Fatalf("stdout = %q, want the expanded token arg", res.Stdout)
	}
}

func TestRunOAuthTokenErrorIsReported(t *testing.T) {
	conn := helperConnector("echo_env")
	conn.OAuth = &OAuthSpec{TokenEnv: "OAUTH_TOKEN", Tokens: stubTokenSource{err: errors.New("not authorized")}}

	_, err := Run(context.Background(), conn, ToolSpec{Name: "x", Description: "d", Args: []string{"run"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "not authorized") {
		t.Fatalf("err = %v, want the token source error", err)
	}
}

func TestRunOAuthWithoutTokenSource(t *testing.T) {
	conn := helperConnector("echo_env")
	conn.OAuth = &OAuthSpec{TokenEnv: "OAUTH_TOKEN"}

	if _, err := Run(context.Background(), conn, ToolSpec{Name: "x", Description: "d", Args: []string{"run"}}, nil); err == nil {
		t.Fatal("expected an error when oauth is declared but no token store is wired")
	}
}
