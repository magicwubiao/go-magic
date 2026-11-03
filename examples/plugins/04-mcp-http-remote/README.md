# 04-mcp-http-remote：远程 MCP（streamable-http / sse / 凭据 / OAuth）

**演示点**：不需要本地进程的远程 MCP 接入；两种传输类型；`${CRED:NAME}` 请求头凭据；OAuth 2.1 免配置授权（动态客户端注册）。

## mcp.json 的三个条目

| 条目 | 演示 | 能否直接连上 |
|---|---|---|
| `deepwiki` | streamable-http + 公开服务，无需凭据 | ✅ 可以，装完点重连即可用 |
| `private-docs` | `Authorization: Bearer ${CRED:DOCS_TOKEN}` 请求头凭据 | ❌ 占位地址，装上后先填凭据、再把 url 换成你的服务 |
| `issues` | sse 传输 + `oauth`（只声明 scopes，client_id 留空走动态注册） | ❌ 占位地址，作模板用 |

## 关键规则

- **公网必须 HTTPS**：`streamable-http` / `sse` 的 url 若是公网主机，明文 http 会被静态校验拒绝
  （localhost / 内网 IP / `*.local` 等豁免——为了照顾没有证书的家用 NAS / 内网服务）。
- **headers 是字面包**：凭据不要明文写进 headers，用 `${CRED:NAME}`；未填写的凭据会在
  连接时以 auth 类错误中止，而不是带着占位符字面量去请求再把 401 抛给你。
- **凭据表单自动生成**：`${CRED:DOCS_TOKEN}` 会让插件页出现 DOCS_TOKEN 输入框，
  填好的值加密存 `plugin_secrets/`，接口只回 `***`。
- **OAuth 免配置路径**：远程条目声明了 `oauth.scopes`（client_id 留空）→ 连接前自动做
  授权服务器发现 + 动态客户端注册（DCR）+ PKCE + 本机回调，token 存 0600，过期自动刷新。
  服务端支持 DCR 时**用户只需要点一次"授权"**。
- **OAuth 只允许远程条目**：stdio 条目声明 `oauth` 会被整条拒绝。

## 安装与验证

```bash
magic plugin install ./examples/plugins/04-mcp-http-remote
magic plugin list          # deepwiki 应显示 connected + 工具数
```

聊天里问 "用 deepwiki 查一下 facebook/react 的架构" 试公开条目；
`private-docs` / `issues` 条目在插件页填完凭据 / 点授权后再把 URL 换成你自己的服务。

## 改造成你自己的服务

1. 把 `url` 换成你的 MCP 端点（streamable-http 或 sse 任选，前者是当前主流）。
2. 鉴权二选一：静态 token（`${CRED:NAME}`）或 OAuth 2.1（`oauth` 段，需要你的授权服务器
   支持发现文档；支持 DCR 就连 client_id 都不用写）。
3. 如果你的服务有内网部署形态，url 允许 `http://192.168.x.x:port` 这类私网地址。
