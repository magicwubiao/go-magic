package integration

import "testing"

// TestIsNoiseContent 验证 P1-4 噪声过滤：代码块/URL/路径/命令不入库。
func TestIsNoiseContent(t *testing.T) {
	noise := []string{
		"here is the code:\n```go\nfunc main() {}\n```",
		"check https://example.com/some/page for details",
		"the file is at C:\\Users\\someone\\config.json",
		"run `kubectl get pods -n prod` please",
		"import os\nimport sys\nprint(os.getcwd())",
		"",
	}
	for _, n := range noise {
		if !isNoiseContent(n) {
			t.Errorf("isNoiseContent(%q) = false, want true", n)
		}
	}

	keep := []string{
		"user prefers short answers with bullet points",
		"the deadline for the payment project is next Friday",
		"我们决定使用 PostgreSQL 作为主数据库",
	}
	for _, k := range keep {
		if isNoiseContent(k) {
			t.Errorf("isNoiseContent(%q) = true, want false", k)
		}
	}
}
