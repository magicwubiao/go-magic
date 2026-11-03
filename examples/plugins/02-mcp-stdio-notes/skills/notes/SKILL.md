---
name: notes
description: "Use when the user wants to record something for later (a decision, a follow-up, a reminder with context), to look up what was recorded before, or to list recent notes."
version: 1.0.0
author: go-magic
license: MIT
tags: [notes, memory, mcp]
tools: [ap_notes-mcp-demo_notes_notes_add, ap_notes-mcp-demo_notes_notes_search, ap_notes-mcp-demo_notes_notes_list]
---

# Notes

插件自带一个本地笔记 MCP server，数据落在 `PLUGIN_DATA` 里（升级插件不丢）。

## 何时使用

- 用户说"记一下"、"别忘了"、"回头提醒我"，且内容有上下文价值 → `notes_add`
- 用户问"上次那个结论是什么"、"我之前记过什么" → `notes_search`
- 需要看最近记录 → `notes_list`

## 用法约定

1. **写之前先搜**：同一件事已经被记过就补充说明，不要新建重复条目。
2. **一条笔记只表达一件事**：把"决策 + 理由 + 后续动作"写在同一句里，
   而不是记三段。
3. **标签克制**：2-3 个稳定标签（如 `release`、`customer-a`）比一堆一次性的好，
   搜索时标签也参与匹配。
4. **写入需要用户确认**：这类工具每次调用都会弹审批，不要为了"顺手记一下"
   连续触发多次；能合并成一条就合并。
5. 返回里带 `saved note #N at <path>`，需要告诉用户存到哪里时引用这个路径。

## 不要做

- 不要把凭据、token、身份证号等敏感信息写进笔记——它是磁盘上的明文 JSON。
- 不要用它替代项目文档：跨会话需要长期共享的内容应该写进仓库文件。
