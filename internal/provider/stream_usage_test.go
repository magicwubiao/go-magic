package provider

import (
	"context"
	"strings"
	"testing"
)

// TestParseStreamCapturesUsageChunk 钉死「usage 独立 chunk 必须被捕获」。
//
// OpenAI 兼容接口在 stream_options.include_usage 时把 usage 放在一个
// choices 为空、没有 finish_reason 的独立 chunk 里（紧跟在 [DONE] 之前），
// 而 [DONE] 行本身不携带数据。只在 finish_reason 那个 chunk 上取 usage
// 会让用量整块丢失——agent 累计不到 token，/usage 页面因此恒为 0。
func TestParseStreamCapturesUsageChunk(t *testing.T) {
	body := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"Hel"},"finish_reason":null}]}`,
		``,
		`data: {"choices":[{"delta":{"content":"lo"},"finish_reason":"stop"}]}`,
		``,
		`data: {"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":2,"total_tokens":13}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	var content strings.Builder
	var finalDone bool
	var got *Usage
	err := ParseStreamResponseWithTools(context.Background(), strings.NewReader(body), func(resp *StreamResponse) {
		if resp == nil {
			return
		}
		content.WriteString(resp.Content)
		if resp.Done {
			finalDone = true
			got = resp.Usage
		}
	})
	if err != nil {
		t.Fatalf("ParseStreamResponseWithTools: %v", err)
	}
	if !finalDone {
		t.Fatalf("流没有以 Done 结束")
	}
	if got == nil {
		t.Fatalf("usage 没有被传递出来：独立 usage chunk 被丢弃了")
	}
	if got.PromptTokens != 11 || got.CompletionTokens != 2 {
		t.Fatalf("usage = (%d,%d)，期望 (11,2)", got.PromptTokens, got.CompletionTokens)
	}
}
