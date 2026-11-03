# 06 · Blender MCP

一个"server 只做转发、真身在 Blender 里"的本地桥示例。

> **为什么不在内置目录里？**
> 它要求 Blender 装在**和 go-magic 同一台机器**上，并且里面的 add-on 已经点过 Connect。go-magic 经常部署在服务器或另一台机器上，内置成目录条目就是一条点了必然超时 / 连上了却零工具的条目，用户没法自己判断是环境问题。

## 前置条件

1. 本机装 **uv**（提供 `uvx`）；
2. Blender 里装 [MCP for Blender](https://github.com/ahujasid/blender-mcp) 的 `addon.py` 并启用；
3. 在 Blender 侧边栏的 MCP 面板点 **Connect** —— add-on 在 `127.0.0.1:9876` 监听，是 **server 反向连它**，所以顺序不能反；
4. 装本插件并连接。

```bash
magic plugin install ./examples/plugins/06-mcp-blender
```

## 失败长什么样（都是环境问题，不是"连不上"）

| 现象 | 原因 |
|---|---|
| 连接超时 / 进程立刻退出 | `uvx` 不在 PATH，或包下载失败（uv 缓存目录权限） |
| 连上了但工具列表为空 | Blender 没开，或 add-on 没点 Connect |
| 工具调用报错 | add-on 报的错原样透传（通常是场景/选择状态不对） |

PyPI 包名是 `mcp-for-blender`（旧名 `blender-mcp` 仍会转发，但已改名）。
