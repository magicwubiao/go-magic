# 08 · Godot MCP

Godot 是这三个引擎里唯一**不需要 add-on** 的：server 直接调 Godot 的 CLI（`--headless --script`）干活，唯一的依赖是"能找到一个可用的 Godot 可执行文件"。

> **为什么不在内置目录里？**
> 它要一个装在这台机器上的 Godot 可执行文件。go-magic 跑在服务器上时，这条配置只能对应一个不存在的路径——内置进目录就是一条"连上了但每个工具都报找不到引擎"的条目。

## 前置条件

1. 本机装 **Godot 4.x**；
2. 本机装 **Node**（提供 `npx`）；
3. 装本插件，并在插件凭据里填 **`GODOT_PATH`**，再连接。

```bash
magic plugin install ./examples/plugins/08-mcp-godot
```

`mcp.json` 里 `"GODOT_PATH": ""` 的空值**不是笔误**：值为空的 env key 会被当成"待填凭据"，插件页据此弹出输入框、加密保存。写死成某个盘符路径会让别人装上就坏。

server 会自己探测常见安装位置（macOS 的 `/Applications/Godot.app`、Linux 的 `/usr/bin/godot`、Windows 的 `C:\Program Files\Godot\Godot.exe`，以及 PATH 里的 `godot`）。Windows 上大家多用便携版 zip（`Godot_v4.x-stable_win64.exe`），探不到，所以必须填：

```
C:\Godot\Godot_v4.5-stable_win64.exe
```

## 失败长什么样

| 现象 | 原因 |
|---|---|
| 连接超时 / 进程立刻退出 | `npx` 不在 PATH，或拉包失败（内网/代理） |
| 工具调用报 `Could not find a valid Godot executable` | `GODOT_PATH` 没填或填错——这是本示例最常见的坑 |
| 工具调用报项目路径错误 | 传的路径下面没有 `project.godot` |

填错的代价很小：探测失败会回落到默认路径并继续起服务，只是工具调用会失败。
