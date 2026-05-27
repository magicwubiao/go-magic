# Go Magic (Magic Agent) v0.3.1

高性能、超轻量级的 AI Agent 框架，使用 Go 语言编写，支持多 AI Provider，内置 Web Dashboard 和 TUI 界面。

## 核心特性

| 特性 | 描述 |
|------|------|
| **多 AI Provider 支持** | 20+ 提供商：DeepSeek、OpenAI、Anthropic、Gemini、Ollama、OpenRouter、Groq、Huoshan、Zhipu、Kimi、MiniMax、DashScope、vLLM 等 |
| **TUI 界面** | 基于 BubbleTea，支持多行输入（Shift+Enter 换行）、Markdown 渲染、流式输出、斜杠命令 |
| **Coding 模式** | 放宽权限、更长超时、允许 Python/Node 代码执行 |
| **Web Dashboard** | React + TypeScript 前端，实时聊天、会话管理、配置管理 |
| **工具系统** | 15+ 内置工具：文件操作、命令执行、代码执行、Web 搜索、浏览器自动化等 |
| **技能系统** | 自动创建、渐进式加载（L0/L1/L2），Skills Hub 集成 |
| **消息网关** | Telegram、Discord、Slack、WhatsApp、WeChat、WeCom 等多平台消息接入 |
| **MCP 协议** | 连接外部 MCP 服务器，扩展 Agent 能力 |
| **会话管理** | SQLite 持久化存储，FTS5 全文搜索 |
| **多平台支持** | Linux、macOS、Windows、FreeBSD、Docker |
| **CI/CD** | GitHub Actions 自动多平台编译和发布 |

## 快速开始

### 安装

**下载 Release**

从 [GitHub Releases](https://github.com/magicwubiao/go-magic/releases) 下载对应平台的二进制文件。

**Go Install**

```bash
go install github.com/magicwubiao/go-magic/cmd/magic@latest
```

**Docker**

```bash
docker run -it magicwubiao/go-magic
```

### 首次运行

```bash
# 初始化配置
magic setup

# 开始聊天
magic chat
```

## TUI 界面

启动 `magic chat` 后进入 TUI 交互界面，支持以下斜杠命令：

| 命令 | 描述 |
|------|------|
| `/help` | 显示帮助信息 |
| `/mode` | 切换模式（chat / coding） |
| `/new` | 新建会话 |
| `/model` | 切换 AI 模型 |
| `/provider` | 切换 AI Provider |
| `/tools` | 查看/管理工具 |
| `/skills` | 查看/管理技能 |
| `/sessions` | 查看历史会话 |
| `/compact` | 压缩当前会话上下文 |
| `/config` | 打开配置 |
| `/quit` | 退出 |

**TUI 快捷操作：**
- `Shift+Enter`：多行输入换行
- `Enter`：发送消息
- 支持 Markdown 渲染和流式输出

## Coding 模式

通过 `/mode coding` 切换到 Coding 模式，专为编程任务优化。

| 特性 | Chat 模式 | Coding 模式 |
|------|-----------|-------------|
| 工具权限 | 受限 | 放宽 |
| 命令超时 | 较短 | 更长 |
| 代码执行 | 不可用 | 支持 Python / Node.js |
| 文件写入 | 需确认 | 自动执行 |
| 适用场景 | 日常对话 | 编程开发 |

### 全局配置默认模式

可在配置文件中设置默认启动模式，无需每次手动切换：

```json
{
  "chat_mode": "coding"
}
```

可选值：`"chat"`（默认）或 `"coding"`。

也可通过 Web Dashboard → 配置 → 通用 → 聊天模式 进行设置。

启动后仍可通过 `/mode chat` 或 `/mode coding` 临时切换。

## Web Dashboard

使用 React + TypeScript 构建的 Web 管理界面。

### 启动

```bash
magic server
```

默认访问地址：`http://localhost:8080`

### 功能

- 实时聊天对话
- 会话管理和历史记录
- Provider / 模型配置
- 工具和技能管理
- 消息网关配置

## 配置

配置文件路径：`~/.magic/config.json`

```json
{
  "provider": {
    "name": "deepseek",
    "api_key": "${DEEPSEEK_API_KEY}"
  },
  "model": "deepseek-chat",
  "tools": {
    "enabled": ["all"]
  },
  "gateway": {
    "enabled": true,
    "platforms": {
      "telegram": {
        "token": "${TELEGRAM_BOT_TOKEN}"
      },
      "discord": {
        "bot_token": "${DISCORD_BOT_TOKEN}"
      }
    }
  }
}
```

### 环境变量

| 变量 | 描述 |
|------|------|
| `DEEPSEEK_API_KEY` | DeepSeek API 密钥 |
| `OPENAI_API_KEY` | OpenAI API 密钥 |
| `ANTHROPIC_API_KEY` | Anthropic API 密钥 |
| `GEMINI_API_KEY` | Gemini API 密钥 |
| `TELEGRAM_BOT_TOKEN` | Telegram Bot Token |
| `DISCORD_BOT_TOKEN` | Discord Bot Token |
| `SLACK_BOT_TOKEN` | Slack Bot Token |
| `GO_MAGIC_HOME` | 配置目录（默认：`~/.magic`） |
| `GO_MAGIC_PROFILE` | Profile 名称（默认：`default`） |

## 工具系统

| 工具集 | 工具 |
|--------|------|
| **文件操作** | read_file、write_file、file_edit、list_files、search_in_files |
| **命令执行** | execute_command、terminal、process |
| **代码执行** | execute_code（Python / Node.js） |
| **Web** | web_search、web_extract |
| **浏览器** | browser_navigate、browser_snapshot、browser_click、browser_type |
| **记忆** | memory_store、memory_recall |
| **技能** | skill_list、skill_view、skill_manage |
| **MCP** | mcp_*（来自已连接的 MCP 服务器） |

## 消息网关

支持将 Agent 接入多个消息平台：

```bash
# 设置网关
magic gateway setup

# 启动网关
magic gateway start

# 配置平台
magic gateway config telegram --token <token>
magic gateway config discord --bot-token <token>
```

支持平台：Telegram、Discord、Slack、WhatsApp、WeChat、WeCom 等。

## 多平台编译

### GitHub Actions

项目已配置 GitHub Actions，推送 Tag 时自动编译并发布多平台二进制文件。

### 手动编译

**要求：**
- Go 1.21+
- Node.js 18+（用于 Web Dashboard）

**构建当前平台：**

```bash
./scripts/build.sh
```

**构建所有平台：**

```bash
./scripts/build.sh all
```

**开发构建：**

```bash
# 构建 Web 资源
cd web && npm install && npm run build && cd ..

# 复制 Web 资源用于嵌入
cp -r web/dist internal/server/dist

# 构建 Go 二进制
go build -o magic ./cmd/magic
```

**构建指定目标：**

```bash
./scripts/build.sh go      # 仅构建 Go 二进制
./scripts/build.sh web     # 仅构建 Web 应用
./scripts/build.sh docker  # 构建 Docker 镜像
```

## 下载

从 [GitHub Releases](https://github.com/magicwubiao/go-magic/releases) 下载最新版本。

支持平台：Linux（amd64/arm64）、macOS（amd64/arm64）、Windows（amd64）、FreeBSD（amd64）。

## 贡献

欢迎贡献！请阅读 [CONTRIBUTING.md](CONTRIBUTING.md) 了解指南。

## 许可证

MIT License - 详见 [LICENSE](LICENSE)。
