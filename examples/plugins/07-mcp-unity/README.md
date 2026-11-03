# 07 · Unity MCP

Unity 侧的本地桥：server 从 PyPI 拉 `mcpforunityserver`，真正操作 Editor 的是装在项目里的 **MCP for Unity** 包。

> **为什么不在内置目录里？**
> 它要求 Unity Editor 开着、且当前项目里装了桥接包——也就是"和 go-magic 同一台机器"。内置到目录里，在服务器部署场景下就是一条点了必然失败的条目。

## 前置条件

1. 本机装 **uv**（提供 `uvx`）；
2. Unity 项目里装 **MCP for Unity** 包：Package Manager → Add package from git URL：
   `https://github.com/CoplayDev/unity-mcp.git?path=/MCPForUnity`（写作时 v10.3.0）；
3. 打开那个项目（Editor 在前台，别是 batchmode）；
4. 装本插件并连接。

```bash
magic plugin install ./examples/plugins/07-mcp-unity
```

## 失败长什么样

| 现象 | 原因 |
|---|---|
| 连接超时 / 进程立刻退出 | `uvx` 不在 PATH，或包下载失败（uv 缓存目录权限） |
| 连上了但工具列表为空 | 项目里没装 MCP for Unity 包，或 Editor 没开 |
| 工具调用报错 | 桥接包透传的 Editor 报错（编译错误时大部分工具都会失败） |

## 版本对不上怎么办

`mcpforunityserver`（server 侧）与项目里的那个包（Editor 侧）来自同一个仓库、按同一版本发布。两侧差太多时工具签名会对不上，报"unknown tool"之类的错——把 `mcp.json` 的 `args` 里 `mcpforunityserver` 换成 `mcpforunityserver==<版本>` 钉死到与包一致的版本即可。
