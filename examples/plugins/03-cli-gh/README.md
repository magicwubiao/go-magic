# 03-cli-gh：用 cli.json 把既有 CLI 包成工具

**演示点**：cli.json 的完整写法——参数模板、条件参数、stdin 传参、凭据占位、超时与输出上限、raw 逃生工具。`cli.json` 是本实现在规范之外的可选组件（其他客户端会忽略），结构与 `config.json` 的 `cli.connectors` 完全一致。

## 目录结构

```
03-cli-gh/
  plugin.json
  cli.json        # 连接器 gh：3 个声明工具 + raw
```

**前置条件**：本机装有 [gh](https://cli.github.com) 且已登录（或在插件凭据表单里填 `GH_TOKEN`）。

## cli.json 里每个演示点对应的位置

| 特性 | 位置 |
|---|---|
| 凭据占位 | `"env": ["GH_TOKEN="]` —— 值为空 = 凭据表单多一个输入框，填好后注入子进程环境，`gh` 自己会读 |
| 参数模板 | `"--limit", "{{limit}}"` —— `{{x}}` 替换成参数值；模板里出现的名字必须在 `params` 里声明 |
| 条件参数 | `"{{?json}}--json"` —— 参数 `json` 为真时保留整条 arg，为假时整条丢弃 |
| 枚举与默认值 | `pr_list` 的 `state`（enum）与 `limit`（default 20） |
| stdin 传参 | `pr_comment` 的 `"stdin_param": "body"` + `--body-file -` —— 评论正文走 stdin，不出现在进程列表里 |
| 超时 | `"timeout_seconds": 30`（连接器级默认，工具级可覆盖；上限 300） |
| 输出上限 | `"max_output_bytes": 65536` —— 单次回传给模型的字节上限，防止把上下文吃穿 |
| raw 逃生工具 | `"raw": true` —— 额外注册 `ap_gh-cli-demo_cli_gh_raw`，模型可对该二进制执行**任意**子命令（args 直传，不经 shell） |

## 安装与验证

```bash
magic plugin install ./examples/plugins/03-cli-gh
magic plugin list
```

聊天里问 "列一下待审的 PR" → 模型调用 `ap_gh-cli-demo_cli_gh_pr_list`。
所有插件 CLI 工具**每次调用都要人工确认**（`read_only: true` 只影响展示语义，
不构成免审批依据——声明是插件作者写的，不是信任根）。

## 什么时候用 CLI 而不是 MCP

- 目标软件**只有命令行接口**、没有 MCP server：CLI 是唯一通路。
- 想暴露的子命令不多于 5 个：写 cli.json 比起实现/引入一个 MCP server 便宜得多。
- 长尾命令太多不想逐条声明：`raw: true` 一个工具兜底，审批兜住风险。

反过来，需要**流式/长连接、资源订阅、复杂 schema** 的服务，用 mcp.json（示例 02 / 04）。

## 安全注意

- 三条硬约束在宿主侧执行：不经 shell（argv 直传，`;` `&&` 不会被解释）、
  参数只出现在声明过的位置、超时封顶 300s。
- `policy.enforce=true` 时，连接器的 `command` 也要过 `cli_allowed_commands`，
  被拒的连接器会在摘要里写明 `blocked by policy`，不会静默消失。
- 能用 header 或 stdin 传的凭据，优先用那两种；`{{env:NAME}}` 展开后会进 argv，
  同机其他用户可通过进程列表看到。
