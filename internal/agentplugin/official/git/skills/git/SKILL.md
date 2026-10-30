---
name: git
description: 使用内置 git 工具查看仓库状态、历史与差异，并在用户确认后提交改动。当用户提到"仓库 / 提交 / commit / diff / 分支"等话题时使用。
version: 1.0.0
tags:
  - git
  - devtools
---

# Git 技能

本技能配套一组 `ap_git_cli_git_*` 工具，直接调用本机 `git` 命令。所有工具的工作目录就是 go-magic 进程的当前目录——如果用户在问"这个仓库"，先假设他们指的是当前目录下的仓库，必要时先用 `status` 确认（不在仓库内时 git 会报错，此时提醒用户切换工作目录或初始化仓库）。

## 工具一览

| 工具 | 对应命令 | 用途 |
| --- | --- | --- |
| `status` | `git status` | 当前分支、已暂存/未暂存/未跟踪文件 |
| `log` | `git log --oneline -n N` | 最近提交历史（count 参数控制条数，默认 20） |
| `diff` | `git diff` | 未暂存的改动 |
| `diff_staged` | `git diff --staged` | 已暂存、即将提交的改动 |
| `branch_list` | `git branch -a -v` | 本地与远程分支 |
| `show` | `git show <revision>` | 单条提交的完整信息与差异 |
| `commit` | `git commit -am <message>` | 提交所有已跟踪文件的改动（**需要用户确认**） |

## 使用习惯

1. **回答前先看状态**。用户问"改了什么 / 现在什么情况"，先跑 `status`，再按需 `diff` 或 `diff_staged`，不要凭空猜。
2. **历史类问题用 `log`**。用户问"最近做了什么"，用 `log` 取 20 条左右，按主题归纳；不要逐条复述。
3. **提交前必须给用户看要提交的内容**：先 `diff`（或 `diff_staged`）总结改动，得到用户认可再调 `commit`。commit 工具走人工确认链路是设计如此，不要试图绕过。
4. `commit` 只覆盖**已跟踪文件**。有未跟踪的新文件时，告诉用户这些文件不会被包含，需要他们先 `git add`。
5. 本技能刻意**不提供** push / reset / rebase 等破坏性或不可逆操作；用户提出时，引导他们在终端里自行执行，并说明风险。
