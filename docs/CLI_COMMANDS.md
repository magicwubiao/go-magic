# Magic CLI 命令完整文档

> 本文档收录了 `magic` 项目的所有命令、子命令、全局标志及使用示例。

- **项目**: [go-magic](https://github.com/magicwubiao/go-magic)
- **构建**: Go 1.26.1
- **License**: MIT

---

## 目录

- [全局标志](#全局标志)
- [命令总览](#命令总览)
- [命令详解](#命令详解)
  - [acp - ACP 连接管理](#acp)
  - [agent - 子代理管理](#agent)
  - [backup - 备份](#backup)
  - [chat - 交互式聊天](#chat)
  - [completion - Shell 补全](#completion)
  - [config - 配置管理](#config)
  - [cron - 定时任务](#cron)
  - [data - 数据迁移](#data)
  - [docs - 文档管理](#docs)
  - [doctor - 诊断](#doctor)
  - [gateway - 消息网关](#gateway)
  - [health - 健康检查](#health)
  - [interactive - REPL 模式](#interactive)
  - [logs - 日志查看](#logs)
  - [mcp - MCP 服务器管理](#mcp)
  - [metrics - 系统指标](#metrics)
  - [model - 模型选择](#model)
  - [plugin - 插件管理](#plugin)
  - [privacy - PII 检测](#privacy)
  - [sessions - 会话管理](#sessions)
  - [setup - 设置向导](#setup)
  - [skills - 技能管理](#skills)
  - [stats - 使用统计](#stats)
  - [status - 系统状态](#status)
  - [tools - 工具管理](#tools)
  - [update - 更新](#update)
  - [validate - 配置验证](#validate)
  - [version - 版本信息](#version)
  - [voice - 语音模式](#voice)

---

## 全局标志

以下标志适用于所有命令：

| 标志 | 简写 | 类型 | 默认值 | 说明 |
|------|------|------|--------|------|
| `--config` | | string | | 配置文件路径 |
| `--debug` | | bool | false | 启用调试模式 |
| `--magic-home` | | string | | 自定义 magic 主目录 |
| `--no-color` | | bool | false | 禁用彩色输出 |
| `--output` | `-o` | string | `text` | 输出格式: `text`, `json`, `yaml`, `table` |
| `--profile` | `-p` | string | | 配置环境 |
| `--verbose` | `-v` | bool | false | 启用详细输出 |
| `--version` | | bool | false | 显示版本信息 |
| `--help` | `-h` | bool | false | 显示帮助 |

### 使用示例

```bash
# 使用 JSON 格式输出
magic --output json config list

# 启用调试模式
magic --debug run "hello world"

# 指定配置文件
magic --config /path/to/config version

# 启用详细日志
magic chat -v

# 使用指定输出格式和配置文件
magic -o json --config ~/.magic/config.json status
```

---

## 命令总览

| 命令 | 分组 | 说明 |
|------|------|------|
| `acp` | 网络/通信 | 管理 ACP 连接，用于智能体间通信 |
| `agent` | 执行 | 管理子代理，支持并行任务执行 |
| `backup` | 维护 | 备份配置和数据 |
| `chat` | 核心 | 启动交互式聊天 |
| `completion` | 工具 | 生成 Shell 补全脚本 |
| `config` | 配置 | 管理配置 |
| `cron` | 任务 | 管理定时任务 |
| `data` | 工具 | 数据迁移工具 |
| `docs` | 工具 | 生成和管理文档 |
| `doctor` | 诊断 | 诊断问题 |
| `gateway` | 网络/通信 | 启动消息网关 |
| `health` | 诊断 | 执行健康检查 |
| `interactive` | 核心 | 启动 REPL 模式 |
| `logs` | 诊断 | 查看日志 |
| `mcp` | 网络/通信 | 管理 MCP 服务器 |
| `metrics` | 诊断 | 显示系统指标 |
| `model` | 配置 | 选择 LLM 提供方和模型 |
| `plugin` | 扩展 | 管理插件 |
| `privacy` | 工具 | PII 检测和脱敏 |
| `sessions` | 数据 | 管理聊天会话 |
| `setup` | 配置 | 运行设置向导 |
| `skills` | 核心 | 管理技能 |
| `stats` | 诊断 | 显示使用统计 |
| `status` | 诊断 | 显示系统状态 |
| `tools` | 配置 | 管理工具 |
| `update` | 维护 | 更新到最新版本 |
| `validate` | 配置 | 验证配置文件 |
| `version` | 信息 | 显示版本信息 |
| `voice` | 核心 | 语音交互模式 |

---

## 命令详解

### acp

**功能**: 管理 ACP (Agent Communication Protocol) 连接，实现智能体间的发现和技能调用。

**用法**: `magic acp <command>`

**子命令**:

| 子命令 | 说明 |
|--------|------|
| `call` | 调用已连接智能体的技能 |
| `connect` | 连接到 ACP 智能体 |
| `disconnect` | 断开 ACP 连接 |
| `list` | 列出所有已连接的 ACP 智能体 |
| `ping` | 检查 ACP 智能体连通性 |
| `serve` | 启动 ACP 服务器 |
| `skills` | 列出已连接智能体的技能 |

**示例**:
```bash
magic acp list
magic acp connect ws://agent-host:8080/acp
magic acp skills
magic acp call agent-name skill-name --args '{"key": "value"}'
magic acp ping
magic acp disconnect agent-name
magic acp serve --port 9090
```

---

### agent

**功能**: 管理子代理，支持在隔离环境中并行执行任务。

**用法**: `magic agent <command>`

**子命令**:

| 子命令 | 说明 |
|--------|------|
| `spawn` | 生成新的子代理执行任务 |
| `list` | 列出所有活跃子代理 |
| `kill` | 终止一个子代理 |
| `stats` | 显示子代理统计信息 |

**示例**:
```bash
magic agent spawn "分析日志文件"
magic agent list
magic agent kill subagent-123
magic agent stats
```

---

### backup

**功能**: 创建 Magic 数据和配置的完整备份。

**用法**: `magic backup [flags]`

**备份内容**:
- 配置文件 (`config.json`)
- 技能 (`skills/`)
- 会话数据库 (`sessions.db`)
- 日志文件 (`logs/`)
- SOUL.md, MEMORY.md, USER.md

**备份位置**: `~/.magic_backup_YYYYMMDD_HHMMSS/`

**示例**:
```bash
magic backup
```

---

### chat

**功能**: 启动与 Magic Agent 的交互式聊天会话。

**用法**: `magic chat [flags]`

**特性**:
- 流式输出
- 斜杠命令 (`/help` 查看所有命令)
- 技能加载
- 会话持久化

**标志**:

| 标志 | 简写 | 默认值 | 说明 |
|------|------|--------|------|
| `--stream` | `-s` | `true` | 启用流式输出 |
| `--no-stream` | `-n` | `false` | 禁用流式输出 |
| `--legacy` | `-l` | `false` | 使用旧版 REPL 模式 |

**示例**:
```bash
magic chat
magic chat -v              # 详细日志模式
magic chat --no-stream     # 禁用流式输出
magic chat --legacy        # 使用旧版模式
```

---

### completion

**功能**: 生成 Shell 补全脚本。

**用法**: `magic completion [bash|zsh|fish|powershell]`

**支持的 Shell**: `bash`, `zsh`, `fish`, `powershell`

**Bash**:
```bash
source <(magic completion bash)
magic completion bash > /etc/bash_completion.d/magic
```

**Zsh**:
```bash
echo "autoload -U compinit; compinit" >> ~/.zshrc
magic completion zsh > "${fpath[1]}/_magic"
```

**Fish**:
```bash
magic completion fish | source
magic completion fish > ~/.config/fish/completions/magic.fish
```

**PowerShell**:
```powershell
magic completion powershell | Out-String | Invoke-Expression
magic completion powershell > magic.ps1
```

**自动安装**:
```bash
magic completion install
```

---

### config

**功能**: 管理 Magic 配置。

**用法**: `magic config <command>`

**子命令**:

| 子命令 | 说明 |
|--------|------|
| `list` | 列出所有配置 |
| `get <key>` | 获取配置值 |
| `set <key> <value>` | 设置配置值 |
| `path` | 显示配置文件路径 |
| `validate` | 验证当前配置 |
| `reset` | 重置配置为默认值 |

**示例**:
```bash
magic config list
magic config get provider
magic config set provider deepseek
magic config set providers.deepseek.api_key sk-xxx
magic config path
magic config validate
magic config reset
```

---

### cron

**功能**: 管理定时任务。

**用法**: `magic cron <command>`

**子命令**:

| 子命令 | 说明 |
|--------|------|
| `add` | 添加新的定时任务 |
| `list` | 列出所有定时任务 |
| `remove` | 删除定时任务 |
| `edit` | 编辑定时任务 |
| `toggle` | 启用/禁用定时任务 |
| `test` | 立即测试运行定时任务 |

**示例**:
```bash
magic cron add --schedule "30 9 * * *" --prompt "每日报告"
magic cron list
magic cron edit job-123
magic cron toggle job-123
magic cron remove job-123
magic cron test job-123
```

---

### data

**功能**: 数据迁移工具。

**用法**: `magic data <command>`

**子命令**:

| 子命令 | 说明 |
|--------|------|
| `migrate` | 从 OpenClaw 或 Hermes Agent 迁移设置 |

**示例**:
```bash
magic data migrate
```

---

### docs

**功能**: 生成和管理文档。

**用法**: `magic docs [generate|serve|man]`

**子命令**:

| 子命令 | 说明 |
|--------|------|
| `generate` | 生成 Markdown 文档 |
| `serve` | 启动本地文档服务器 |
| `man` | 生成 Unix man page |

**标志**:

| 标志 | 默认值 | 说明 |
|------|--------|------|
| `--output` | `./docs` | 文档输出目录 |
| `--format` | `markdown` | 输出格式: `markdown`, `html`, `man` |
| `--port` | `8080` | 文档服务器端口 |
| `--all` | `false` | 包含隐藏命令 |

**示例**:
```bash
magic docs generate
magic docs generate --format html -o ./site
magic docs serve --port 3000
magic docs man
```

---

### doctor

**功能**: 诊断 Magic Agent 设置和配置问题。

**用法**: `magic doctor [flags]`

**示例**:
```bash
magic doctor
```

---

### gateway

**功能**: 启动消息网关，支持 Telegram、Discord、企业微信等多平台。

**用法**: `magic gateway <command>`

**子命令**:

| 子命令 | 说明 |
|--------|------|
| `start` | 启动网关 |
| `stop` | 停止网关 |
| `status` | 查看网关状态 |

**健康检查**: `http://localhost:8080/health`

**支持的平台**:
- Telegram
- Discord
- 企业微信 (WeCom)
- QQ
- DingTalk (钉钉)
- Feishu (飞书)
- WeChat (微信)

**示例**:
```bash
magic gateway start
magic gateway status
magic gateway stop
```

---

### health

**功能**: 执行健康检查，验证所有系统组件是否正常运行。

**用法**: `magic health [flags]`

**示例**:
```bash
magic health
```

---

### interactive

**功能**: 启动交互式 REPL (Read-Eval-Print Loop) 模式。

**用法**: `magic interactive [prompt] [flags]`

**特性**:
- 方向键历史记录
- 自动补全
- 多行支持
- 特殊命令

**特殊命令**:

| 命令 | 说明 |
|------|------|
| `/help` | 显示帮助 |
| `/exit` / `/quit` | 退出 REPL |
| `/clear` | 清屏 |
| `/history` | 显示命令历史 |
| `/save <file>` | 保存会话到文件 |
| `/load <file>` | 从文件加载命令 |
| `/env` | 显示环境变量 |
| `/vars` | 显示已定义变量 |
| `/reset` | 重置会话 |

**标志**:

| 标志 | 默认值 | 说明 |
|------|--------|------|
| `--prompt` | `magic> ` | REPL 提示符 |
| `--history-size` | `1000` | 最大历史记录数 |
| `--multiline` | `false` | 启用多行模式 |

**示例**:
```bash
magic interactive
magic interactive "my> "
magic interactive --multiline --prompt ">> "
```

---

### logs

**功能**: 查看和管理 Magic Agent 日志文件。

**用法**: `magic logs <command>`

**子命令**:

| 子命令 | 说明 |
|--------|------|
| `list` | 列出所有日志文件 |
| `show` | 显示日志文件内容 |
| `latest` | 显示最新的日志文件 |

**示例**:
```bash
magic logs list
magic logs show magic_2026-04-28_14-42-28.log
magic logs latest
```

---

### mcp

**功能**: 管理 MCP (Model Context Protocol) 服务器，扩展工具能力。

**用法**: `magic mcp <command>`

**支持的传输协议**: stdio, SSE

**子命令**:

| 子命令 | 说明 |
|--------|------|
| `add` | 添加 MCP 服务器配置 |
| `list` | 列出所有已连接 MCP 服务器 |
| `connect` | 连接到 MCP 服务器 |
| `disconnect` | 断开 MCP 服务器 |
| `health` | 检查 MCP 服务器健康状态 |

**示例**:
```bash
magic mcp add my-server --command "npx @modelcontextprotocol/server-filesystem"
magic mcp list
magic mcp connect my-server
magic mcp health
magic mcp disconnect my-server
```

---

### metrics

**功能**: 显示当前系统指标和统计信息。

**用法**: `magic metrics [flags]`

**示例**:
```bash
magic metrics
```

---

### model

**功能**: 选择或查看 LLM 提供方和模型。

**用法**: `magic model [provider:model] [flags]`

**支持的提供方**:

| 提供方 | 说明 |
|--------|------|
| `openai` | OpenAI |
| `anthropic` | Anthropic (Claude) |
| `deepseek` | DeepSeek |
| `huoshan` | 火山引擎 |
| `kimi` | Kimi (月之暗面) |
| `minimax` | MiniMax |
| `ollama` | Ollama (本地) |
| `dashscope` | 阿里云 DashScope |
| `vllm` | vLLM |
| `zhipu` | 智谱 AI |
| `openrouter` | OpenRouter |
| `gemini` | Google Gemini |
| `groq` | Groq |
| `together` | Together AI |
| `mistral` | Mistral AI |
| `cohere` | Cohere |
| `perplexity` | Perplexity |
| `doubao` | 豆包 |
| `wenxin` | 百度文心 |
| `moonshot` | Moonshot |
| `mimo` | Mimo |
| `hunyuan` | 腾讯混元 |

**标志**:

| 标志 | 简写 | 说明 |
|------|------|------|
| `--list <provider>` | `-l` | 列出某提供方的可用模型 |

**示例**:
```bash
magic model                           # 查看当前模型
magic model gpt-4o                   # 设置当前提供方的模型
magic model deepseek:deepseek-chat   # 设置提供方和模型
magic model huoshan:ep-20250105-xxxxx
magic model --list openai            # 列出 OpenAI 可用模型
```

---

### plugin

**功能**: 管理 Magic 插件，扩展工具和能力。

**用法**: `magic plugin <command>`

**加载路径**: `~/.magic/plugins/`

**子命令**:

| 子命令 | 说明 |
|--------|------|
| `list` | 列出已加载的插件 |
| `discover` | 发现可用插件 |
| `load <path>` | 加载插件 |
| `unload <name>` | 卸载插件 |

**示例**:
```bash
magic plugin list
magic plugin discover
magic plugin load /path/to/plugin.so
magic plugin unload my-plugin
```

---

### privacy

**功能**: PII (个人可识别信息) 检测和脱敏工具。

**用法**: `magic privacy <command>`

**检测类型**: 手机号、邮箱、身份证号、银行卡号、IP 地址等

**子命令**:

| 子命令 | 说明 |
|--------|------|
| `detect` | 检测文本中的 PII 但不脱敏 |
| `redact` | 脱敏文本中的 PII |
| `audit` | 显示 PII 脱敏审计日志 |
| `stats` | 显示 PII 统计信息 |

**示例**:
```bash
magic privacy detect "我的手机是13800138000"
magic privacy redact "我的邮箱是user@example.com"
magic privacy audit
magic privacy stats
```

---

### sessions

**功能**: 管理聊天会话。

**用法**: `magic sessions <command>`

**子命令**:

| 子命令 | 说明 |
|--------|------|
| `list` | 列出所有会话 |
| `show <id>` | 显示会话详情 |
| `delete <id>` | 删除会话 |

**示例**:
```bash
magic sessions list
magic sessions show session-123
magic sessions delete session-123
```

---

### setup

**功能**: 运行完整的交互式设置向导，一次性完成所有配置。

**用法**: `magic setup [flags]`

**示例**:
```bash
magic setup
```

---

### skills

**功能**: 管理 Magic Agent 的技能。

**用法**: `magic skills <command>`

**技能加载层级**:
1. **内置技能**: 随应用打包
2. **全局技能**: `~/.magic/skills/`
3. **工作区技能**: `./skills/` 或 `.magic/skills/`

**支持格式**: SKILL.md (推荐), JSON, Markdown, Text, 目录含 manifest.json

**子命令**:

| 子命令 | 说明 |
|--------|------|
| `list` | 列出已安装技能 |
| `show <name>` | 显示技能详情 |
| `search <keyword>` | 搜索技能 |
| `install <name>` | 安装技能 |
| `create <name>` | 从模板创建新技能 |
| `delete <name>` | 删除技能 |
| `match <input>` | 查找匹配输入的最佳技能 |
| `import <source>` | 从本地路径或远程 URL 导入技能 |
| `migrate` | 从 OpenClaw 迁移到 Hermes Agent 格式 |

**示例**:
```bash
magic skills list
magic skills show code-quality
magic skills search "documentation"
magic skills install docs-writing
magic skills create my-custom-skill
magic skills match "帮我写个Python脚本"
magic skills import ./my-skill.md
magic skills import https://example.com/skills/my-skill.md
magic skills delete old-skill
magic skills migrate
```

---

### stats

**功能**: 显示 Magic 使用统计信息。

**用法**: `magic stats [flags]`

**显示内容**:
- 配置信息 (环境、提供方、模型)
- 已加载的技能数量
- 会话数据库状态
- 日志文件数量
- 工具启用/禁用状态

**示例**:
```bash
magic stats
```

---

### status

**功能**: 显示全面的系统状态和诊断信息。

**用法**: `magic status [flags]`

**显示内容**:
- 配置状态
- 已加载的技能和插件
- 可用工具
- 系统资源
- 健康检查

**示例**:
```bash
magic status
```

---

### tools

**功能**: 配置和管理工具。

**用法**: `magic tools <command>`

**工具分类**:

| 分类 | 工具 |
|------|------|
| **文件操作** | `read_file`, `write_file`, `file_edit`, `list_files`, `directory_tree`, `search_in_files` |
| **Web 工具** | `web_search`, `web_extract` |
| **代码执行** | `python_execute`, `node_execute`, `execute_command` |
| **记忆工具** | `memory_store`, `memory_recall` |
| **任务管理** | `todo` |
| **AI 能力** | `clarify`, `vision_analyze`, `image_gen`, `tts` |
| **技能** | `skill` |

**子命令**:

| 子命令 | 说明 |
|--------|------|
| `list` | 列出所有可用工具 |
| `show <name>` | 显示工具详情 |
| `enable <name>` | 启用工具 |
| `disable <name>` | 禁用工具 |
| `search <keyword>` | 搜索工具 |
| `schema` | 以 OpenAI 格式显示工具 schema |

**示例**:
```bash
magic tools list
magic tools list --prefix file        # 按前缀筛选
magic tools show read_file
magic tools show read_file --schema   # 显示 schema
magic tools enable browser_navigate
magic tools disable web_search
magic tools search "file"
magic tools schema
```

---

### update

**功能**: 更新 Magic 到最新版本。

**用法**: `magic update [flags]`

**示例**:
```bash
magic update
```

---

### validate

**功能**: 验证配置文件。

**用法**: `magic validate [flags]`

**示例**:
```bash
magic validate
```

---

### version

**功能**: 显示版本信息。

**用法**: `magic version [flags]`

**示例**:
```bash
magic version
```

---

### voice

**功能**: 语音交互模式，支持语音转文字和文字转语音。

**用法**: `magic voice <command>`

**子命令**:

| 子命令 | 说明 |
|--------|------|
| `listen` | 启动语音模式 (Push-to-Talk) |
| `speak` | 将文本转换为语音 |
| `test` | 测试语音配置 |

**示例**:
```bash
magic voice listen
magic voice speak "Hello, this is Magic Agent"
magic voice test
```

---

## 快速参考

### 日常使用

```bash
magic chat                    # 开始聊天
magic config set provider ... # 配置提供方
magic model gpt-4o           # 选择模型
magic skills list            # 查看技能
magic tools list             # 查看工具
```

### 系统管理

```bash
magic status                  # 查看系统状态
magic health                  # 健康检查
magic doctor                  # 诊断问题
magic stats                   # 使用统计
magic backup                  # 备份数据
magic update                  # 更新版本
```

### 配置管理

```bash
magic setup                   # 设置向导
magic config list             # 查看配置
magic config set key value    # 修改配置
magic config validate         # 验证配置
magic config path             # 查看配置文件位置
```

### 技能与扩展

```bash
magic skills list             # 列出技能
magic skills search keyword   # 搜索技能
magic skills install name     # 安装技能
magic skills create name      # 创建技能
magic plugin list             # 列出插件
magic plugin discover         # 发现插件
```

### 工具管理

```bash
magic tools list              # 列出所有工具
magic tools show <name>       # 查看工具详情
magic tools enable <name>     # 启用工具
magic tools disable <name>    # 禁用工具
```

---

## 常见模式

### 结合全局标志

```bash
# JSON 格式输出
magic -o json status
magic --output json config list

# 调试模式
magic --debug chat

# 指定配置文件
magic --config /custom/path/config.json chat

# 多标志组合
magic -v -o json --config ~/.magic/config.json stats
```

### 管道使用

```bash
# 配合 jq 处理 JSON 输出
magic -o json config list | jq '.provider'

# 配合 grep 筛选
magic tools list | grep "file"

# 重定向到文件
magic -o json status > status.json
```
