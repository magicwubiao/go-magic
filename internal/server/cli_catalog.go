package server

import (
	"context"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/cliconn"
	"github.com/magicwubiao/go-magic/pkg/clicatalog"
)

// CLI 应用目录接口。
//
//	GET  /api/cli/catalog          目录列表（带本机是否已安装、版本、是否已添加）
//	POST /api/cli/catalog/check    就绪复检：版本 + 登录/服务状态
//	POST /api/cli/catalog/install  一键添加：校验命令存在 → 写入配置 → 注册工具
//
// 为什么只检测不自动安装：包管理器安装会弹权限确认、耗时不可控、失败原因
// 千差万别（源不可达 / 需要管理员 / 需要重启 shell）。这里把安装命令原样
// 展示给用户复制，装完点「重新检测」即可——服务端不做任何系统写操作。
//
// 同理，`Login` 一类的交互式命令（gh auth login / aws configure）永远由用户
// 在自己的终端跑，服务端只负责展示命令、并在用户说"好了"之后复检一次。

// cliCatalogView 是目录条目的展示形态。
type cliCatalogView struct {
	clicatalog.Entry
	// Installed 表示命令在本机 PATH 上找得到。
	Installed bool `json:"installed"`
	// InstallPath 是解析到的绝对路径（已安装时给出，便于用户确认装的是哪一份）。
	InstallPath string `json:"install_path,omitempty"`
	// Version 是版本探测的首行输出（未安装或条目没配探测时为空）。
	Version string `json:"version,omitempty"`
	// InstallCommands 是当前平台的安装命令（仅展示，服务端不执行）。
	InstallCommands []string `json:"install_commands"`
	// LoginCommands 是当前平台的登录命令（仅展示，服务端不执行）。
	LoginCommands []string `json:"login_commands,omitempty"`
	// Ready 是就绪检查的结论；nil 表示未检查（列表接口不做，避免每次
	// 打开页面都去连 docker daemon / AWS）。
	Ready *bool `json:"ready,omitempty"`
	// ReadyMessage 是就绪检查的首行输出（当前 context、失败原因等）。
	ReadyMessage string `json:"ready_message,omitempty"`
	// Added 表示同名连接器已经存在（此时按钮应变成"去编辑"）。
	Added bool `json:"added"`
}

// cliProbeTimeout 是单条诊断命令的上限。aws sts 这类要走网络，
// 给到 6s；再长就不该在页面里同步等了。
const cliProbeTimeout = 6 * time.Second

// handleCLICatalog 返回内置 CLI 目录，附带本机安装状态。
//
// 列表只做 LookPath + 版本探测（都是本地、毫秒级）；就绪检查由
// /api/cli/catalog/check 按需触发，避免打开页面就等一串网络往返。
func (s *Server) handleCLICatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	existing := s.cliConnectorConfigs()
	entries := clicatalog.All()
	out := make([]cliCatalogView, 0, len(entries))
	for _, e := range entries {
		view := s.buildCatalogView(e)
		_, view.Added = existing[e.DefaultName]
		out = append(out, view)
	}
	jsonResponse(w, map[string]interface{}{
		"entries":    out,
		"categories": clicatalog.Categories(),
	})
}

// handleCLICatalogCheck 对一个目录条目做完整复检（版本 + 就绪）。
//
// 只接受目录 ID：命令与参数全部来自内置目录，用户输入不参与拼装，
// 因此这里不存在"传个 command 进来就能执行任意程序"的口子。
func (s *Server) handleCLICatalogCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := decodeCLIBody(r, &body); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	entry, ok := clicatalog.Get(strings.TrimSpace(body.ID))
	if !ok {
		jsonError(w, http.StatusNotFound, "unknown catalog entry: "+body.ID)
		return
	}

	view := s.buildCatalogView(entry)
	view.Ready, view.ReadyMessage = probeCatalogReadiness(r.Context(), entry)
	jsonResponse(w, view)
}

