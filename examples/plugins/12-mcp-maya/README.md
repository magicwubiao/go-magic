# 12 · Maya MCP

Autodesk Maya 的"本地桥"：[Xxx91n/mcp-for-maya](https://github.com/Xxx91n/mcp-for-maya)（MIT）走的是 Maya **自带的 Python commandPort**，不需要装 add-on。`uvx mcp-for-maya` 在宿主侧起 MCP server，再反过去连 Maya 里那个 `:7001` 命令端口。

> **为什么不在内置目录里？**
> 它要求 Maya 装在**和 go-magic 同一台机器**上，并且命令端口已经打开。go-magic 常部署在服务器上，内置成目录条目就是"点了必然超时"或"连上了却零工具"的一条，用户没法自己判断是环境问题。

## 前置条件

1. 本机装 **Maya 2023 或更新**（注入用的 helper 依赖 `ast.unparse`；实测 2024 GUI）。更早的版本会在注入时直接报错，不是"可能不支持"；
2. 本机装 **uv**（提供 `uvx`）。server 需要 Python 3.10+，`uvx` 会自己拉——**不要往 Maya 自带的 Python 里装**，两套环境本来就该分开；
3. 在 Maya 的 **Script Editor** 里把语言切到 **Python**（不是 MEL），执行：

```python
import maya.cmds as cmds
cmds.commandPort(name=":7001", sourceType="python")
```

4. 装本插件并连接：

```bash
magic plugin install ./examples/plugins/12-mcp-maya
```

`commandPort` **不随 Maya 会话持久**，重启 Maya 就没了。要免手动，把它写进 `userSetup.py`：

```python
import maya.cmds as cmds
cmds.evalDeferred('cmds.commandPort(name=":7001", sourceType="python")', lowestPriority=True)
```

各平台 scripts 目录：

- Windows：`%MAYA_APP_DIR%\<版本>\scripts\userSetup.py`
- Linux：`~/maya/<版本>/scripts/userSetup.py`
- macOS：`~/Library/Preferences/Autodesk/maya/<版本>/scripts/userSetup.py`

不确定路径时在 Maya 的 Python 里执行 `import maya.cmds as cmds; print(cmds.internalVar(userScriptDir=True))`。

## 7001 不是 MCP 地址

和 11（FreeCAD）同一类坑：**7001 是 Maya 的 Python 命令端口**，不是 MCP 的 http/sse 端点。想"手动加个连接器填 `http://127.0.0.1:7001`"一定失败——传输只能是 `stdio`，由 `uvx mcp-for-maya` 去连 7001。

端口是不是开着，在宿主 shell 里就能验：

```bash
# macOS / Linux
nc -z 127.0.0.1 7001 && echo open
# Windows PowerShell
Test-NetConnection 127.0.0.1 -Port 7001
```

## 多个 Maya 实例

`commandPort` 是对 `host:port` 的独占监听，同端口第二个实例 bind 会失败，所以**每个实例要一个端口**（`:7001`、`:7002`……）。server 会自动扫描本机 Maya 端口，扫不到时用 `add_session(host, port)` 手动加；也可以用环境变量把范围收窄：

```json
"env": {
  "MAYA_MCP_INCLUDE_PORTS": "7001,7002",
  "MAYA_MCP_EXCLUDE_PORTS": "7011"
}
```

（`INCLUDE_PORTS` 支持区间写法，如 `7005-7010`。）

## 失败长什么样（都是环境问题）

| 现象 | 原因 |
|---|---|
| 连接超时 / 进程立刻退出 | `uvx` 不在 PATH，或拉包失败（内网/代理） |
| 连上了但 `list_sessions` 返回空 | Maya 没开，或命令端口没打开（Maya 重启后最常见） |
| 工具调用报连接被拒 | 端口号对不上，或 `commandPort` 只开了 MEL 端口 |
| Script Editor 反复刷语法错误 | 扫到了 MEL 端口；新版会自动豁免，仍异常就用 `EXCLUDE_PORTS` 排掉 |
| `scene_viewport_snapshot` 报能力错误 | 当前是 headless / `mayapy` 会话；视口截图和 playblast 只对 GUI 会话可用 |
| 注入时报版本错误 | Maya 低于 2023 |

第一次连不上，让模型调 `maya_setup_guide(action="diagnose")`，它会按端口、进程、`userSetup.py` 逐项查。

## 安全：这桥是"在 Maya 里远程执行 Python"

`execute_code` / `write_module` 会以**你的用户权限**在 Maya 里跑任意 Python，删文件、改场景都算它。默认边界与收窄开关：

- 命令端口只绑 localhost，Maya 本体不开网络面；
- 动手前先存盘。模型可能不打招呼就删节点、改拓扑，重要场景开着版本管理；
- 只是不想让它跑任意代码，按需加环境变量（放 `mcp.json` 的 `env` 里）：

| 变量 | 作用 |
|---|---|
| `MAYA_MCP_DISABLE_EXECUTE=1` | 去掉 `execute_code` |
| `MAYA_MCP_DISABLE_WRITE_MODULE=1` | 去掉 `write_module` |
| `MAYA_MCP_DISABLE_ARBITRARY=1` | 三个逃生舱一起禁 |
| `MAYA_MCP_SAFE_MODE=1` | 保留 `execute_code`，但代码要过默认拒绝的 AST 允许清单 |

- 唯一的对外流量是两个资产工具（Poly Haven，CC0）：只在调用时访问 `api.polyhaven.com` / `dl.polyhaven.com`，下载在宿主侧做 md5 校验。不调用就不出网。

## 省 token

`scene_snapshot` 一次就能拿到全场景的空间模型，比逐个 `scene_inspect` 便宜得多；截图工具返回的是图像，纯文本客户端或想省钱就别主动要图，先用 `scene_assert` / `scene_review` 做文本校验。
