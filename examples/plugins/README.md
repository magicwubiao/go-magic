# 插件示例（Agent Plugins）

这个目录下每个子目录都是一个**完整、可直接安装**的 Agent Plugin，覆盖插件系统的全部组件类型。做自己的插件时，从最接近需求的示例开始改，比从零读规范快得多。

| 目录 | 演示内容 | 外部依赖 |
|---|---|---|
| [01-skill-only](01-skill-only/) | 纯技能插件：SKILL.md 前言字段、skill 内置脚本、图标、`examples` 问法 | 无 |
| [02-mcp-stdio-notes](02-mcp-stdio-notes/) | 自带一个可运行的 stdio MCP server（纯 Python 标准库），`${PLUGIN_ROOT}` / `${PLUGIN_DATA}` 变量、数据落盘 | Python 3.8+ |
| [03-cli-gh](03-cli-gh/) | cli.json 连接器：参数模板 `{{x}}`、条件参数 `{{?x}}`、stdin 传参、凭据占位、raw 逃生工具 | 本机装有 `gh` |
| [04-mcp-http-remote](04-mcp-http-remote/) | 远程 MCP：streamable-http / sse 两种传输、`${CRED:NAME}` 请求头凭据、OAuth 2.1 免配置授权 | 网络 |
| [05-media-provider](05-media-provider/) | 扩展命名空间：插件给内置 `image_gen` 工具"换后端"，返回 `b64` 由内核落盘 | Python 3.8+ |

## 安装任一示例

```bash
# 开发期直接装目录（推荐，改完重装即可）
magic plugin install ./examples/plugins/02-mcp-stdio-notes

# 分发用 zip（打包时不要把 README 之外的多余文件带进去也行，多余文件会被忽略）
cd examples/plugins/02-mcp-stdio-notes && zip -r ../notes-mcp-demo.zip . && cd -

magic plugin list                # 看状态：技能数 / MCP / CLI
magic plugin remove notes-mcp-demo
```

网页端等价操作：设置 → 插件 → 上传 zip / 启用 / 禁用 / 卸载 / 填凭据 / 重连。

## 最小骨架：30 秒做一个自己的插件

一个只有技能的插件只需要两个文件：

```
my-plugin/
  plugin.json
  skills/my-skill/SKILL.md
```

```json
{
  "$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json",
  "name": "my-plugin",
  "version": "0.1.0",
  "description": "一句话说明它解决什么问题"
}
```

```markdown
---
name: my-skill
description: "Use when ...（触发场景要写具体，模型靠它决定什么时候加载）"
---

# My Skill

这里写希望模型遵循的做法、步骤与边界。
```

装上后技能就会出现在会话里（`/skill my-skill` 或自动触发）。

## 必须知道的约定

- **组件按固定位置发现**：技能在 `skills/<name>/SKILL.md`，MCP 在根目录 `mcp.json`，CLI 连接器在根目录 `cli.json`。位置不对 = 组件不存在。
- **`$schema` 缺失或不支持 = 整个插件被拒**；其它错误都是隔离的：坏一个 skill 只跳过它，坏一条 MCP 只标该条，`cli.json` 损坏只关 CLI 组件。
- **凭据不进配置文件**：`mcp.json` / `cli.json` 里值为空的 env key（如 `"GH_TOKEN="`）和 `${CRED:NAME}` 占位符都会变成插件页的凭据表单，填好的值加密存 `plugin_secrets/`，接口永远回 `***`。
- **路径变量只有两个**：`${PLUGIN_ROOT}`（插件目录，只读）与 `${PLUGIN_DATA}`（数据目录，可写、升级不丢），只在 stdio 的 `args` / `env` 值 / `cwd` 里展开，单次、不递归。
- **工具名带前缀**：MCP 是 `ap_<插件>_<server>_<工具>`，CLI 是 `ap_<插件>_cli_<连接器>_<工具>`。改名插件不会和你机器上已有的同名工具撞车。
- **插件工具一律逐次审批**：MCP 工具与 CLI 工具的 `readOnly` / `read_only` 是作者自声明，不是免审批通行证。示例里的 `read_only: true` 只影响展示语义。
- **policy 照样生效**：开了 `policy.enforce` 时，插件的 command / url 也要过白名单。

## 本地调试技巧

- stdio server 可以单独手跑：示例 02 / 05 的 server 是独立的 Python 脚本，直接往 stdin 里喂 JSON-RPC 帧就能测，不必每次都走宿主。
- `magic plugin list` 的 warnings 列出被跳过的 skill 与被禁用的组件——"工具没出现"先看这里，再看 MCP 条目的 Error。
- 改了插件内容后 `magic plugin install` 重装一次（原地升级，`PLUGIN_DATA` 不丢），或网页端点"重新扫描"。

## 规范

本实现遵循 [Agent Plugins 1.0.0](https://agent-plugins.org) 规范；`cli.json` 与媒体后端扩展（`io.github.magicwubiao.media-provider`）是本实现的可选扩展，其他客户端会安全忽略。完整字段说明见 `docs/USAGE.zh-CN.md` 第 15.4 节。

仓库里的 `internal/agentplugin/examples_test.go` 会把本目录所有示例跑一遍完整校验（清单、技能解析、MCP/CLI 条目、扩展引用），改坏了会在 `go test ./internal/agentplugin/` 里直接红。
