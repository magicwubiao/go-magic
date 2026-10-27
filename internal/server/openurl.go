package server

// openURLBrowser 用系统默认浏览器打开 URL（OAuth 自动授权用）。
//
// best-effort：弹不出浏览器（headless 服务器、无桌面会话）只记日志不报错，
// 用户仍然可以从连接器页手动点"登录"拿 authorize URL。
//
// 只 Start 不 Wait：打开浏览器是 fire-and-forget，等它返回没有意义；
// 也绝不 attach 输出管道，避免浏览器进程的 stdout 堵住我们的管道。

import (
	"os/exec"
	"runtime"

	"github.com/magicwubiao/go-magic/pkg/log"
)

func openURLBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// rundll32 比 "cmd /c start" 安全：url 作为单参数传递，无 shell 拼接。
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Warnf("[MCP] failed to open browser for OAuth (open %s manually): %v", url, err)
	}
}
