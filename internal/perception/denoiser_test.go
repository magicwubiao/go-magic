package perception

import "testing"

// TestDetectNoiseIsConservative 锁死"噪声检测误报导致模型反问用户"的回归。
//
// 每条命中都会把 ambiguity 抬给 cognition 层，最终可能变成 ClarificationNeeded
// ⇒ 模型反问用户。旧实现的假阳性率过高：只要输入含 3 个以上问号就判歧义，
// 而中文用户写多问句是常态。这里的用例都是**正常任务**，必须全部判为无噪声。
func TestDetectNoiseIsConservative(t *testing.T) {
	p := NewParser()

	clean := []struct {
		name  string
		input string
	}{
		{"cjk multi-question is a normal request", "能帮我改下首页导航吗？太多了。顺便看下表单？还有那个页脚？"},
		{"code block question marks are not questions", "这个正则是干嘛的\n```\nvar re = /a?b?c?/\n```\n帮我解释下"},
		{"url query string is not a question", "看看 https://example.com/a?b=1&c=2&d=3 这个接口"},
		{"normal cjk task", "把导航精简一下"},
		{"normal english task", "refactor the navigation bar"},
		{"two questions is fine", "这是什么？要怎么改？"},
		{"single char punctuation only", "?"},
	}
	for _, tc := range clean {
		t.Run(tc.name, func(t *testing.T) {
			got := p.detectNoise(tc.input)
			if got.HasNoise {
				t.Errorf("expected no noise for %q, got noise types %v (suggestions %v)",
					tc.input, got.NoiseTypes, got.Suggestions)
			}
		})
	}
}

// TestDetectNoiseStillCatchesRealNoise 确保收紧阈值后仍能识别真正需要澄清的
// 输入——修 bug 不能把功能一起修掉。
func TestDetectNoiseStillCatchesRealNoise(t *testing.T) {
	p := NewParser()

	dirty := []struct {
		name  string
		input string
	}{
		{"cjk too short", "改"},
		{"latin too short", "ab"},
		{"many parallel questions", "a? b? c? d? e?"},
		{"vague english task", "do something with the files"},
		{"vague english task 2", "just make something for me"},
	}
	for _, tc := range dirty {
		t.Run(tc.name, func(t *testing.T) {
			got := p.detectNoise(tc.input)
			if !got.HasNoise {
				t.Errorf("expected noise to be detected for %q, got none", tc.input)
			}
		})
	}
}

// TestCountRealQuestionsSkipsCodeAndURLs 直接锁语义：统计问号时必须跳过
// 代码块、行内代码与 URL。
func TestCountRealQuestionsSkipsCodeAndURLs(t *testing.T) {
	cases := []struct {
		input string
		want  int
	}{
		{"??? ", 3},
		{"```\n???\n```", 0},
		{"`a?b?c?`", 0},
		{"https://x.com/p?a=1&b=2&c=3", 0},
		{"你问号？中文的？也要算？", 3},
	}
	for _, tc := range cases {
		if got := countRealQuestions(tc.input); got != tc.want {
			t.Errorf("countRealQuestions(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

// TestIsOnlyPunctuation 保证空白/标点输入不会被当成"内容过短的不完整请求"。
func TestIsOnlyPunctuation(t *testing.T) {
	if !isOnlyPunctuation("?  ! ,") {
		t.Error("pure punctuation should be detected")
	}
	if isOnlyPunctuation("hi 中文1") {
		t.Error("text with letters/digits is not pure punctuation")
	}
}
