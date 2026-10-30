package server

import (
	"net/http"
	"os/exec"
	"runtime"
	"strings"

	"github.com/magicwubiao/go-magic/internal/cliconn"
	"github.com/magicwubiao/go-magic/pkg/clicatalog"
)

// CLI 应用目录接口。
//
//	GET  /api/cli/catalog          目录列表（带本机是否已安装、是否已添加）
//	POST /api/cli/catalog/install  一键添加：校验命令存在 → 写入配置 → 注册工具
//
// 为什么只检测不自动安装：包管理器安装会弹权限确认、耗时不可控、失败原因
// 千差万别（源不可达 / 需要管理员 / 需要重启 shell）。这里把安装命令原样
// 展示给用户复制，装完点「重新检测」即可——服务端不做任何系统写操作。

// cliCatalogView 是目录条目的展示形态。
type cliCatalogView struct {
	clicatalog.Entry
	// Installed 表示命令在本机 PATH 上找得到。
	Installed bool `json:"installed"`
	// InstallPath 是解析到的绝对路径（已安装时给出，便于用户确认装的是哪一份）。
	InstallPath string `json:"install_path,omitempty"`
	// InstallCommands 是当前平台的安装命令（仅展示，服务端不执行）。
	InstallCommands []string `json:"install_commands"`
	// Added 表示同名连接器已经存在（此时按钮应变成"去编辑"）。
	Added bool `json:"added"`
}

// handleCLICatalog 返回内置 CLI 目录，附带本机安装状态。
func (s *Server) handleCLICatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	existing := s.cliConnectorConfigs()
	entries := clicatalog.All()
	out := make([]cliCatalogView, 0, len(entries))
	for _, e := range entries {
		view := cliCatalogView{
			Entry:           e,
			InstallCommands: e.Install.CommandsFor(runtime.GOOS),
		}
		if path, err := exec.LookPath(e.Connector.Command); err == nil {
			view.Installed = true
			view.InstallPath = path
		}
		_, view.Added = existing[e.DefaultName]
		out = append(out, view)
	}
	jsonResponse(w, map[string]interface{}{
		"entries":    out,
		"categories": clicatalog.Categories(),
	})
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

	s.upsertCLIConnector(w, name, conn)
}
