## 项目概述
Go Magic 是一个高性能、超轻量级的 Go 实现的 AI Agent，灵感来源于 Nous Research 的 hermes-agent。支持多 Provider（OpenAI、DeepSeek、Huoshan 等）和多种消息网关（Telegram、Discord、QQ 等）。

## 技术栈
- **语言**：Go 1.25
- **依赖管理**：go modules
- **数据库**：SQLite（会话持久化）
- **构建工具**：Make、shell scripts

## 目录结构
```
/workspace/projects/
├── cmd/                    # 入口命令
│   ├── magic/              # 主程序入口
│   │   └── main.go
│   └── setup/              # 安装程序
├── internal/               # 内部包
├── pkg/                    # 公共包
├── plugins/                # 插件系统
├── skills/                 # 技能系统
├── config/                 # 配置
├── go.mod / go.sum         # 依赖管理
├── build.sh                # 构建脚本
├── Dockerfile              # Docker 构建
└── docker-compose.yml      # Docker Compose
```

## 关键入口 / 核心模块
- **主入口**：`cmd/magic/main.go`
- **构建命令**：`make build` 或 `./build.sh`
- **主要命令**：acp、agent、chat、config、gateway、health、interactive、mcp、repl、sessions、skills、voice 等

## 运行与预览
- **预览**：不支持（backend 类型）
- **运行**：`go run cmd/magic/main.go <command>`
- **构建**：`make build` 生成 `magic` 可执行文件

## 用户偏好与长期约束
- Go 版本必须 >= 1.25（项目要求）
- 使用 go modules 管理依赖
- 支持 Docker 部署

## 常见问题和预防
- SQLite 数据库用于会话存储
- 支持多平台消息网关集成
- MCP 协议用于连接外部服务器
