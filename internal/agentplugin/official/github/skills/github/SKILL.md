---
name: github
description: 使用内置 gh 工具查看 GitHub 仓库、PR 与 Issue，并在用户确认后创建它们。当用户提到 GitHub、某个仓库、PR 或 issue 时使用。
version: 1.0.0
tags:
  - github
  - devtools
---

# GitHub 技能

本技能配套一组 `ap_github_cli_gh_*` 工具，调用本机 `gh`（GitHub 官方 CLI）。

## 授权

工具能跑起来不代表已登录。认证按顺序取自：

1. 用户在插件"授权"表单里填的 **GH_TOKEN**（Personal Access Token，classic 或 fine-grained 均可）；
2. 本机已有的 `gh auth login` 登录态。

两者都没有时，读公开仓库的只读工具也可能够用（gh 支持匿名拉部分公开数据，但限流严格）；一旦报 401/403 或 "gh auth login" 提示，就引导用户去插件页点"授权"填 token，或在终端执行 `gh auth login`。**不要替用户跑交互式登录命令。**

## 工具一览

| 工具 | 对应命令 | 审批 |
| --- | --- | --- |
| `repo_view` | `gh repo view OWNER/REPO` | 免确认 |
| `pr_list` | `gh pr list -R OWNER/REPO --limit N` | 免确认 |
| `issue_list` | `gh issue list -R OWNER/REPO --limit N` | 免确认 |
| `pr_create` | `gh pr create -R OWNER/REPO --title … --body …` | **需确认** |
| `issue_create` | `gh issue create -R OWNER/REPO --title … --body …` | **需确认** |

## 使用要点

1. `repo` 参数统一写 `OWNER/REPO`（如 `microsoft/vscode`）。用户说"我们仓库"而没给名字时，先用 `status`/`log` 之类本地工具确认当前目录的 remote，再向用户确认。
2. 汇报列表结果时按状态分组提炼（open/closed/draft），别整列复读。
3. `pr_create` / `issue_create` 调用前，把将要提交的标题和正文完整展示给用户过目——审批弹窗里用户只看一眼，你的复述才是他判断的依据。
4. 本技能只暴露读和"创建"，刻意不含 merge / close / delete 等不可逆操作；用户提出时引导他们在 GitHub 上或终端里自行处理。
