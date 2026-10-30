package cliconn

import "strings"

// secretPrefixes 是"这个值几乎肯定是一枚明文凭据"的前缀表。
//
// 只收广泛认知的 token 形态，宁缺毋滥：误报的代价是用户多一次确认，
// 漏报的代价是明文悄悄落盘。前缀表之外不做熵值启发式——普通配置值
// （路径、镜像名、repo 名）被误判会打断装机流程。
var secretPrefixes = []string{
	// GitHub PAT（classic 与 fine-grained）
	"ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_",
	// OpenAI / Anthropic / 通用 SDK key
	"sk-", "sk-proj-", "sk-ant-",
	// AWS access key id
	"AKIA", "ASIA",
	// Slack
	"xoxb-", "xoxa-", "xoxp-", "xoxs-", "xoxr-",
	// Google
	"AIza", "ya29.",
	// GitLab / DigitalOcean / npm / PyPI / HuggingFace
	"glpat-", "dop_v1_", "npm_", "pypi-", "hf_",
	// Supabase / Vercel / Cloudflare API token
	"sbp_", "v1.", "cf_",
	// Stripe live keys
	"sk_live_", "rk_live_",
	// 常见 HTTP 认证头直接当值填
	"Bearer ",
}

// LooksSecretLike 报告一个待保存的 env 值是否像明文凭据。
//
// ${VAR} / ${VAR:-默认} 引用不算——那正是我们希望用户采用的形态，
// 磁盘上只有变量名。空值也不算。
func LooksSecretLike(value string) bool {
	v := strings.TrimSpace(value)
	if v == "" || strings.HasPrefix(v, "${") {
		return false
	}
	for _, prefix := range secretPrefixes {
		if strings.HasPrefix(v, prefix) {
			return true
		}
	}
	return false
}
