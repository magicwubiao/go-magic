# go-magic 项目规范

## 项目概述

**go-magic (Magic Agent)** -- 高性能、轻量级 AI Agent 框架，使用 Go 编写。

核心能力：
- 支持 20+ AI 提供商（DeepSeek、OpenAI、Anthropic、Gemini 等）
- 内置 TUI 界面（BubbleTea）
- Web Dashboard（嵌入二进制）
- 15+ 内置工具
- Skills 系统
- 多平台消息网关（Telegram、Discord、Slack 等）
- MCP 协议支持

## 技术栈

- **语言**: Go 1.26
- **前端**: React/TypeScript (Vite)
- **数据库**: SQLite (modernc.org/sqlite)
- **CLI 框架**: Cobra
- **TUI**: BubbleTea (CharmBracelet)
- **包管理**: Go modules, npm/pnpm

## 目录结构

```
/workspace/projects/
├── cmd/magic/           # 主程序入口
│   ├── main.go
│   ├── server.go        # Web Dashboard 服务器
│   ├── chat.go          # 聊天命令
│   ├── agent.go         # Agent 核心
│   └── ...
├── internal/            # 内部包
│   └── server/          # HTTP 服务器
├── pkg/                 # 公共包
├── web/                 # React 前端
│   ├── src/
│   ├── package.json
│   └── vite.config.ts
├── scripts/             # 构建脚本
├── Makefile             # 构建目标
├── go.mod               # Go 依赖
└── Dockerfile           # Docker 镜像构建
```

## 关键入口

- **CLI 入口**: `cmd/magic/main.go`
- **Web Server**: `cmd/magic/server.go`（端口 5000）
- **TUI 入口**: `cmd/magic/tui.go`
- **构建产物**: `./dist/magic`

## 运行与预览

### 构建

```bash
# 构建全部（web + cli）
make build

# 仅构建 CLI
make build-cli

# 仅构建 Web
make build-web
```

### 运行

```bash
# 运行 CLI
./dist/magic

# 启动 Web Dashboard
./dist/magic server --port 5000

# 交互式 TUI
./dist/magic chat
```

### Docker

```bash
docker build -t magicwubiao/go-magic .
docker run -it magicwubiao/go-magic
```

## 项目类型

- **project_type**: `backend`
- **deploy_type**: `agent`（AI Agent 服务）
- **可预览**: 否（CLI 工具，无独立预览服务）

## 已知约束

1. Web 前端由 Vite 构建到 `internal/server/dist`，由 Go 嵌入二进制
2. 构建必须先完成 web 前端，再编译 Go 二进制
3. 服务默认端口 5000
4. 使用 `CGO_ENABLED=0` 静态编译

## 常见命令

| 命令 | 说明 |
|------|------|
| `make build` | 构建 web + cli |
| `make test` | 运行测试 |
| `make lint` | 运行 linter |
| `make run` | 源码运行 |
| `make docker` | 构建并运行 Docker |
