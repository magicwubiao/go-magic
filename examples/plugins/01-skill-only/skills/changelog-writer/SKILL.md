---
name: changelog-writer
description: "Use when a release is being prepared and CHANGELOG.md must be written or refreshed from git history, when commits need grouping into Features / Fixes / Breaking Changes, or when release notes have to be phrased for end users rather than for reviewers."
version: 1.0.0
author: go-magic
license: MIT
tags: [changelog, release, git, writing]
tools: [execute_command, read_file, write_file]
---

# Changelog Writer

把一段 git 历史整理成给**使用者**看的 CHANGELOG，而不是把 commit 标题原样搬运。

## 何时使用

- 准备发版，需要写/刷新 `CHANGELOG.md`
- 需要把杂乱的 commit 归并成 Features / Fixes / Breaking Changes
- 需要判断"这个提交到底算不算破坏性变更"

## 步骤

1. **先取范围，不要全量重写**。默认取"上一个 tag 到 HEAD"：

   ```bash
   bash scripts/collect-commits.sh v1.2.0 HEAD
   ```

   脚本输出 `hash<TAB>subject`，一行一条，已按时间正序排列。

2. **分类，不按目录按影响面**：
   - `feat` / 新增能力 → Features
   - `fix` / 行为纠正 → Fixes
   - 配置项改名、默认值变化、工具/接口删除、错误码语义变化 → **Breaking Changes（宁可多标）**
   - `chore` / `ci` / `test` / 仅内部重构 → 不写进 CHANGELOG

3. **改写措辞**。每一条都要回答"用户会感知到什么变化"：
   - 差：`fix: nil pointer in ctx usage popover`
   - 好：`修复移动端上下文用量浮层右侧数值被裁切的问题`

4. **保留可追溯性**。每条末尾追加快捷 hash（7 位），不要贴完整 commit 正文。

5. **写入文件**：新版本块插在 `## [Unreleased]` 之后、旧版本之前；不要动历史条目。
   写完回读一次文件头 20 行确认缩进与标题层级没被破坏。

## 格式

```markdown
## [1.3.0] - 2026-11-03

### Breaking Changes
- `cli.json` 的 `timeout_seconds` 上限从 600 降为 300，超出部分会被截断 (a1b2c3d)

### Features
- 插件示例目录 `examples/plugins/`，覆盖技能 / MCP / CLI 三类组件 (e4f5a6b)

### Fixes
- 移动端上下文用量浮层右侧数值被裁切 (c7d8e9f)
```

## 边界

- 不修改作者署名、版本号、发布日期——这些由发版流程决定。
- 拿不准某条是否破坏兼容时，写进 Breaking Changes 并在描述里说明"若未使用 X 则无影响"。
- 发现同一需求被拆成 5 个 `fixup!` 提交时，合并成一条，不要列 5 行。
