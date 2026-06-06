# AGENTS.md - go-magic 项目规范

## 项目概述

- **项目名**: go-magic (Magic Agent)
- **GitHub**: https://github.com/magicwubiao/go-magic
- **描述**: 高性能、超轻量级 AI Agent 框架，使用 Go 语言编写
- **版本**: v0.3.1
- **Go 版本要求**: 1.25+

## 技术栈

| 组件 | 技术 |
|------|------|
| 后端 | Go 1.25+ |
| 前端 | React + TypeScript + Vite |
| 状态管理 | Pinia |
| 路由 | Vue Router |
| TUI | BubbleTea |
| 数据库 | SQLite (FTS5) |
| 包管理 | pnpm (前端) |

## 目录结构

```
go-magic/
├── cmd/magic/          # 主程序入口
│   ├── main.go        # TUI 入口
│   ├── coding.go      # Coding 模式
│   ├── tui.go         # TUI 核心
│   └── server.go      # Web 服务入口
├── internal/          # 内部包
│   ├── agent/         # Agent 核心
│   ├── cortex/        # 思考引擎
│   ├── execution/     # 执行器
│   ├── skills/        # Skills 系统
│   ├── solo/          # SOLO 长任务模式
│   ├── subagent/      # 子 Agent
│   ├── tool/          # 工具系统
│   └── server/        # HTTP 服务
├── web/               # Web 前端
│   └── src/
│       ├── api/       # API 客户端
│       ├── stores/    # Pinia stores
│       ├── views/     # 页面组件
│       └── locales/   # 国际化
├── pkg/               # 公共包
│   └── config/        # 配置
└── skills/            # 内置 Skills
    ├── chat/
    ├── coding/
    └── devops/
```

## 关键模块

### SOLO 长任务模式

**功能**: Plan + Execute 架构处理长编码任务

**文件**:
- `internal/solo/task.go` - 任务定义
- `internal/solo/manager.go` - 任务管理器
- `internal/solo/executor.go` - 步骤执行器
- `internal/solo/handler.go` - API 处理
- `internal/server/solo_handler.go` - Server 集成
- `cmd/magic/solo_tui.go` - TUI 命令
- `web/src/views/SoloView.vue` - Web 界面

**API 接口**:
| 方法 | 路径 | 功能 |
|------|------|------|
| POST | `/api/solo/tasks` | 创建任务 |
| GET | `/api/solo/tasks` | 列出任务 |
| GET | `/api/solo/tasks/:id` | 获取任务 |
| DELETE | `/api/solo/tasks/:id` | 删除任务 |
| POST | `/api/solo/tasks/:id/plan` | 生成计划 |
| POST | `/api/solo/tasks/:id/approve` | 批准执行 |
| POST | `/api/solo/tasks/:id/cancel` | 取消 |
| POST | `/api/solo/tasks/:id/pause` | 暂停 |
| POST | `/api/solo/tasks/:id/resume` | 恢复 |
| GET | `/api/solo/tasks/:id/results` | 获取结果 |
| GET | `/api/solo/tasks/:id/logs` | 获取日志 |

**TUI 命令**: `/solo` 进入 SOLO 模式

### Skills Hub

**功能**: 技能市场，支持多源搜索安装

**文件**:
- `internal/skills/manager.go` - 技能管理
- `internal/skills/types.go` - 类型定义
- `internal/tool/registry.go` - 工具注册

**支持源**: github, official, hub, skills.sh

**工具**:
- `find_skills` - 搜索技能
- `install_skill` - 安装技能

### Tools 系统

**15+ 内置工具**:

| 工具集 | 工具 |
|--------|------|
| Web | web_search, web_extract |
| File | read_file, write_file, file_edit, list_files, search_in_files |
| Terminal | execute_command, terminal, process |
| Browser | browser_navigate, browser_snapshot, browser_click, browser_type |
| Code | execute_code (Python, Node.js) |
| Memory | memory_store, memory_recall |
| Skills | skill_list, skill_view, skill_manage |
| SOLO | solo_coding |

## 运行方式

```bash
# Web 服务
./magic server

# TUI 界面
./magic

# 指定端口
./magic server --port 8080
```

## 环境变量

| 变量 | 说明 |
|------|------|
| `MAGIC_HOME` | 配置目录 |
| `PORT` | Web 端口 (默认 5000) |

## 用户偏好

1. **SOLO 模式**: Coding 任务默认使用 SOLO 长任务模式
2. **编程语言**: 主要使用 Go 后端，TypeScript 前端
3. **包管理**: 前端用 pnpm，不用 npm/yarn

## 常见约束

1. **Go 版本**: 必须 >= 1.25
2. **Node 版本**: >= 22, pnpm >= 10.33.0
3. **TUI 端口**: 不使用 9000 端口
4. **预览端口**: Web 项目只暴露 5000 端口

## 工作区规范

- 工作区根目录: `/workspace/projects`
- 技术项目根目录: `/workspace/projects`
- 源代码打包: `git archive --format=tar.gz -o source.tar.gz HEAD`
