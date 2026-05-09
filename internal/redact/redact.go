package redact

import (
	"regexp"
)

// 预编译的正则列表
var patterns = []struct {
	re   *regexp.Regexp
	name string
}{
	// OpenAI / Anthropic / Google API keys
	{regexp.MustCompile(`(?i)(sk-[a-zA-Z0-9]{20,})`), "openai_key"},
	{regexp.MustCompile(`(?i)(sk-ant-api[a-zA-Z0-9_-]{20,})`), "anthropic_key"},
	{regexp.MustCompile(`(?i)(AIza[a-zA-Z0-9_-]{35})`), "google_key"},
	// Generic Bearer tokens
	{regexp.MustCompile(`(?i)(Bearer\s+)[a-zA-Z0-9._-]{20,}`), "bearer_token"},
	// Generic API keys in headers/config
	{regexp.MustCompile(`(?i)(api[_-]?key\s*[:=]\s*["']?)[a-zA-Z0-9._-]{16,}`), "api_key"},
	// xAI / Groq / Mistral keys
	{regexp.MustCompile(`(?i)(xai-[a-zA-Z0-9]{20,})`), "xai_key"},
	{regexp.MustCompile(`(?i)(gsk_[a-zA-Z0-9]{20,})`), "groq_key"},
	// AWS keys
	{regexp.MustCompile(`(?i)(AKIA[A-Z0-9]{16})`), "aws_key"},
	{regexp.MustCompile(`(?i)(aws[_-]?secret[_-]?access[_-]?key\s*[:=]\s*["']?)[a-zA-Z0-9/+=]{40}`), "aws_secret"},
	// Private keys (PEM)
	{regexp.MustCompile(`-----BEGIN (?:RSA |EC |DSA )?PRIVATE KEY-----[\s\S]*?-----END (?:RSA |EC |DSA )?PRIVATE KEY-----`), "private_key"},
	// GitHub tokens
	{regexp.MustCompile(`(?i)(gh[ps]_[a-zA-Z0-9]{36})`), "github_token"},
	// Slack tokens
	{regexp.MustCompile(`(?i)(xox[bpors]-[a-zA-Z0-9-]{10,})`), "slack_token"},
	// Discord tokens
	{regexp.MustCompile(`(?i)([MN][a-zA-Z\d]{23,}\.[\w-]{6}\.[\w-]{27})`), "discord_token"},
	// Database URLs with passwords
	{regexp.MustCompile(`(?i)(postgres(?:ql)?|mysql|mongodb|redis)://[^:]+:([^@]+)@`), "db_password"},
	// Generic env vars that look like secrets
	{regexp.MustCompile(`(?i)((?:password|passwd|secret|token|credential|auth_key)\s*[:=]\s*["']?)[^\s"']{8,}`), "generic_secret"},
}

// Redact replaces sensitive patterns in text
func Redact(text string) string {
	for _, p := range patterns {
		text = p.re.ReplaceAllStringFunc(text, func(match string) string {
			// For patterns with capture groups (like "Bearer xxx"), keep prefix
			groups := p.re.FindStringSubmatch(match)
			if len(groups) > 1 {
				// Keep the prefix group, redact the rest
				return groups[1] + "***REDACTED***"
			}
			return "***REDACTED***"
		})
	}
	return text
}

// RedactIfEnabled checks config and redacts
func RedactIfEnabled(text string, enabled bool) string {
	if !enabled {
		return text
	}
	return Redact(text)
}
