# go-magic 文档中心

欢迎使用 go-magic 文档！本项目是一个高性能、超轻量级的 Go 实现的 AI Agent，灵感来自 Nous Research 的 magic-agent。

## 快速链接

- [快速开始](QUICKSTART.md) - 快速上手指南
- [架构总览](ARCHITECTURE.md) - 完整架构说明
- [API 文档](API.md) - API 参考手册
- [工具系统](TOOLS.md) - 内置工具详解
- [插件系统](PLUGIN_SYSTEM.md) - 插件开发指南
- [Cortex 架构](CORTEX_ARCHITECTURE_OVERVIEW.md) - 三层六系统架构

## 文档结构

```
docs/
├── README.md                  # 文档首页
├── ARCHITECTURE.md           # 架构总览
├── API.md                    # API 文档
├── QUICKSTART.md             # 快速开始
├── TESTING.md                # 测试指南
├── TOOLS.md                  # 工具系统文档
├── TOOL_DEVELOPMENT.md       # 工具开发指南
├── PLUGIN_SYSTEM.md          # 插件系统文档
├── PLUGIN_DEVELOPMENT.md     # 插件开发指南
├── PLUGIN_API.md             # 插件 API
├── CLI_USAGE.md              # CLI 使用指南
├── CONFIGURATION.md          # 配置指南
├── CORTEX_ARCHITECTURE_OVERVIEW.md  # Cortex 架构
├── DEVELOPMENT.md            # 开发指南
├── DEPLOYMENT.md             # 部署指南
├── FAQ.md                    # 常见问题
├── GLOSSARY.md               # 术语表
├── CHANGELOG.md              # 变更日志
└── architecture/             # 各模块详细文档
    ├── 01-perception-layer.md
    ├── 02-cognition-layer.md
    ├── 03-execution-layer.md
    ├── 04-memory-system.md
    ├── 05-fts-retrieval.md
    └── 06-skill-evolution.md
```

## 核心功能

| 功能 | 描述 |
|------|------|
| **Cortex Agent** | 三层认知架构（感知层、认知层、执行层）+ 六大自进化系统 |
| **多模型支持** | OpenAI、DeepSeek、Huoshan、Anthropic、Zhipu、Kimi、MiniMax 等 |
| **工具系统** | 15+ 内置工具，支持并行执行 |
| **插件系统** | 动态加载插件扩展功能 |
| **技能系统** | 可扩展技能框架，支持自动创建 |
| **MCP 协议** | 连接外部 MCP 服务器 |
| **子 Agent** | 并行执行多个子 Agent |
| **上下文压缩** | 智能历史摘要防止上下文溢出 |
| **安全执行** | 命令白名单、注入检测、危险模式拦截 |
| **会话管理** | SQLite 持久化聊天会话 |
| **网关集成** | Telegram、Discord、WeCom、QQ、DingTalk、飞书、微信 |

## Cortex 架构预览

```
┌─────────────────────────────────────────────────────────────────────┐
│   Layer 1: Perception → Layer 2: Cognition → Layer 3: Execution     │
│   ┌─────────────┐    ┌─────────────┐    ┌─────────────────────┐     │
│   │  Intent     │    │  Task       │    │  Checkpoint &       │     │
│   │  Classification    │  Planning  │    │  Resume Support     │     │
│   │  Complexity │    │  DAG       │    │  Result Validation   │     │
│   │  Assessment │    │  Management │    │  Progress Tracking  │     │
│   └─────────────┘    └─────────────┘    └─────────────────────┘     │
└─────────────────────────────────────────────────────────────────────┘

Six Self-Evolution Systems:
┌────────────┐ ┌────────┐ ┌─────────────┐ ┌──────────────┐
│  Message   │ │ Nudge  │ │ Background  │ │  Frozen      │
│  Trigger   │ │ System │ │ Review      │ │  Snapshot    │
└────────────┘ └────────┘ └─────────────┘ └──────────────┘
┌────────────┐ ┌────────────────┐
│  FTS       │ │  Skill Auto-   │
│  Memory    │ │  Evolution     │
└────────────┘ └────────────────┘
```

## 贡献

欢迎提交 Issue 和 Pull Request！

## 许可证

MIT License - 详见 [LICENSE](../LICENSE)
