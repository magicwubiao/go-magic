package cliconn

import (
	"sort"
	"strings"
)

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

// MaskPlaceholder 是脱敏后替换成的占位串。
//
// 与 server 侧回显 env / client_secret 用的 "***" 保持同一个记号：
// 用户在不同界面看到的是同一种"这里本来有值"的表示。
const MaskPlaceholder = "***"

// minMaskLen 是参与脱敏的最短值长度。
//
// 太短的值（REIGON=cn、布尔开关）替换掉只会把正常命令显示成一串 ***，
// 而真正的凭据没有短于 4 位的。
const minMaskLen = 4

// sensitiveKeyWords 是环境变量名里出现就按凭据处理的分词表。
//
// 只认"完整的一段"（按非字母数字切分后逐段比较），所以 MONKEY 不会因为
// 含 KEY 被误判，S3_KEY / API_KEY / DB_PASSWORD 会命中。
var sensitiveKeyWords = map[string]bool{
	"TOKEN":       true,
	"TOKENS":      true,
	"SECRET":      true,
	"SECRETS":     true,
	"PASSWORD":    true,
	"PASSWD":      true,
	"PASSPHRASE":  true,
	"PWD":         true,
	"APIKEY":      true,
	"ACCESSKEY":   true,
	"SECRETKEY":   true,
	"PRIVATEKEY":  true,
	"CREDENTIAL":  true,
	"CREDENTIALS": true,
	"AUTH":        true,
	"BEARER":      true,
	"PAT":         true,
	"SIGNATURE":   true,
	"KEY":         true,
}

// isSensitiveEnvKey 判断变量名本身是否表示"这是凭据"。
func isSensitiveEnvKey(key string) bool {
	key = strings.ToUpper(strings.TrimSpace(key))
	if key == "" {
		return false
	}
	words := strings.FieldsFunc(key, func(r rune) bool {
		return !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9')
	})
	for _, w := range words {
		if sensitiveKeyWords[w] {
			return true
		}
	}
	return false
}

// SecretEnvValues 返回一组 env 里"不该回显"的值（按长度降序）。
//
// 判据是"变量名像凭据"或"值形似凭据"——两者取或：GH_TOKEN 的值可能是任意
// 形态的字符串，而 FOO 的值可能是一枚 sk- 开头的 key，两种情况都不能漏。
// extra 用于把调用方已经拿在手里的凭据（如 OAuth 的 access token）一起登记，
// 它不一定在 env 里以可识别的键名出现。
//
// 注意这是**展示层**脱敏，不是访问控制：命中与否只影响回显，值照常传给子进程。
func SecretEnvValues(env map[string]string, extra ...string) []string {
	seen := make(map[string]bool, len(env)+len(extra))
	out := make([]string, 0, len(env)+len(extra))
	add := func(v string) {
		v = strings.TrimSpace(v)
		if len(v) < minMaskLen || seen[v] {
			return
		}
		seen[v] = true
		out = append(out, v)
	}
	for k, v := range env {
		if isSensitiveEnvKey(k) || LooksSecretLike(v) {
			add(v)
		}
	}
	for _, v := range extra {
		add(v)
	}
	// 长的先替换：否则 s3cr3t-abcdef 会先被它的前缀 s3cr3t 替换掉，
	// 剩下的 "-abcdef" 留在命令里，等于泄露了一半。
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

// MaskSecrets 把一段文本里的凭据值替换成 ***。
//
// secrets 需按长度降序排列（SecretEnvValues 已保证），否则一个值的短前缀
// 会先被替换掉、剩下的半截留在原文里。
func MaskSecrets(s string, secrets []string) string {
	if s == "" || len(secrets) == 0 {
		return s
	}
	for _, sec := range secrets {
		if sec == "" {
			continue
		}
		s = strings.ReplaceAll(s, sec, MaskPlaceholder)
	}
	return s
}

// MaskArgv 返回脱敏后的 argv 副本（入参不被修改）。
func MaskArgv(argv []string, secrets []string) []string {
	if len(argv) == 0 || len(secrets) == 0 {
		return argv
	}
	out := make([]string, len(argv))
	for i, a := range argv {
		out[i] = MaskSecrets(a, secrets)
	}
	return out
}
