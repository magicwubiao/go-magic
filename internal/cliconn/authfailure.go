package cliconn

import "regexp"

// authFailurePatterns 匹配"这大概率是认证问题"的子进程 stderr。
//
// 只扫 stderr：CLI 的认证报错（git/gh/docker/aws）几乎都走 stderr，
// 而 stdout 里出现同形文本的概率低得多——例如编译错误的行号可能带 401。
//
// 误报的代价很小：只是在失败结果里多一个 auth_required 提示，模型多建议
// 一次"重新登录"而不是瞎猜重试。所以模式可以适当放宽，不用追求精确。
var authFailurePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bauthentication (is )?(required|failed)\b`),
	regexp.MustCompile(`(?i)\bnot (logged in|authenticated)\b`),
	regexp.MustCompile(`(?i)\bplease (log ?in|authenticate)\b`),
	regexp.MustCompile(`(?i)\bmust (be )?(logged in|authenticated)\b`),
	regexp.MustCompile(`(?i)\bunauthorized\b`),
	regexp.MustCompile(`(?i)\bunauthenticated\b`),
	regexp.MustCompile(`(?i)\bbad credentials\b`),
	regexp.MustCompile(`(?i)\b(invalid|missing|no|expired) (credentials|token)\b`),
	regexp.MustCompile(`(?i)\btoken (has )?expired\b`),
	regexp.MustCompile(`(?i)\bunable to authenticate\b`),
	regexp.MustCompile(`(?i)\bfailed to authenticate\b`),
	// AWS 错误码
	regexp.MustCompile(`(?i)\b(invalidclienttokenid|expiredtoken)\b`),
	// OAuth 错误码
	regexp.MustCompile(`(?i)\binvalid_client\b`),
	// HTTP 状态（带上下文，避免匹配行号/版本号里的 401）
	regexp.MustCompile(`(?i)\b401 unauthorized\b`),
	regexp.MustCompile(`(?i)\bhttp 401\b`),
	regexp.MustCompile(`(?i)\bstatus (code:? )?401\b`),
	// CLI 自己的登录引导（gh: "please run: gh auth login"）
	regexp.MustCompile(`(?i)\bauth login\b`),
}

// LooksLikeAuthFailure 报告一段 stderr 是否像认证失败。
func LooksLikeAuthFailure(stderr string) bool {
	for _, re := range authFailurePatterns {
		if re.MatchString(stderr) {
			return true
		}
	}
	return false
}
