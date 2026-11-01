package server

import (
	"errors"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/mcp"
)

// 手写连接器拿不到 token 时，错误里必须自带"下一步在哪"。
// 连接器页的 CLI 页签删掉之后，底层那句"去 UI 完成授权"指向的按钮已经不存在了。
func TestAnnotateCLIOAuthError(t *testing.T) {
	base := mcp.WrapKind(mcp.ErrKindAuth,
		errors.New(`no OAuth credentials for "cliconn:gh" — complete the authorization flow in the UI`))

	// 插件内连接器有完整的凭据弹窗与"去登录"，原样返回，不加噪音。
	if got := annotateCLIOAuthError("cliconn:agentplugin/demo/gh", base); got != base {
		t.Fatalf("plugin connector error was annotated: %v", got)
	}

	got := annotateCLIOAuthError("cliconn:gh", base)
	if !errors.Is(got, base) {
		t.Fatalf("original error lost: %v", got)
	}
	// 分类必须穿过这层包装：上层（agent 的工具结果、前端 blocker）按它决定
	// 展示"重新登录"还是普通失败。
	if kind := mcp.KindOf(got); kind != mcp.ErrKindAuth {
		t.Fatalf("kind = %q, want auth", kind)
	}
	for _, want := range []string{"agent plugin", "token_env"} {
		if !strings.Contains(got.Error(), want) {
			t.Errorf("migration hint missing %q: %v", want, got)
		}
	}
}
