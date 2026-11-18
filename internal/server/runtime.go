package server

// 运行时一键安装：Node.js / Python / Git Bash（Windows）。
//
// 为什么独立于 pkg/clicatalog：CLI 目录的收录标准是"凭据要留在进程环境里的
// 跨服务工具"，node/python/git 这类本机运行时装进去只会白白吃掉工具列表
// 上下文；但 MCP server（npx）、skill 脚本、Windows 上的 shell 又确实依赖
// 它们，缺了应用就是半残。所以单独开一个固定清单的安装入口。
//
// 安全边界（与 cli_install.go 一致，这里是其第二处实例）：
//   - 只执行**本文件硬编码**的安装命令：请求体只能传运行时 id，
//     不接受任何命令字符串——否则这就是一个任意命令执行接口；
//   - 需要 sudo 的命令不代跑：服务端没有 TTY，必然卡在密码提示；
//   - 安装是长任务（winget / brew 可能几分钟），复用 cli_install.go 的
//     "提交任务 + 轮询"机制，catalog_id 用 "runtime:<id>" 前缀与 CLI 目录
//     任务区分，轮询接口复用 GET /api/plugins/cli/install/{id}。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// runtimeInstallTimeout 是版本探测的硬上限（安装任务的超时在 cli_install.go）。
const runtimeProbeTimeout = 3 * time.Second

// runtimeSpec 是一个可安装的本机运行时。
type runtimeSpec struct {
	// ID 是前端唯一能传的东西（node / python / gitbash）。
	ID string
	// Name 是展示名（英文，中文由前端 locale 提供）。
	Name string
	// Binary 是安装完成后用于确认"是否已可用"的可执行文件名。
	Binary string
	// Probe 列出候选可执行文件：探测时按顺序找第一个能报出版本的。
	// Windows 的 python 是商店别名 stub，LookPath 能找到但跑不出版本，
	// 所以 found 以"能报出版本"为准而不是只看 LookPath。
	Probe []string
	// Install 按 GOOS 给安装命令（按顺序试，第一条成功即停）。
	Install map[string][]string
}

// 内置运行时清单。新增条目只改这里——接口侧依旧只认 id。
var runtimeSpecs = []runtimeSpec{
	{
		ID:     "node",
		Name:   "Node.js",
		Binary: "node",
		Probe:  []string{"node"},
		Install: map[string][]string{
			"windows": {"winget install --id OpenJS.NodeJS.LTS -e"},
			"darwin":  {"brew install node"},
			"linux":   {"apt-get install -y nodejs npm"},
		},
	},
	{
		ID:     "python",
		Name:   "Python",
		Binary: "python",
		Probe:  []string{"python3", "python"},
		Install: map[string][]string{
			"windows": {"winget install --id Python.Python.3.12 -e"},
			"darwin":  {"brew install python3"},
			"linux":   {"apt-get install -y python3"},
		},
	},
	{
		// Git Bash 对 Windows 是刚需（process 工具的命令面依赖它），
		// 其它平台装的就是 git 本体。
		ID:     "gitbash",
		Name:   "Git Bash",
		Binary: "git",
		Probe:  []string{"git"},
		Install: map[string][]string{
			"windows": {"winget install --id Git.Git -e"},
			"darwin":  {"brew install git"},
			"linux":   {"apt-get install -y git"},
		},
	},
}

// runtimeSpecByID 按 id 取运行时定义。
func runtimeSpecByID(id string) (runtimeSpec, bool) {
	for _, spec := range runtimeSpecs {
		if spec.ID == id {
			return spec, true
		}
	}
	return runtimeSpec{}, false
}

// runtimeProbeResult 是单个运行时的探测结果（给前端列表）。
type runtimeProbeResult struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Binary  string `json:"binary"`
	Found   bool   `json:"found"`
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
}

