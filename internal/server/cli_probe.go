package server

// 目录条目的就绪探测：给"登录是否完成"用（cli_login.go 的委托登录轮询）。
//
// 探测命令来自 pkg/clicatalog 的 Entry.Check（如 `aws sts get-caller-identity`、
// `gh auth status`），只在这两个场景跑：登录收尾与登录轮询——不在页面加载时
// 批量跑，否则打开插件页要等一串网络往返。

import (
	"context"
	"os/exec"
	"time"

	"github.com/magicwubiao/go-magic/internal/cliconn"
	"github.com/magicwubiao/go-magic/pkg/clicatalog"
)

// cliProbeTimeout 是单条诊断命令的上限。aws sts 这类要走网络，
// 给到 6s；再长就不该在页面里同步等了。
const cliProbeTimeout = 6 * time.Second

// probeCatalogReadiness 跑条目的就绪检查，返回结论与一句话说明。
//
// 没装 / 没配检查项时返回 nil：nil 表示"没有结论"，而不是"不可用"——
// 否则 docker 这类没有登录概念的工具会永远显示红点。
func probeCatalogReadiness(ctx context.Context, e clicatalog.Entry) (*bool, string) {
	path, err := exec.LookPath(e.Connector.Command)
	if err != nil {
		notReady := false
		return &notReady, "command not found on PATH"
	}
	if len(e.Check) == 0 {
		return nil, ""
	}
	res := cliconn.Probe(ctx, path, e.Check, cliProbeTimeout)
	ok := res.Err == nil && res.ExitCode == 0
	msg := res.Output
	if res.TimedOut {
		msg = "timed out after " + cliProbeTimeout.String()
	}
	if res.Err != nil {
		msg = res.Err.Error()
	}
	return &ok, msg
}
