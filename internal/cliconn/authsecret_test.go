package cliconn

import "testing"

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
