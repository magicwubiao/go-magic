---
name: pr-review
description: "Use when reviewing a pull request or a local diff before merge, when checking whether a change respects the project's conventions, or when a reviewer needs a structured verdict instead of a list of nitpicks."
version: 1.0.0
author: go-magic
license: MIT
tags: [code-review, git, quality]
tools: [execute_command, read_file, search_in_files]
---

# PR Review

按"能不能合、合之前必须改什么"的顺序审，而不是从头到尾挑毛病。

## 步骤

1. **先看范围**：`git diff --stat <base>...HEAD`，控制在 400 行以内；
   超过就按文件分轮，不要一次性把所有 diff 塞进上下文。
2. **先看意图再看实现**：读 PR 描述与首个提交，写下"这个改动打算做什么"。
   实现与意图不一致本身就是最重要的问题。
3. **按四个维度检查**（顺序不能反）：
   - **正确性**：边界条件、错误分支、并发/锁序、资源释放
   - **兼容性**：对外接口、配置字段、落盘格式、默认值是否变化
   - **可运维性**：失败时日志是否可定位、错误是否被吞、超时是否配了
   - **风格**：命名、注释、重复代码——放最后，且只提能落地的点
4. **每条结论都要能验证**：给出文件:行号，或给出一条能复现的命令。
   无法验证的猜测写成"待确认"，不要写成结论。

## 输出格式

```markdown
## 结论：需要修改 / 可以合入 / 建议驳回

### 阻塞项（必须改）
1. `internal/foo/bar.go:88` 落盘失败被忽略，用户会看到"成功"但文件没写 → 需返回错误

### 建议（可后续处理）
1. `web/src/views/X.vue:120` 300px 固定宽度在窄屏会被裁，建议 `max-width: calc(100vw - 56px)`

### 待确认
- 这个字段改名后，旧配置文件是否还需要兼容？
```

## 边界

- 不评论作者本人，只评论代码与影响。
- 风格问题如果不能落到具体改法，不要提。
- 不为了"显得仔细"而凑条数：三个真问题比二十条格式意见有用。
- 涉及安全（命令拼接、路径逃逸、凭据落盘）的问题一律升级为阻塞项。
