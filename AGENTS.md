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

## 工具集系统 (Toolset)

项目实现了工具集（Toolset）系统，用于分组管理工具，支持按平台启用/禁用。

### 内置工具集
| 工具集 | 工具 | 描述 |
|--------|------|------|
| **web** | web_search, web_extract | Web 搜索和内容提取 |
| **terminal** | execute_command | 终端命令执行 |
| **file** | read_file, write_file, file_edit, list_files, search_in_files | 文件操作 |
| **browser** | web_fetch, web_select | 轻量级浏览器工具 |
| **memory** | memory_store, memory_recall | 持久化记忆 |
| **todo** | todo | 任务规划 |
| **session** | session_search | 会话搜索 |
| **skills** | skill_list, skill_invoke, skill_info | 技能管理 |
| **cron** | cronjob | 定时任务 |
| **delegation** | delegate_task, poll_task, list_tasks, cancel_task | 子代理委托 |
| **utility** | json, yaml, string, hash, uuid, random, time, math, csv, env, system_info | 实用工具 |
| **mcp** | mcp_* | MCP 服务器工具 |

### 工具集管理
```go
// 创建工具集管理器
ts := toolset.NewManager()

// 注册工具集
ts.Register("web", []string{"web_search", "web_extract"})
ts.Register("file", []string{"read_file", "write_file"})

// 获取工具集工具
tools := ts.GetTools("web")

// 按标签过滤
filtered := ts.FilterByTags([]string{"search", "web"})

// 导出为 JSON
json := ts.ExportJSON()
```

## 技能系统 (Skills)

### 功能特性
- SKILL.md 格式支持
- 渐进式加载
- 代理自动创建
- 外部目录支持

### 管理接口
```go
// 列出所有技能
skills := manager.List()

// 获取技能详情
skill, _ := manager.Get("git-workflow")

// 创建新技能
manager.Create(name, description, content, category, tags)

// 更新技能
manager.Update(name, newContent)

// 删除技能
manager.Delete(name)

// 按分类获取
categories := manager.GetCategories()

// 获取技能目录
dir, _ := manager.GetSkillDir(name)
```

## MCP 集成

支持连接外部 MCP 服务器，提供扩展工具能力。

### 配置示例
```yaml
mcp:
  servers:
    github:
      command: npx
      args: ["-y", "@modelcontextprotocol/server-github"]
      transport: stdio
    filesystem:
      command: npx
      args: ["-y", "@modelcontextprotocol/server-filesystem", "/path/to/dir"]
      transport: stdio
```

### 命令
- `magic mcp list` - 列出所有 MCP 服务器和工具
- `magic mcp connect <name> <command>` - 连接 MCP 服务器
- `magic mcp disconnect <name>` - 断开 MCP 服务器
- `magic mcp health [name]` - 检查服务器健康状态

## 子代理委托 (Delegate Task)

支持并行子代理任务，用于复杂任务的分解执行。

### 工具
| 工具 | 描述 |
|------|------|
| delegate_task | 启动子代理任务 |
| poll_task | 轮询任务状态和结果 |
| list_tasks | 列出所有子代理任务 |
| cancel_task | 取消运行中的任务 |

### 使用示例
```json
// 启动子代理
{
  "task": "分析 /workspace/projects 目录下的代码结构",
  "context": "项目根目录",
  "timeout": 300
}

// 轮询结果
{
  "task_id": "task_123456_789",
  "wait": true
}
```
