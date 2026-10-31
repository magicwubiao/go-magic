package cliconn

import (
	"strings"
	"testing"
)

func TestLooksSecretLike(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  bool
	}{
		{"github classic pat", "ghp_0123456789abcdefghijklmnopqrstuvwxyz", true},
		{"github fine-grained pat", "github_pat_11AAAAAAA0bbbbbbbbbbbb", true},
		{"openai key", "sk-proj-abc123def456", true},
		{"aws access key", "AKIAIOSFODNN7EXAMPLE", true},
		{"slack bot token", "xoxb-123456789-abcdef", true},
		{"google api key", "AIzaSyA-1234567890abcdefghijklmnopqrstu", true},
		{"gitlab pat", "glpat-abc123", true},
		{"huggingface token", "hf_abcdefghijklmnopqrstuvwxyz", true},
		{"bearer header as value", "Bearer eyJhbGciOi", true},
		{"env reference gh", "${GH_TOKEN}", false},
		{"env reference with default", "${GH_TOKEN:-none}", false},
		{"plain repo name", "owner/name", false},
		{"image tag", "node:20-alpine", false},
		{"empty", "", false},
		{"whitespace only", "   ", false},
		{"short sk false positive", "skip", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := LooksSecretLike(tc.value); got != tc.want {
				t.Fatalf("LooksSecretLike(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

// TestSecretEnvValues 判据是"变量名像凭据"或"值形似凭据"，两者取或。
func TestSecretEnvValues(t *testing.T) {
	env := map[string]string{
		"GH_TOKEN":    "ghp_0123456789abcdefghijklmnop", // 名字命中
		"api_key":     "abc123def456",                   // 下划线分词后命中 KEY
		"DB_PASSWORD": "hunter2hunter2",                 // 名字命中
		"FOO":         "sk-proj-abcdef123456",           // 名字不命中，值形似
		"REGION":      "cn-hangzhou",                    // 都不是：保持可见
		"SHORT_TOKEN": "abc",                            // 太短：不参与（避免把命令显示成一串 ***）
		"MONKEY":      "banana",                         // 含 KEY 但不是独立词
		"WEBHOOK_URL": "https://example.com/robot?k=1",  // 名字与值都不像凭据
	}
	got := SecretEnvValues(env)
	masked := strings.Join(got, ",")
	for _, want := range []string{"ghp_0123456789abcdefghijklmnop", "abc123def456", "hunter2hunter2", "sk-proj-abcdef123456"} {
		if !strings.Contains(masked, want) {
			t.Errorf("secret %q missing from %q", want, masked)
		}
	}
	for _, unwanted := range []string{"cn-hangzhou", "banana", "robot?k=1", "abc,"} {
		if strings.Contains(masked, unwanted) {
			t.Errorf("%q should not be treated as a secret (got %q)", unwanted, masked)
		}
	}

	// 长值排在短值之前：否则前缀先被替换，剩下的半截会留在命令里。
	prefixed := SecretEnvValues(map[string]string{"A_TOKEN": "s3cr3t-abcdef"})
	if len(prefixed) != 1 || prefixed[0] != "s3cr3t-abcdef" {
		t.Fatalf("expected the full value, got %v", prefixed)
	}

	// extra：OAuth token 不一定以可识别的键名出现在 env 里，调用方要能强制登记。
	withExtra := SecretEnvValues(nil, "opaque-oauth-token")
	if len(withExtra) != 1 || withExtra[0] != "opaque-oauth-token" {
		t.Fatalf("extra value not registered: %v", withExtra)
	}
	// 去重与长度过滤对 extra 同样生效。
	if got := SecretEnvValues(nil, "abc"); len(got) != 0 {
		t.Fatalf("too-short extra should be ignored, got %v", got)
	}
}

// TestMaskArgv 脱敏只影响回显，不改入参。
func TestMaskArgv(t *testing.T) {
	secrets := []string{"s3cr3t-abcdef", "ghp_0123456789abcdefghijklmnop"}
	in := []string{"gh", "pr", "list", "--token", "ghp_0123456789abcdefghijklmnop", "--x=s3cr3t-abcdef"}
	out := MaskArgv(in, secrets)

	want := []string{"gh", "pr", "list", "--token", MaskPlaceholder, "--x=" + MaskPlaceholder}
	for i := range want {
		if out[i] != want[i] {
			t.Fatalf("MaskArgv[%d] = %q, want %q", i, out[i], want[i])
		}
	}
	if in[4] != "ghp_0123456789abcdefghijklmnop" {
		t.Fatal("MaskArgv must not mutate its input")
	}
	// 没有凭据时原样返回（同一个切片，不做无谓分配）。
	if got := MaskArgv(in, nil); &got[0] != &in[0] {
		t.Fatal("MaskArgv with no secrets should return the argument as-is")
	}
}

func TestLooksLikeAuthFailure(t *testing.T) {
	positive := []string{
		"fatal: Authentication failed for 'https://github.com/o/r.git/'",
		"gh: To get started with GitHub CLI, please run: gh auth login",
		"error: You must be logged in to the server (Unauthorized)",
		"HTTP 401 Unauthorized",
		"Error: Bad credentials",
		"Your token has expired. Please authenticate again.",
		"An error occurred (ExpiredToken) when calling the ListBuckets operation",
		"An error occurred (InvalidClientTokenId) when calling the GetCallerIdentity operation",
		"oauth2: cannot fetch token: 400 Bad Request\nResponse: {\"error\":\"invalid_client\"}",
		"error: not logged in",
		"Unable to authenticate. Please log in via your browser.",
	}
	for _, s := range positive {
		if !LooksLikeAuthFailure(s) {
			t.Errorf("LooksLikeAuthFailure(%q) = false, want true", s)
		}
	}

	negative := []string{
		"",
		"main.go:401:5: undefined: foo", // 行号里的 401 不是 HTTP 状态
		"fatal: pathspec 'auth' did not match any files", // git 用法错误
		"error: failed to push some refs",                // 权限/远端拒绝，不是登录
		"command not found",
		"pull access denied for foo, repository does not exist", // docker 拉取失败常见形态
		"error: could not read config file: Permission denied",  // 文件权限，不是凭据
	}
	for _, s := range negative {
		if LooksLikeAuthFailure(s) {
			t.Errorf("LooksLikeAuthFailure(%q) = true, want false", s)
		}
	}
}