// handleCLICatalogInstall 把目录条目一键写成连接器。
func (s *Server) handleCLICatalogInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		ID   string            `json:"id"`
		Name string            `json:"name"`
		Env  map[string]string `json:"env"`
		// Overwrite 允许覆盖同名连接器；默认拒绝，避免误把用户改过的配置冲掉。
		Overwrite bool `json:"overwrite"`
		// AllowPlaintext 显式允许把明文凭据写进 config.json。默认拒绝：
		// 疑似 token 的值（ghp_/AKIA/sk- 等）会被拦下并提示改用 ${VAR} 引用，
		// 引用在每次执行前展开，token 不落盘。
		AllowPlaintext bool `json:"allow_plaintext"`
	}
	if err := decodeCLIBody(r, &body); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	entry, ok := clicatalog.Get(strings.TrimSpace(body.ID))
	if !ok {
		jsonError(w, http.StatusNotFound, "unknown catalog entry: "+body.ID)
		return
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = entry.DefaultName
	}
	if err := cliconn.ValidateConnectorName(name); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, exists := s.cliConnectorConfigs()[name]; exists && !body.Overwrite {
		jsonError(w, http.StatusConflict, "a CLI connector named "+name+" already exists")
		return
	}

	// 先校验凭据（纯内存判断），再检测命令：缺 token 是用户马上能改的问题，
	// 不该被"命令还没装"的提示盖住。
	// 只接受目录声明过的环境变量：多余的键不写进配置，避免借安装接口
	// 往子进程里塞任意环境变量。
	env := make([]string, 0, len(entry.Requires))
	for _, req := range entry.Requires {
		value := strings.TrimSpace(body.Env[req.Key])
		if value == "" {
			if req.Required {
				jsonError(w, http.StatusBadRequest, "missing required value: "+req.Label)
				return
			}
			continue
		}
		// 明文凭据防线：疑似 token 的值默认拒绝落盘，引导走 ${VAR} 引用。
		// 引用在每次执行前展开（换 token 不用重启），磁盘上只有变量名。
		if !body.AllowPlaintext && cliconn.LooksSecretLike(value) {
			jsonError(w, http.StatusBadRequest,
				"value for "+req.Key+" looks like a plaintext credential; "+
					"set an environment variable and submit a reference like ${"+req.Key+"} instead, "+
					"or allow storing it as plaintext explicitly")
			return
		}
		env = append(env, req.Key+"="+value)
	}

	if _, err := exec.LookPath(entry.Connector.Command); err != nil {
		hints := entry.Install.CommandsFor(runtime.GOOS)
		msg := "command " + entry.Connector.Command + " was not found on PATH"
		if len(hints) > 0 {
			msg += "; install it first: " + strings.Join(hints, " | ")
		}
		jsonError(w, http.StatusBadRequest, msg)
		return
	}

	conn := entry.Connector
	conn.Env = env
	conn.Enabled = nil // 默认启用：安装完就能用

	if status, err := s.upsertCLIConnectorCore(name, conn); err != nil {
		jsonError(w, status, err.Error())
		return
	}

	// 装完当场自检一次：用户能立刻看到"命令在、版本对、登录也好了"，
	// 而不是等进了对话才发现 401。
	ready, message := probeCatalogReadiness(r.Context(), entry)
	jsonResponse(w, map[string]interface{}{
		"success":       true,
		"name":          name,
		"ready":         ready,
		"ready_message": message,
	})
}

// buildCatalogView 组装条目的展示信息（不含就绪检查）。
func (s *Server) buildCatalogView(e clicatalog.Entry) cliCatalogView {
	view := cliCatalogView{
		Entry:           e,
		InstallCommands: e.Install.CommandsFor(runtime.GOOS),
		LoginCommands:   e.Login.CommandsFor(runtime.GOOS),
	}
	path, err := exec.LookPath(e.Connector.Command)
	if err != nil {
		return view
	}
	view.Installed = true
	view.InstallPath = path
	if len(e.Detect) > 0 {
		res := cliconn.Probe(context.Background(), path, e.Detect, cliProbeTimeout)
		if res.Err == nil && res.ExitCode == 0 {
			view.Version = res.Output
		}
	}
	return view
}

// probeCatalogReadiness 跑条目的就绪检查，返回结论与一句话说明。
//
// 没装 / 没配检查项时返回 nil：nil 表示"没有结论"，而不是"不可用"——
// 否则 git、jq 这类没有登录概念的工具会永远显示红点。
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
