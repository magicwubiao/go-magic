package tool

import (
	"testing"

	cruntime "github.com/chromedp/cdproto/runtime"
)

// 背景：browser_console 走 CDP Runtime.evaluate，脚本按 Script 编译，
// 顶层 `return` / 顶层 `await` 都是语法错误——而"return document.title"
// 正是模型写控制台片段的自然写法。线上日志里这类报错出现 14 次
// （"SyntaxError: Illegal return statement"）。
// 修复：识别这类错误后包一层 async IIFE 重试一次。这里锁死识别口径。

func TestNeedsAsyncIIFE(t *testing.T) {
	mk := func(desc, text string) *cruntime.ExceptionDetails {
		e := &cruntime.ExceptionDetails{Text: text}
		if desc != "" {
			e.Exception = &cruntime.RemoteObject{Description: desc}
		}
		return e
	}

	cases := []struct {
		name string
		exc  *cruntime.ExceptionDetails
		want bool
	}{
		{"nil 异常不重试", nil, false},
		{"顶层 return", mk("SyntaxError: Illegal return statement", "Uncaught"), true},
		{"顶层 return 小写描述", mk("syntaxerror: illegal return statement", ""), true},
		{"只有 Text 字段也要能识别", mk("", "SyntaxError: Illegal return statement"), true},
		{"顶层 await", mk("SyntaxError: await is only valid in async functions", "Uncaught"), true},
		{"普通运行时错误不重试", mk("TypeError: Cannot read properties of undefined (reading 'click')", "Uncaught"), false},
		{"元素查询失败不重试", mk("DOM Error while querying (-1)", ""), false},
		{"空描述不重试", mk("", ""), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := needsAsyncIIFE(tc.exc); got != tc.want {
				t.Fatalf("needsAsyncIIFE() = %v, want %v", got, tc.want)
			}
		})
	}
}
