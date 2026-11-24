package tool

import (
	"strings"
	"testing"
)

// 回归：analyze_error 是纯规则匹配，规则未覆盖的错误此前返回空
// possible_causes/suggestions，对调用方没有分析价值。
// 现在要求：常见错误模式有具体分析；未命中模式的输入也有非空兜底。

func hasContent(analysis *ErrorAnalysis) bool {
	return len(analysis.Causes) > 0 && len(analysis.Suggestions) > 0
}

func TestAnalyzeErrorKnownPatternsHaveContent(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantSub string // causes 或 suggestions 中应出现的特征文本
	}{
		{
			name:    "go custom panic (no specific branch)",
			input:   "panic: custom failure while opening DB\n\ngoroutine 1 [running]:\nmain.main()\n\t/app/main.go:12 +0x25",
			wantSub: "runtime condition",
		},
		{
			name:    "go missing module",
			input:   "main.go:5:2: no required module provides package github.com/foo/bar; to add it:\n\tgo get github.com/foo/bar",
			wantSub: "go mod tidy",
		},
		{
			name:    "python attribute error",
			input:   "Traceback (most recent call last):\n  File \"app.py\", line 3, in main\nAttributeError: 'NoneType' object has no attribute 'append'",
			wantSub: "attribute",
		},
		{
			name:    "python module not found",
			input:   "Traceback (most recent call last):\n  File \"app.py\", line 1, in <module>\nModuleNotFoundError: No module named 'requests'",
			wantSub: "pip install",
		},
		{
			name:    "python file not found",
			input:   "Traceback (most recent call last):\n  File \"app.py\", line 2, in main\nFileNotFoundError: [Errno 2] No such file or directory: 'data.csv'",
			wantSub: "os.path.exists",
		},
		{
			name:    "js is not a function",
			input:   "TypeError: x.foo is not a function\n    at main (app.js:10:5)",
			wantSub: "import/export",
		},
		{
			name:    "node ENOENT",
			input:   "Error: ENOENT: no such file or directory, open 'config.json'\n    at Object.openSync (fs.js:1:1)",
			wantSub: "path does not exist",
		},
		{
			name:    "node ECONNREFUSED",
			input:   "Error: connect ECONNREFUSED 127.0.0.1:5432\n    at TCPConnectWrap.afterConnect (net.js:1:1)",
			wantSub: "service not running",
		},
		{
			name:    "java number format",
			input:   "Exception in thread \"main\" java.lang.NumberFormatException: For input string: \"abc\"\n\tat java.lang.Integer.parseInt(Integer.java:652)",
			wantSub: "Integer.parseInt",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := analyzeError(tc.input, detectLanguageFromError(tc.input))
			if !hasContent(got) {
				t.Fatalf("analysis for %q returned empty causes/suggestions: %#v", tc.input, got)
			}
			joined := strings.Join(got.Causes, " ") +
				"+" + suggestionsToString(got.Suggestions)
			if !strings.Contains(strings.ToLower(joined), strings.ToLower(tc.wantSub)) {
				t.Errorf("analysis should mention %q, got causes=%v suggestions=%+v",
					tc.wantSub, got.Causes, got.Suggestions)
			}
		})
	}
}

func suggestionsToString(s []FixSuggestion) string {
	var b strings.Builder
	for _, x := range s {
		b.WriteString(x.Description)
		b.WriteString(" ")
		b.WriteString(x.Code)
	}
	return b.String()
}

// TestAnalyzeErrorNeverEmpty 兜底：任意未识别的输入都必须给出非空分析，
// 而不是空数组。
func TestAnalyzeErrorNeverEmpty(t *testing.T) {
	inputs := []string{
		"exit status 1",
		"something went terribly wrong",
		"ERROR 42: the frobnicator failed to frobnicate",
	}
	for _, in := range inputs {
		got := analyzeError(in, "unknown")
		if !hasContent(got) {
			t.Errorf("input %q produced empty causes/suggestions: %#v", in, got)
		}
	}
}
