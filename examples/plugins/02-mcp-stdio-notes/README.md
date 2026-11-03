# 02-mcp-stdio-notes：自带可运行的 stdio MCP server

**演示点**：`mcp.json` 的 stdio 声明；`${PLUGIN_ROOT}` / `${PLUGIN_DATA}` 两个运行时变量；数据目录里持久化；一个只依赖 Python 标准库、可以直接抄走的最小 MCP 协议实现。

## 目录结构

```
02-mcp-stdio-notes/
  plugin.json
  mcp.json                    # server 条目名 notes，工具名会是 ap_notes-mcp-demo_notes_notes_add
  server/notes_server.py      # 最小 stdio MCP server（initialize / tools/list / tools/call / ping）
  skills/notes/SKILL.md       # 教模型怎么用这三个工具
```

## mcp.json 怎么读

```json
{
  "type": "stdio",
  "command": "python",                          // 单个可执行 token，不做占位符展开
  "args": ["${PLUGIN_ROOT}/server/notes_server.py"],  // args 展开变量
  "cwd": "${PLUGIN_ROOT}",                      // 省略时默认就是插件根，这里写出来做演示
  "env": { "NOTES_DB": "${PLUGIN_DATA}/notes.json" }  // env 值展开变量，key 不展开
}
```

- `${PLUGIN_ROOT}`：插件安装目录，**只读**。升级插件时会被整体替换。
- `${PLUGIN_DATA}`：数据目录（`agent-plugins-data/` 下），**可写**，升级、卸载默认都保留。
- 展开是单次的、不递归，且展开结果不允许逃出这两个目录。

## 安装与验证

```bash
magic plugin install ./examples/plugins/02-mcp-stdio-notes   # 需要本机有 python（Windows）或改成 python3
magic plugin list
```

聊天里说 "把「周五前确认灰度名单」记到笔记里"，工具调用会弹审批，确认后返回
`saved note #1 at <PLUGIN_DATA>/notes.json`。

## 单独调试 server

不用走宿主，直接喂 JSON-RPC 帧：

```bash
NOTES_DB=/tmp/notes.json python server/notes_server.py <<'EOF'
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"notes_add","arguments":{"text":"hello"}}}
EOF
```

写 stdio server 的三条铁律（示例代码里都有对应处理）：

1. **stdout 只发协议帧**，一行一个 JSON；调试输出一律走 stderr，否则帧错位。
2. **每帧 flush**，行缓冲不打开宿主会一直等。
3. **notifications/*（含 initialized）不回复**；工具内部错误用 `result.isError`，
   协议层错误（未知方法）才用 JSON-RPC error。

## 接真实服务

把 `command` 换成你的二进制、`args` 换成真实参数即可。需要 API key 时在 `env` 里加
`"API_KEY": ""`（值为空 = 凭据表单里多一个输入框），或把 key 写进
`args` / `headers` 的 `${CRED:API_KEY}` 占位符——见示例 04。
