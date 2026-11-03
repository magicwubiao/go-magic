---
name: chat-interrupt
description: "Use when the user wants to interrupt an agent run in the interactive terminal (TUI) to add a new requirement, record a task for later, list or reprioritize pending requirements, or ask where a long-running turn stands."
version: 2.0.0
author: go-magic
license: MIT
tags: [chat, interrupt, requirements, todo, workflow, interactive]
tools: [todo]
---

# Chat 打断机制 & 需求管理

在 `magic` 交互式终端（TUI）中，AI 正在生成回答时，可以随时**打断**它并添加新的需求；需求会被记录下来，在后续对话中按优先级处理。

## 适用范围（重要）

本技能描述的 `/req*` 系列命令**只在 TUI（`magic` 交互式终端）中提供**。

- ✅ TUI：可用 `/req`、`/reqs`、`/req-done`、`/req-del`、`/req-priority`、`/context`、`/stop`、`/interrupt`
- ❌ Web 聊天 / 机器人会话：**没有**这些命令。Web 侧只支持 `/skill <name>` 与 `/mcp <server> <tool>` 两类指令。

在 Web/机器人会话里，请改用自然语言表达（"先记一下，还需要一个邮箱验证功能"），不要提示用户去敲 `/req`。

## 使用场景

| 场景 | 说明 |
|------|------|
| 💡 想到新需求 | AI 正在写代码时，突然想到一个新功能需要加入 |
| 🐛 发现新 Bug | 测试中发现了 Bug，需要优先修复 |
| 🔄 变更需求 | 当前方案不符合实际，需要调整方向 |
| 📝 记录任务 | 不想打断当前对话，先把需求记下来 |

## 快速使用

### 方法 1：打断并直接说（推荐）

AI 正在回答时，按 `Esc`（或输入 `/interrupt`）停止当前生成，然后直接输入新需求：

```
[AI 正在生成回答...]
(Esc)
> 等等，我想到一个新需求：需要添加用户认证功能
```

AI 会把这条需求记入待办列表，并在接下来优先处理。

### 方法 2：用 `/req` 命令记录

```
> /req 添加用户登录页面
> /req --priority high 修复数据库连接超时问题
> /req 需要生成 API 文档
```

### 方法 3：先记录、稍后处理

```
> /req 需要支持导出为 PDF
> 好的，先继续刚才的功能
```

## 命令参考

### 需求管理（仅 TUI）

| 命令 | 说明 | 示例 |
|------|------|------|
| `/req <内容>` | 添加新需求 | `/req 添加用户登录` |
| `/req --priority high <内容>` | 以指定优先级添加 | `/req --priority high 修复超时` |
| `/reqs` | 查看所有待处理需求 | `/reqs` |
| `/req-done <id>` | 标记需求已完成 | `/req-done <id>` |
| `/req-del <id>` | 删除需求 | `/req-del <id>` |
| `/req-priority <id> <级别>` | 设置优先级 | `/req-priority <id> high` |

### 打断与上下文（仅 TUI）

| 命令 | 说明 |
|------|------|
| `Esc` / `/interrupt` | 停止当前生成 / 打断 agent 执行 |
| `/stop` | 停止当前生成 |
| `/context` | 查看当前上下文与累积的需求清单 |

## 典型工作流

```
1. > 帮我写一个用户注册功能
2. [AI 正在生成代码...]
3. Esc → > 等等，再加一个邮箱验证功能
4. AI：记录需求 "添加邮箱验证功能"，并整合进当前工作
5. > 好的继续吧
```

### 优先级

```
🔴 High > 🟡 Medium > 🟢 Low

/req --priority high 生产环境紧急 Bug 修复
/req --priority medium 添加日志功能
/req --priority low 优化代码注释
```

## 需求存储

需求通过 `todo` 工具持久化到：

```
~/.magic/todos/todos.json
```

因此退出会话后仍然保留，`/reqs` 和 `todo` 工具读到的是同一份数据。

## 最佳实践

### ✅ 推荐
- 打断时一句话说清需求要点
- 用 `/req` 快速记录，而不是把需求埋在长描述里
- 完成后及时 `/req-done`，避免清单越积越乱
- 给紧急事项设置 `high` 优先级

### ❌ 不推荐
- 一次打断抛出一大串需求（分批添加）
- 需求完成后不标记（清单失真）
- 在 Web / 机器人会话中提示用户使用 `/req`（那里不存在该命令）
