# 04-mcp-deepwiki：最简远程 MCP（streamable-http）

**演示点**：`mcp.json` 的 `streamable-http` 远程声明；只有两个文件的极简插件（无技能、无本地进程、无凭据）；`plugin.json` 的 `extensions` 目录来源声明。

[DeepWiki](https://deepwiki.com) 是公开服务：对任意公开 GitHub 仓库提问、读仓库 wiki，不需要注册、不需要 API key，是演示"云端 MCP 怎么接"最干净的例子。

## 目录结构

```
04-mcp-deepwiki/
  plugin.json
  mcp.json                    # server 条目名 deepwiki，工具名会是 ap_mcp-deepwiki_deepwiki_<工具>
```

## mcp.json 怎么读

```json
{
  "type": "streamable-http",              // 远程传输；"sse" 也是合法取值
  "url": "https://mcp.deepwiki.com/mcp"   // 公网主机必须是 HTTPS（本机/局域网可用 http）
}
```

没有 `command` / `args` / `env`——远程条目不需要起本地进程，装完插件后由宿主直接对 URL 发起 MCP 会话。超时、重试由宿主统一管（远程读大仓库可能要几十秒，宿主对慢工具有专门的耐心阈值）。

## plugin.json 里的 extensions

```json
"extensions": {
  "io.github.magicwubiao.catalog": { "id": "deepwiki", "kind": "mcp" }
}
```

这是**纯元数据**，只回答"这个插件是从目录里的哪一条装出来的"：目录安装会把 `pkg/connectors` 的内置条目物化成插件目录，`id` / `kind` 与内置条目对应。客户端不消费它，其他客户端也会安全忽略——自己写的插件可以完全不带这段。

## 安装与验证

```bash
magic plugin install ./examples/plugins/04-mcp-deepwiki
magic plugin list
```

聊天里问 "用 deepwiki 介绍一下 facebook/react 的架构"，工具调用会弹审批，确认后返回仓库 wiki 的回答。需要外网可达 `mcp.deepwiki.com`。

## 想接需要鉴权的远程 MCP？

把这份 `mcp.json` 复制过去改 `url`，再加两样东西（见 `docs/USAGE.zh-CN.md` 15.4 节）：

- **请求头凭据**：加 `"headers": {"Authorization": "Bearer ${CRED:MY_TOKEN}"}`，`${CRED:NAME}` 会变成插件页凭据表单里的一个输入框，值加密存储、接口只回 `***`；
- **OAuth 2.1**：在对应条目加 `"oauth": true`，插件页会出现"登录"入口，token 由宿主代管。

`type` 换成 `"sse"` 即接 SSE 传输的 server，其余字段不变。
