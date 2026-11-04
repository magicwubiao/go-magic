# 11 · FreeCAD MCP

CAD 版"本地桥"：[neka-nat/freecad-mcp](https://github.com/neka-nat/freecad-mcp)（MIT）由两半组成——FreeCAD 里的 add-on 在 `127.0.0.1:9875` 起 XML-RPC 服务，外面的 `uvx freecad-mcp` 只做转发。与 06（Blender）同构，区别是 Blender 的 add-on 只监听、由 server 反向连；FreeCAD 这边也一样，但**协议是 XML-RPC，不是 MCP over HTTP**。

> **为什么不在内置目录里？**
> 它要求 FreeCAD 装在**和 go-magic 同一台机器**上，并且 add-on 已经点过 Start RPC Server。go-magic 常部署在服务器上，内置成目录条目就是"点了必然超时"或"连上了却零工具"的一条，用户没法自己判断是环境问题。

## 前置条件

1. 本机装 **FreeCAD 1.1+**（1.1 的 Mod 目录多一层版本号；更早版本没有这层）：
   - Windows：`%APPDATA%\FreeCAD\v1-1\Mod\`
   - macOS：`~/Library/Application Support/FreeCAD/v1-1/Mod/`
   - 不确定就打开发 FreeCAD → 视图 → 面板 → Python 控制台，执行
     `import os; print(os.path.join(FreeCAD.getUserAppDataDir(), "Mod"))`
2. 把仓库里的 **`addon/FreeCADMCP` 这一个文件夹**拷进 Mod 目录。别把整个仓库放进去，也别多套一层同名目录——层级错了 add-on 根本不会加载。
3. 重启 FreeCAD，切到 **MCP Addon** 工作台 → **Start RPC Server**（菜单里勾 **Auto-Start Server** 可以以后免点）。没出现工作台就先看 Report View 的加载错误。
4. 本机装 **uv**（提供 `uvx`）。server 需要 Python 3.12+，`uvx` 会自己拉——**不要把它的依赖装进 FreeCAD 自带的 Python**，两套环境本来就该分开。
5. 装本插件：

```bash
magic plugin install ./examples/plugins/11-mcp-freecad
```

## 9875 不是 MCP 地址

这是本示例最容易踩的坑：9875 是 **add-on 的 XML-RPC 端点**，不是 MCP 的 http/sse 端口。想"手动加个连接器填 `http://127.0.0.1:9875`"一定失败——MCP 传输只能是 `stdio`，由 `uvx freecad-mcp` 去访问 9875。判断端口有没有起来，可以在 FreeCAD 的 Python 控制台里执行：

```python
from rpc_server import rpc_server as bridge
print(bridge.rpc_server_instance.server_address if bridge.rpc_server_instance else "RPC server stopped")
```

## 失败长什么样（都是环境问题）

| 现象 | 原因 |
|---|---|
| 连接超时 / 进程立刻退出 | `uvx` 不在 PATH，或拉包失败（内网/代理） |
| 连上了但工具列表为空 | FreeCAD 没开；add-on 没加载（目录层级放错，多套了一层同名文件夹）；RPC server 没启动 |
| 工具调用报连接被拒 | 9875 没在监听，或 add-on 与 server 的协议版本不匹配（两边一起升级，别只升一半） |
| 配成 http/sse 后连不上 | 9875 是 XML-RPC，传输必须 stdio（见上一节） |
| Windows 上报 `EFTYPE` / `uv_spawn` | 用 `cmd /c` 包一层：`"command": "cmd", "args": ["/c", "freecad-mcp"]`，或填 `freecad-mcp.exe` 的绝对路径 |

## 安全：这桥是"远程执行 Python"

add-on 的 `execute_code` 工具会在 FreeCAD 里以**你的用户权限**跑任意 Python，写文件、删文档都算它。几个默认与边界：

- 默认只监听 localhost，不接受远程连接，也拒绝浏览器发来的请求；
- 模型可能不打招呼就关文档、改尺寸。**动手前先存盘**，重要模型开着版本管理（或 Git 管 `.FCStd`）；
- 确实要跨机器用时，别只开端口：FreeCAD 里勾 **Remote Connections** + 填 **Allowed IPs** + **Set Auth Token**，server 侧用 `"env": {"FREECAD_MCP_TOKEN": "..."}` 带同一枚 token。token 明文传输，不受控的网络请改用 SSH 隧道（`ssh -N -L 9875:localhost:9875 user@freecad-host`），server 保持默认 `--host localhost`；
- 跨机器时只想让 server 连过去、不开放网络，`--host <ip>` 配合 SSH 隧道即可。

## 省 token

每次工具返回都带一张视口截图，视觉模型才能"看见"自己改了什么，但很费 token。纯文本客户端或想省钱，在 `mcp.json` 的 `args` 里加 `--only-text-feedback`，也可以用 `get_view` 主动取图。

```json
"args": ["freecad-mcp", "--only-text-feedback"]
```