// probeRuntime 对候选可执行文件逐个探测：LookPath + 跑一次 --version。
//
// 只喂硬编码的 argv（与 cliconn.Probe 同一纪律），不掺任何用户输入。
// 版本输出取第一行（如 v20.11.0 / Python 3.12.4 / git version 2.45.1）。
func probeRuntime(spec runtimeSpec) runtimeProbeResult {
	out := runtimeProbeResult{ID: spec.ID, Name: spec.Name, Binary: spec.Binary}
	for _, bin := range spec.Probe {
		path, err := exec.LookPath(bin)
		if err != nil {
			continue
		}
		version := runtimeVersion(path)
		if version == "" {
			// Windows 商店 stub 的典型形态：在 PATH 上但跑不出版本。
			continue
		}
		out.Found = true
		out.Path = path
		out.Version = version
		return out
	}
	return out
}

// runtimeVersion 跑 `<path> --version`，返回去空白后的首行；失败返回空。
func runtimeVersion(binPath string) string {
	ctx, cancel := context.WithTimeout(context.Background(), runtimeProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath, "--version")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	text := strings.TrimSpace(string(output))
	if text == "" {
		return ""
	}
	// 首行就够展示了；多余行（如 git 的版权行）裁掉。
	if idx := strings.IndexAny(text, "\r\n"); idx >= 0 {
		text = strings.TrimSpace(text[:idx])
	}
	return text
}

// handleRuntimeList 列出全部内置运行时的本机状态。
//
//	GET /api/runtimes
//
// 有未就绪项时先从注册表刷新一次 PATH 再重探（Windows 安装器改的是注册表，
// 运行中进程的 PATH 是启动快照）：覆盖"用户刚装完回来点刷新"以及"在
// 外部终端自己装的"两种场景，省掉一次重启。
func (s *Server) handleRuntimeList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	probeAll := func() []runtimeProbeResult {
		items := make([]runtimeProbeResult, 0, len(runtimeSpecs))
		for _, spec := range runtimeSpecs {
			items = append(items, probeRuntime(spec))
		}
		return items
	}
	items := probeAll()
	missing := false
	for _, it := range items {
		if !it.Found {
			missing = true
			break
		}
	}
	if missing {
		refreshProcessPath()
		items = probeAll()
	}
	jsonResponse(w, map[string]any{
		"ok":       true,
		"platform": runtime.GOOS,
		"runtimes": items,
	})
}

// handleRuntimeInstall 提交一次运行时安装任务。
//
//	POST /api/runtimes/install
//	body: {"id":"node"}
//
// 轮询复用 GET /api/plugins/cli/install/{id}：任务都存在同一个表里，
// 前端一份轮询逻辑同时服务 CLI 目录与运行时。
func (s *Server) handleRuntimeInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid json body: "+err.Error())
		return
	}
	spec, ok := runtimeSpecByID(strings.TrimSpace(body.ID))
	if !ok {
		jsonError(w, http.StatusNotFound, "unknown runtime: "+body.ID)
		return
	}

	catalogID := "runtime:" + spec.ID
	if running := s.runningInstallJob(catalogID); running != nil {
		jsonResponse(w, map[string]any{"ok": true, "reused": true, "job": running.snapshot()})
		return
	}

	commands := runtimeInstallCommands(spec)
	if len(commands) == 0 {
		jsonError(w, http.StatusBadRequest,
			fmt.Sprintf("runtime %q has no install command for %s", spec.ID, runtime.GOOS))
		return
	}

	job := &cliInstallJob{
		id:        newInstallJobID(),
		catalogID: catalogID,
		commands:  commands,
		state:     cliInstallRunning,
		binary:    spec.Binary,
		startedAt: time.Now(),
	}
	s.addInstallJob(job)
	go s.runCLIInstall(job)

	jsonResponse(w, map[string]any{"ok": true, "job": job.snapshot()})
}

// runtimeInstallCommands 取当前平台的安装命令，套用与 CLI 目录相同的
// 无人值守规则：过滤 sudo、给 winget 补协议接受开关。
func runtimeInstallCommands(spec runtimeSpec) []string {
	raw := spec.Install[runtime.GOOS]
	var out []string
	for _, c := range raw {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		// sudo 会等密码，服务端没有 TTY，跑了只会挂住十分钟。
		if strings.Contains(c, "sudo ") || strings.HasPrefix(c, "sudo") {
			continue
		}
		out = append(out, unattendedCommand(c))
	}
	return out
}
