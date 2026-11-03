# 01-skill-only：纯技能插件

**演示点**：不含 MCP 与 CLI 时插件可以有多小；SKILL.md 前言各字段的写法；skill 目录里的 `scripts/` 怎么被引用；`icon` 与 `examples` 字段在 UI 上的作用。

## 目录结构

```
01-skill-only/
  plugin.json                       # 清单：$schema + name 必须，其余可选
  assets/icon.svg                   # 清单 icon 指向它（png/jpg/gif/webp/svg 都行）
  skills/
    changelog-writer/
      SKILL.md                      # 前言：name / description / version / tags / tools
      scripts/collect-commits.sh    # skill 自带脚本，SKILL.md 里相对引用
    pr-review/
      SKILL.md                      # 一个插件可以带多个 skill
```

## 安装与验证

```bash
magic plugin install ./examples/plugins/01-skill-only
magic plugin list        # skills: 2
```

聊天里问 "帮我把这周的提交整理成 CHANGELOG"，模型会按 changelog-writer 的步骤走；
`/skill changelog-writer` 可以显式加载。

## 写 SKILL.md 的要点

- `description` 决定**什么时候被加载**，写场景不写功能："Use when preparing a release
  and CHANGELOG.md must be written…" 优于 "Writes changelogs"。
- `tools` 列出这个技能希望用到的既有工具名，只是提示，不注册新工具。
- 正文写**步骤与边界**：什么时候做、按什么顺序、什么时候停下来问用户。
  示例里刻意包含了"不要做"清单——边界比技巧更能约束模型行为。
- 需要 shell 脚本就放进 skill 目录的 `scripts/`（只列目录名，脚本本体可执行即可），
  SKILL.md 里给出确切的调用命令。

## 改造成你自己的插件

1. 复制目录，改 `plugin.json` 的 `name`（小写字母/数字/连字符/点，1-64 字符）。
2. 每个 `skills/<目录名>/` 就是一个技能，目录名 = 技能名。
3. 不需要的 skill 直接删目录，`skills/` 本身缺失也不报错。
