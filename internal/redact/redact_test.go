package redact

import "strings"

import "testing"

// TestRedactRemovesSecrets 锁住底线：命中后原文中的秘密不得残留。
// 历史 bug：带捕获组的模式会把组 1（秘密本身）当作「保留前缀」输出，
// 导致脱敏后密钥依然完整出现在文本里。
func TestRedactRemovesSecrets(t *testing.T) {
	secrets := []string{
		"sk-abcdefghijklmnopqrstuvwxyz012345",
		"sk-ant-api03-abcdefghijklmnopqrstuvwxyz",
		"AIzaSyABCDEFGHIJKLMNOPQRSTUVWXYZ0123456",
		"gho_0123456789abcdefghijklmnopqrstuvwxyz",
		"ghp_0123456789abcdefghijklmnopqrstuvwxyz",
		"xai-abcdefghijklmnopqrstuvwxyz012345",
		"gsk_abcdefghijklmnopqrstuvwxyz012345",
		"AKIAIOSFODNN7EXAMPLE",
		"xoxb-123456789012-abcdefghijkl",
	}
	for _, secret := range secrets {
		in := "the token is " + secret + " keep it safe"
		out := Redact(in)
		if strings.Contains(out, secret) {
			t.Errorf("secret leaked after redact: %q -> %q", secret, out)
		}
		if !strings.Contains(out, "***REDACTED***") {
			t.Errorf("no redaction marker for %q -> %q", secret, out)
		}
	}
}

// TestRedactKeepsUsefulPrefix 前缀型模式应保留可读前缀，便于排查。
func TestRedactKeepsUsefulPrefix(t *testing.T) {
	out := Redact("Authorization: Bearer abcdefghijklmnopqrstuvwxyz123456")
	if !strings.Contains(out, "Bearer ") {
		t.Errorf("bearer prefix should be kept, got %q", out)
	}
	if strings.Contains(out, "abcdefghijklmnopqrstuvwxyz123456") {
		t.Errorf("bearer token leaked: %q", out)
	}

	out = Redact("postgres://admin:supersecret@db.local:5432/app")
	if strings.Contains(out, "supersecret") {
		t.Errorf("db password leaked: %q", out)
	}
}
