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
│   ├── magic/              # 主程序入口 (50个命令)
│   └── setup/              # 安装程序
├── internal/               # 内部包 (44个模块)
│   ├── agent/             # AI Agent 核心
│   ├── tool/              # 工具系统
│   ├── skills/            # 技能管理
│   ├── session/           # 会话管理
│   ├── memory/            # 记忆系统
│   ├── gateway/           # 消息网关
│   ├── provider/          # LLM Provider
│   ├── skin/              # 皮肤/主题系统
│   └── ...                # 其他模块
├── pkg/                    # 公共包
├── plugins/                # 插件系统
├── skills/                 # 技能系统
├── scripts/                # 构建脚本
│   └── windows/            # Windows 脚本
├── server/                 # Web Dashboard 服务
├── web/                    # Vue 3 前端
├── docs/                   # 文档
├── config/                 # 配置
├── build.sh                # 构建脚本
├── Makefile                # Make 构建
├── Dockerfile              # Docker 构建
├── docker-compose.yml      # Docker Compose
├── .gitignore              # Git 忽略
└── go.mod / go.sum         # 依赖管理
```

## 关键入口 / 核心模块
- **主入口**：`cmd/magic/main.go`
- **构建命令**：`make build` 或 `./build.sh`
- **主要命令**：acp、agent、chat、config、gateway、health、interactive、mcp、repl、sessions、skills、skin、voice 等

## 运行与预览
- **预览**：不支持（backend 类型）
- **Web Dashboard**：`magic server` 或 `magic server --port 5000`
- **运行**：`go run cmd/magic/main.go <command>`
- **构建**：`make build` 生成 `magic` 可执行文件

## Web Dashboard UI

现代化的 Web 管理界面，基于 Vue 3 + TypeScript + Vite 构建。

### 目录结构
```
web/
├── src/
│   ├── views/
│   │   ├── ChatView.vue      # 主聊天界面
│   │   ├── HistoryView.vue   # 历史记录
│   │   ├── SettingsView.vue  # 设置页面
│   │   └── ...
│   ├── components/
│   │   ├── Sidebar.vue       # 侧边栏
│   │   ├── CommandPalette.vue # 命令面板
│   │   ├── MarkdownRenderer.vue # Markdown 渲染
│   │   ├── FileUpload.vue    # 文件上传
│   │   ├── KeyboardShortcuts.vue # 快捷键指南
│   │   └── ToolCallDisplay.vue # 工具调用展示
│   └── ...
└── dist/                      # 构建产物 (嵌入 internal/server/dist)
```

### 功能特性
- **ChatView**: 流式响应、文件上传、模型选择、快捷键支持
- **CommandPalette**: 模糊搜索命令 (Ctrl+K)
- **KeyboardShortcuts**: 全面的键盘快捷键指南
- **ToolCallDisplay**: 可视化工具调用过程
- **MarkdownRenderer**: 代码高亮、表格、列表等

### 构建命令
```bash
cd web && pnpm build          # 构建前端
cp -r web/dist internal/server/dist  # 复制到嵌入目录
go build ./cmd/magic         # 编译后端
```

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
| **terminal** | execute_command, terminal, process | 终端命令执行（多后端） |
| **file** | read_file, write_file, file_edit, list_files, search_in_files | 文件操作 |
| **browser** | browser_navigate, browser_snapshot, browser_click, browser_type, browser_scroll, browser_back, browser_get_images, browser_console | 浏览器自动化 |
| **memory** | memory_store, memory_recall | 持久化记忆 |
| **todo** | todo | 任务规划 |
| **session** | session_search | 会话搜索 |
| **skills** | skill_list, skill_view, skill_manage, skill_create, skill_delete | 技能管理 |
| **cron** | cronjob | 定时任务 |
| **delegation** | delegate_task, poll_task, list_tasks, cancel_task | 子代理委托 |
| **code_execution** | execute_code | 内置 Python/Node.js 代码执行（带工具调用） |
| **homeassistant** | ha_list_entities, ha_get_state, ha_list_services, ha_call_service, ha_events, ha_config | 智能家居控制 |
| **utility** | json, yaml, string, hash, uuid, random, time, math, csv, env, system_info | 实用工具 |
| **mcp** | mcp_* | MCP 服务器工具 |

## 终端后端系统 (Terminal Backends)

支持多种执行后端，用于安全隔离的终端操作。

### 支持的后端
| 后端 | 描述 | 使用场景 |
|------|------|----------|
| **local** | 本地执行（默认） | 信任的本地操作 |
| **docker** | Docker 容器隔离 | 安全隔离、跨平台 |
| **ssh** | 远程服务器执行 | 远程操作、沙箱隔离 |

### Docker 后端配置
```yaml
terminal:
  backend: docker
  docker_image: golang:1.25-alpine
  container_memory: 512m
  container_cpu: 1.0
```

### SSH 后端配置
```yaml
terminal:
  backend: ssh
  ssh_host: my-server.example.com
  ssh_user: myuser
  ssh_key: ~/.ssh/id_rsa
```

### 后端管理器
```go
// 创建后端管理器
manager := NewBackendManager()

// 列出可用后端
backends := manager.List()

// 执行命令
result, _ := manager.Execute(ctx, "docker", "ls -la", "/workspace", 30*time.Second)
```

## 浏览器自动化工具

增强的浏览器自动化工具集，支持页面导航、元素操作、内容提取等。

### 工具列表
| 工具 | 描述 |
|------|------|
| browser_navigate | 导航到 URL，获取页面内容 |
| browser_snapshot | 获取页面快照（标题、链接、表单等） |
| browser_click | 点击页面元素 |
| browser_type | 输入文本到输入框 |
| browser_scroll | 滚动页面 |
| browser_back | 返回上一页 |
| browser_get_images | 提取页面图片 URL |
| browser_console | 获取控制台消息（需要 JS） |

### 使用示例
```json
// 导航到页面
{"tool": "browser_navigate", "args": {"url": "https://example.com"}}

// 获取页面快照
{"tool": "browser_snapshot", "args": {"url": "https://example.com", "selector": "article"}}

// 提取所有图片
{"tool": "browser_get_images", "args": {"url": "https://example.com"}}
```

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
- 渐进式加载 (L0/L1/L2)
- 条件激活 (fallback_for_toolset, requires_toolset)
- 平台限制 (macos/linux/windows)
- 代理自动创建
- Skills Hub 集成
- 外部目录支持

### 渐进式加载
```go
// Level 0: 仅列表 - 返回名称、描述、分类
level0, _ := manager.LoadSkillAtLevel("git-workflow", &SkillViewOptions{Level: Level0})

// Level 1: 完整内容 - 返回技能完整内容
level1, _ := manager.LoadSkillAtLevel("git-workflow", &SkillViewOptions{Level: Level1})

// Level 2: 带引用 - 返回特定引用文件
level2, _ := manager.LoadSkillAtLevel("git-workflow", &SkillViewOptions{
    Level: Level2,
    Path:  "references/commands.md",
})
```

### 条件激活
```go
// 根据可用工具集/工具过滤技能
visible := manager.FilterSkillsByCondition(
    []string{"web", "terminal"},           // 可用工具集
    []string{"web_search", "read_file"},  // 可用工具
    "linux",                               // 当前平台
)
```

### Skills Hub 集成
```go
// 搜索 Hub
skills, _ := manager.SearchHub("kubernetes", []HubSource{HubSourceOfficial, HubSourceSkillsSh})

// 从 Hub 安装
manager.InstallFromHub(HubSourceOfficial, "security/1password")

// 检查更新
updates, _ := manager.CheckForUpdates()
```

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

## Dashboard UI

Web 管理界面，基于 React + TypeScript + Vite 构建。

### 目录结构
```
web/
├── src/
│   ├── api/              # API 调用
│   ├── components/       # 组件
│   ├── hooks/           # React Hooks
│   ├── i18n/            # 国际化
│   ├── lib/             # 工具库
│   ├── pages/           # 页面
│   ├── plugins/         # 插件系统
│   └── styles/          # 样式
└── dist/                # 构建产物
```

### 页面路由
- `/` - 首页仪表盘
- `/sessions` - 会话管理
- `/chat` - 聊天界面
- `/skills` - 技能管理
- `/tools` - 工具集
- `/logs` - 日志查看
- `/settings` - 设置
- `/config` - 配置

### API 端点
- `/api/health` - 健康检查
- `/api/sessions` - 会话管理
- `/api/sessions/:id/messages` - 消息管理
- `/api/skills` - 技能管理
- `/api/toolsets` - 工具集
- `/api/plugins` - 插件管理
- `/api/logs` - 日志
- `/api/config` - 配置
- `/api/system/info` - 系统信息
- `/api/analytics/models` - 模型分析
- `/api/dashboard/themes` - 主题
- `/api/dashboard/plugins` - 仪表盘插件

### 构建和运行
```bash
# 构建前端
cd web && pnpm build

# 复制到嵌入目录
cp -r dist/* ../internal/server/dist/

# 构建 Go 二进制
go build -o magic ./cmd/magic

# 运行服务
./magic server --port 5000
```

### 预览脚本
- `scripts/coze-preview-build.sh` - 构建脚本
- `scripts/coze-preview-run.sh` - 运行脚本

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

## Home Assistant 集成

支持智能家居控制，连接 Home Assistant 实例进行设备管理。

### 环境配置
```bash
export HASS_URL="http://homeassistant.local:8123"
export HASS_TOKEN="your_long_lived_access_token"
```

### 工具列表
| 工具 | 描述 |
|------|------|
| ha_list_entities | 列出所有实体及其状态 |
| ha_get_state | 获取特定实体状态 |
| ha_list_services | 列出可用服务 |
| ha_call_service | 调用 Home Assistant 服务 |
| ha_events | 订阅/管理事件 |
| ha_config | 获取系统配置信息 |

### 使用示例
```json
// 列出所有灯光
{"tool": "ha_list_entities", "args": {"domain": "light"}}

// 获取实体状态
{"tool": "ha_get_state", "args": {"entity_id": "light.living_room"}}

// 开灯
{"tool": "ha_call_service", "args": {
  "domain": "light",
  "service": "turn_on",
  "entity_id": "light.living_room",
  "data": {"brightness": 255}
}}

// 设置温度
{"tool": "ha_call_service", "args": {
  "domain": "climate",
  "service": "set_temperature",
  "entity_id": "climate.thermostat",
  "data": {"temperature": 22}
}}
```

## 代码执行 (execute_code)

内置 Python 和 Node.js 代码执行，支持从代码中调用工具。

### 支持的语言
| 语言 | 说明 |
|------|------|
| `python`, `python3` | Python 3.x (默认) |
| `node`, `nodejs`, `js` | Node.js (JavaScript) |

### 环境配置
```bash
# 可选：设置工作目录限制
export CODE_ALLOWED_DIRS="/tmp,/workspace"
export CODE_TIMEOUT=120
export CODE_MEMORY_LIMIT=512
```

### 工具接口
```go
tool := NewExecuteCodeTool()

// 注册可用的工具
tool.RegisterTool("read_file", &ReadFileTool{})
tool.RegisterTool("web_search", &WebSearchTool{})

// 执行 Python 代码
result, _ := tool.Execute(ctx, map[string]interface{}{
    "code":      "print('Hello'); result = read_file(path='/tmp/test.txt')",
    "language":  "python",
    "timeout":   60,
    "workdir":   "/tmp",
})

// 执行 Node.js 代码
result, _ := tool.Execute(ctx, map[string]interface{}{
    "code":      "console.log('Hello'); const result = await read_file({path: '/tmp/test.txt'});",
    "language":  "node",
    "timeout":   60,
})
```

### Python 代码中使用工具
```python
# 读取文件
content = read_file('/tmp/test.txt')
print(content)

# 搜索网页
results = web_search('golang tutorial', count=5)
print(results)

# 搜索文件
matches = search_files('TODO', path='/workspace')
print(matches)

# 执行命令
output = terminal('ls -la')
print(output)
```

### Node.js 代码中使用工具
```javascript
// 读取文件
const content = await read_file({path: '/tmp/test.txt'});
console.log(content);

// 搜索网页
const results = await web_search({query: 'golang tutorial', count: 5});
console.log(results);

// 搜索文件
const matches = await search_files({pattern: 'TODO', path: '/workspace'});
console.log(matches);

// 执行命令
const output = await terminal({command: 'ls -la'});
console.log(output);

// JSON 输出
json_dump({ key: 'value' });
```

### 内置模板
```go
// 使用预置模板 (Python)
code, ok := GetTemplate("file_processor")
code, ok := GetTemplate("web_scraper")
code, ok := GetTemplate("data_analysis")

// 使用预置模板 (Node.js)
code, ok := GetTemplate("file_processor_node")
code, ok := GetTemplate("web_scraper_node")
code, ok := GetTemplate("data_analysis_node")

// 列出所有模板
templates := ListTemplates()
```

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

## Profile 多实例支持

支持多个完全隔离的配置实例，每个实例有独立的配置、密钥、记忆、会话等。

### 目录结构
```
~/.magic/
├── config.yaml          # 默认配置
├── .env                 # 默认密钥
├── skills/              # 默认技能
├── sessions/            # 默认会话
├── profiles/
│   ├── default/         # 默认配置
│   ├── work/            # 工作配置
│   └── dev/             # 开发配置
```

### 配置示例
```yaml
# ~/.magic/profiles/work/config.yaml
provider:
  name: deepseek
  api_key: ${DEEPSEEK_API_KEY}
  
memory:
  enabled: true
  max_entries: 1000
```

### 环境变量
```bash
export GO_MAGIC_HOME=~/.magic
export GO_MAGIC_PROFILE=work  # 使用 work 配置
```

### CLI 命令
```bash
magic profile list           # 列出所有配置
magic profile create dev     # 创建新配置
magic profile switch prod    # 切换到 prod 配置
magic profile delete dev     # 删除配置
magic profile export work    # 导出配置
magic profile import work.tar.gz  # 导入配置
```

## MCP Server

go-magic 可以作为 MCP Server 被其他 MCP 客户端（如 Claude Code、Cursor）使用。

### 启动 MCP Server
```bash
magic mcp serve
```

### MCP Client 配置
在 Claude Code 的 `~/.claude/claude_desktop_config.json` 中添加：
```json
{
  "mcpServers": {
    "go-magic": {
      "command": "go-magic",
      "args": ["mcp", "serve"]
    }
  }
}
```

### 可用工具
| 工具 | 描述 |
|------|------|
| conversations_list | 列出所有会话 |
| conversation_get | 获取会话详情 |
| messages_read | 读取消息历史 |
| messages_send | 发送消息 |
| events_poll | 轮询新事件 |
| channels_list | 列出可用频道 |

### 配置示例
```yaml
mcp:
  enabled: true
  port: 8765
  auth:
    enabled: true
    api_key: ${MCP_API_KEY}
```

## Dashboard UI

Web 管理界面，用于管理会话、配置、技能、日志等。

### 快速启动
```bash
# 安装
npm install -g go-magic-web-ui

# 启动
go-magic-web-ui start
# 或指定端口
go-magic-web-ui start --port 9000
```

### Docker 部署
```yaml
version: '3.8'
services:
  go-magic:
    image: go-magic:latest
    ports:
      - "8642:8642"
  
  web-ui:
    image: go-magic-web-ui:latest
    ports:
      - "8648:8648"
    environment:
      - UPSTREAM=http://go-magic:8642
```

### 功能特性
- **AI 聊天** - 实时流式响应、多会话管理、Markdown 渲染
- **会话管理** - 创建、重命名、删除、按平台分组
- **工具集配置** - 按需启用/禁用工具集
- **技能管理** - 浏览、安装、查看技能详情
- **配置管理** - Provider、Display、Agent 设置
- **日志查看** - 实时日志、筛选、搜索
- **Profile 管理** - 多实例配置切换

### API 端点
| 端点 | 方法 | 描述 |
|------|------|------|
| `/api/sessions` | GET/POST | 会话列表/创建 |
| `/api/sessions/:id` | GET/DELETE | 会话详情/删除 |
| `/api/toolsets` | GET | 工具集列表 |
| `/api/skills` | GET | 技能列表 |
| `/api/config` | GET/PUT | 配置读写 |
| `/api/logs` | GET | 日志查询 |
| `/api/health` | GET | 健康检查 |

### 开发
```bash
cd web
pnpm install
pnpm dev     # 开发模式
pnnpm build   # 构建
```

## CLI 命令参考

### setup 命令
交互式配置向导，引导用户完成完整设置流程。
```bash
magic setup              # 完整设置向导
magic setup --skip-model # 跳过模型选择
magic setup --skip-tools  # 跳过工具集选择
```

### doctor 命令
诊断工具，检查配置和连接状态。
```bash
magic doctor              # 完整诊断
magic doctor --check config    # 仅配置
magic doctor --check provider  # 仅 Provider
magic doctor --check tools    # 仅工具
magic doctor --check gateway   # 仅 Gateway
magic doctor --check skills    # 仅技能
```

### migrate 命令
从 OpenClaw 迁移配置和数据。
```bash
magic migrate              # 交互式迁移
magic migrate --dry-run    # 预览迁移内容
magic migrate --preset user-data  # 仅用户数据
magic migrate --overwrite   # 覆盖冲突
```

### tools 命令
工具和工具集管理。
```bash
magic tools list           # 列出所有工具
magic tools list --json    # JSON 格式输出
magic tools toolsets list       # 列出所有工具集
magic tools toolsets show web   # 查看工具集详情
magic tools toolsets enable browser  # 启用工具集
magic tools toolsets disable terminal # 禁用工具集
```

### model 命令
模型选择和配置。
```bash
magic model               # 交互式模型选择
magic model list          # 列出可用模型
magic model set anthropic/claude-opus-4.6  # 设置模型
```

### gateway 命令
消息网关管理。
```bash
magic gateway setup       # 配置网关
magic gateway start       # 启动网关
magic gateway stop        # 停止网关
magic gateway status      # 查看状态
```

### 快速恢复序列
当配置出现问题时，按以下顺序排查：
```bash
1. magic doctor           # 诊断问题
2. magic model            # 重新选择模型
3. magic setup            # 重新设置
4. magic sessions list    # 查看会话
5. magic gateway status   # 网关状态
```

## 使用量统计 (Usage Tracking)

内置 Token 和成本追踪系统，支持每日/每月统计、预算限制和告警。

### CLI 命令
```bash
magic usage              # 显示今日统计
magic usage --daily      # 按日统计
magic usage --monthly    # 按月统计
magic usage --budget     # 设置预算
magic usage --export     # 导出 CSV
```

### 功能特性
- 按 Provider/Model 分组统计
- 每日/每月/每年汇总
- 成本计算（基于 Provider 定价）
- 预算限制和告警
- 数据导出 (CSV/JSON)

## 密钥管理 (Secrets)

加密存储 API 密钥和其他敏感信息，支持多种后端。

### CLI 命令
```bash
magic secrets list              # 列出所有密钥
magic secrets set OPENAI_KEY    # 存储新密钥
magic secrets get OPENAI_KEY    # 获取密钥值
magic secrets delete OPENAI_KEY # 删除密钥
magic secrets rename OLD NEW    # 重命名
magic secrets import-env        # 从环境变量导入
```

### 后端支持
- **keyring**: 系统密钥链 (Linux: libsecret, macOS: Keychain, Windows: Credential Manager)
- **file**: 加密文件存储 (AES-256-GCM)

## 自动更新 (Auto-Update)

检查和安装新版本，支持备份、回滚和多通道更新。

### CLI 命令
```bash
magic update              # 检查并更新
magic update --check      # 仅检查
magic update --backup     # 更新前备份
magic update --force      # 强制更新
magic update --channel beta  # 使用 beta 通道
```

## Prompt 模板系统

管理和使用提示模板，支持变量插值和分类管理。

### CLI 命令
```bash
magic prompt list              # 列出模板
magic prompt list --system     # 包含系统模板
magic prompt list --tags code  # 按标签筛选
magic prompt show <name>       # 查看详情
magic prompt create <name>     # 创建模板
magic prompt edit <name>       # 编辑模板
magic prompt delete <name>     # 删除模板
magic prompt export [name]     # 导出
magic prompt import <file>     # 导入
```

### 内置模板
| 模板 | 描述 | 分类 |
|------|------|------|
| default | 默认 Agent 提示 | system |
| research | 研究助手 | system |
| coder | 代码助手 | system |
| summarizer | 内容总结 | agent |

## 交互式 UI

终端进度条和动画效果。

### Spinner 示例
```go
spinner := ui.NewSpinner("Loading...")
spinner.Start()
defer spinner.Stop()

// 执行任务
result := doWork()

spinner.Success("Done!")
```

### 进度条示例
```go
bar := ui.NewProgressBar(100)
bar.Start()

for i := 0; i < 100; i++ {
    bar.Update(i)
    time.Sleep(10 * time.Millisecond)
}

bar.Finish()
```

## Webhook 事件系统

事件驱动架构，支持订阅和触发 Webhook。

### 配置
```yaml
webhook:
  enabled: true
  port: 8643
  events:
    - message.received
    - agent.started
    - agent.completed
    - tool.called
  handlers:
    - url: https://example.com/webhook
      events: [message.received]
      secret: ${WEBHOOK_SECRET}
```

### 事件类型
| 事件 | 描述 |
|------|------|
| message.received | 收到新消息 |
| message.sent | 发送消息 |
| agent.started | Agent 启动 |
| agent.completed | Agent 完成 |
| tool.called | 工具调用 |
| error | 发生错误 |

## 审计日志

记录敏感操作的审计日志。

### CLI 命令
```bash
magic audit list              # 查看审计日志
magic audit list --user alice # 按用户筛选
magic audit list --since 24h  # 最近 24 小时
magic audit export            # 导出日志
```

### 记录的操作
- 密钥访问和修改
- 配置变更
- 插件安装/卸载
- 敏感工具调用
- 认证和授权事件

## 多语言支持 (i18n)

支持多种语言界面。

### 支持的语言
| 语言 | 代码 |
|------|------|
| English | en |
| 中文 | zh |
| 日本語 | ja |
| 한국어 | ko |

### CLI 命令
```bash
magic i18n list               # 列出可用语言
magic i18n set zh              # 设置语言
magic i18n translate <key>     # 翻译指定 key
```

## 插件市场

发现和安装社区插件。

### CLI 命令
```bash
magic plugin list              # 列出已加载插件
magic plugin search <query>    # 搜索插件
magic plugin install <id>      # 安装插件
magic plugin uninstall <id>    # 卸载插件
magic plugin enable <id>       # 启用插件
magic plugin disable <id>      # 禁用插件
magic plugin reload <id>       # 热重载插件
magic plugin update [id]       # 更新插件
magic plugin info <id>         # 查看插件详情
magic plugin discover          # 发现可用插件
magic plugin check            # 检查插件更新
```

### 插件类型
| 类型 | 描述 | 示例 |
|------|------|------|
| **script** | Shell 脚本插件 | Python、Bash、PowerShell |
| **binary** | 二进制可执行插件 | Go、C++、Rust 编译产物 |
| **http** | HTTP API 插件 | REST API 服务 |
| **native** | 原生 Go 插件 | 编译成共享库 |

### 插件结构
```
~/.magic/plugins/
├── manifest.json     # 插件元数据
├── plugin.go         # 插件代码（script 类型）
├── plugin.sh         # Shell 脚本（script 类型）
└── README.md         # 文档
```

### manifest.json 格式
```json
{
  "id": "my-plugin",
  "name": "My Plugin",
  "version": "1.0.0",
  "author": "Author Name",
  "description": "Plugin description",
  "category": "utilities",
  "tags": ["tool", "utility"],
  "type": "script",
  "entry": "plugin.sh",
  "permissions": ["network", "filesystem"],
  "hooks": ["pre_agent", "post_agent"],
  "commands": [
    {
      "name": "mycmd",
      "description": "My command"
    }
  ]
}
```

### 插件沙箱隔离
插件在受限环境中运行，支持多种隔离级别：

```go
// 沙箱配置
sandbox := plugin.NewSandbox(&plugin.SandboxConfig{
    AllowedPaths:    []string{"/tmp", "/workspace"},
    DeniedPaths:     []string{"/etc", "/root"},
    NetworkAccess:   false,       // 禁用网络访问
    EnvWhitelist:    []string{"PATH", "HOME"},
    MaxMemoryMB:     512,
    MaxCPUPercent:   50,
    Timeout:         30 * time.Second,
})
```

### 插件仓库
支持多个插件源：

```go
// GitHub releases
repo := plugin.NewGitHubRepository("owner/repo")

// 本地目录
repo := plugin.NewLocalRepository("/path/to/plugins")

// GitHub API 搜索
results, _ := repo.Search(context.Background(), "keyword")
```

## Context Files (项目上下文)

每次对话自动加载项目上下文文件，影响 Agent 行为。

### 支持的文件
| 文件 | 描述 |
|------|------|
| `AGENTS.md` | 项目规范和经验 |
| `SOUL.md` | Agent 个性定义 |
| `MEMORY.md` | 持久记忆 |
| `USER.md` | 用户画像 |
| `.context/` | 额外上下文目录 |

### CLI 命令
```bash
magic context load <path>      # 加载项目上下文
magic context list             # 列出已加载的上下文
magic context reload           # 重新加载上下文
magic context clear            # 清除所有上下文
```

### 功能特性
- 自动检测 AGENTS.md 并加载
- 支持 SOUL.md 个性化 Agent
- 上下文版本控制
- 增量更新

## 内置学习循环 (Learning Loop)

Agent 自动从经验中学习并改进技能。

### 功能
- **技能自动改进**: 复杂任务后自动优化技能
- **记忆强化**: 定期提醒 Agent 保存重要信息
- **会话搜索**: FTS5 全文搜索 + LLM 摘要
- **自我改进**: 分析失败并调整策略

### CLI 命令
```bash
magic learn analyze            # 分析学习数据
magic learn stats             # 显示学习统计
magic learn improve <skill>   # 改进指定技能
```

## 用户画像建模 (User Profiling)

跨会话学习用户偏好和特点。

### 功能
- 用户偏好学习
- 专业知识追踪
- 交流风格适应
- 长期记忆整合

### CLI 命令
```bash
magic profile user             # 查看用户画像
magic profile update <key>    # 更新用户信息
magic profile clear            # 清除画像
```

## 增强 TUI 界面

功能完整的终端用户界面。

### 功能
- 多行编辑支持
- Slash 命令自动补全
- 命令历史搜索
- 流式工具输出
- 实时进度显示
- ANSI 颜色支持

### 快捷键
| 快捷键 | 描述 |
|--------|------|
| `↑/↓` | 浏览历史 |
| `Ctrl+R` | 搜索历史 |
| `Tab` | 自动补全 |
| `Ctrl+C` | 中断当前任务 |
| `Ctrl+L` | 清屏 |

## Personality 系统

个性化 Agent 性格和行为模式。

### 内置个性
| 个性 | 描述 |
|------|------|
| default | 默认助手 |
| creative | 创意写作 |
| precise | 精确分析 |
| friendly | 友好闲聊 |
| professional | 专业严谨 |

### CLI 命令
```bash
magic personality list         # 列出所有个性
magic personality set <name>   # 设置当前个性
magic personality create      # 创建自定义个性
magic personality preview <name>  # 预览个性
```

## Skin/Theme 系统

CLI 视觉主题定制，支持多种内置皮肤和用户自定义。

### 内置皮肤
| 皮肤 | 描述 |
|------|------|
| default | 经典金色/kawaii 风格 |
| mono | 简洁灰度单色 |
| slate | 冷色调开发者风格 |
| cyber | 霓虹赛博朋克主题 |

### CLI 命令
```bash
magic skin list              # 列出所有皮肤
magic skin list --all       # 显示详细信息
magic skin show [name]      # 查看皮肤详情
magic skin preview [name]   # 预览皮肤效果
magic skin set <name>      # 设置活动皮肤
magic skin create <name>    # 创建自定义皮肤
magic skin delete <name>    # 删除用户皮肤
magic skin export [name]    # 导出皮肤为 JSON
```

### 皮肤配置示例
用户皮肤存储在 `~/.magic/skins/` 目录，YAML 格式：

```yaml
name: my-theme
description: My custom skin

colors:
  banner_border: "#FFD700"
  banner_title: "#FF8C00"
  banner_text: "#FFFFFF"
  success: "#00FF00"
  error: "#FF0000"

spinner:
  frames: ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴"]
  thinking_verbs: ["thinking", "processing"]
  speed: 80

branding:
  agent_name: "magic"
  prompt_symbol: ">"

tool_prefix: "┊"
```

### 工具图标
每个皮肤可以自定义工具图标：

```yaml
tool_emojis:
  web_search: "🌐"
  read_file: "📄"
  execute_command: "⚡"
  memory_store: "💾"
  execute_code: "💻"
```

## Slash Commands

会话中可用的斜杠命令。

### 核心命令
| 命令 | 描述 |
|------|------|
| `/new` | 开始新对话 |
| `/reset` | 重置当前会话 |
| `/model <provider:model>` | 切换模型 |
| `/personality <name>` | 切换个性 |
| `/retry` | 重试上一条消息 |
| `/undo` | 撤销上一条消息 |
| `/compress` | 压缩上下文 |
| `/usage` | 显示使用统计 |
| `/insights [days]` | 显示使用洞察 |
| `/skills` | 浏览技能列表 |
| `/help` | 显示帮助 |

### 工具命令
| 命令 | 描述 |
|------|------|
| `/web <query>` | 网页搜索 |
| `/browse <url>` | 浏览网页 |
| `/code <language>` | 代码执行 |
| `/terminal` | 终端操作 |

## Terminal 后端扩展

支持多种终端执行后端。

### 支持的后端
| 后端 | 描述 |
|------|------|
| local | 本地执行（默认） |
| docker | Docker 容器隔离 |
| ssh | 远程服务器执行 |
| daytona | Daytona 云端隔离 |
| singularity | Singularity 容器 |

### 配置
```yaml
terminal:
  backend: daytona  # 使用 Daytona 云端后端
  daytona_api_key: ${DAYTONA_API_KEY}
  daytona_workspace: ${DAYTONA_WORKSPACE}
```

## 会话压缩 (Context Compression)

优化长对话的上下文窗口。

### 功能
- 自动摘要历史消息
- 保留关键信息
- 减少 Token 消耗
- 保持对话连贯性

### CLI 命令
```bash
magic compress                # 压缩当前会话
magic compress --dry-run     # 预览压缩效果
magic compress --level full  # 完整压缩
```

## Usage Insights

详细的使用分析和洞察。

### CLI 命令
```bash
magic usage insights          # 最近 7 天洞察
magic usage insights --days 30  # 最近 30 天
magic usage insights daily   # 按日分析
magic usage insights models  # 按模型分析
```

### 分析内容
- 请求和 Token 统计
- 成本分析
- Top 模型排名
- 每日趋势
- 使用高峰时段
- 优化建议


## Voice Mode 增强

实时语音交互，支持 TTS/ASR 流式处理。

### 功能
- 多提供商支持 (OpenAI, ElevenLabs, Azure, Coqui)
- 流式语音合成和识别
- 实时对话模式
- 语音指令控制

### CLI 命令
```bash
magic voice listen           # 启动语音模式
magic voice speak <text>     # 文字转语音
magic voice test            # 测试配置
```

## Batch 批处理

批量轨迹生成和 RL 训练支持。

### 功能
- 批量任务执行
- 轨迹生成和导出
- 查询批处理
- 进度追踪

### CLI 命令
```bash
magic batch list                    # 列出任务
magic batch create <file> --type trajectory  # 创建任务
magic batch status <job_id>        # 查看状态
magic batch export <job_id>        # 导出结果
```

## 图像理解/生成

Vision 工具集成，支持图像分析和生成。

### 功能
- 图像内容理解
- 图像描述生成
- 图表/截图分析
- 多模态交互

### 支持模型
- GPT-4V / Claude Vision
- Gemini Pro Vision
- 本地模型 (LLaVA, CogVLM)

## Skills Hub 集成

agentskills.io 官方技能市场集成。

### 功能
- 搜索官方技能
- 一键安装技能
- 自动更新检查
- 技能版本管理

### CLI 命令
```bash
magic skills hub search <query>   # 搜索 Hub
magic skills hub install <name>   # 安装技能
magic skills hub update           # 检查更新
```

## 扩展消息平台

支持更多消息网关集成。

### 新增平台
| 平台 | 描述 |
|------|------|
| WhatsApp | WhatsApp Business API |
| Signal | Signal 消息 |
| Matrix | Matrix 协议 |
| Email | 邮件网关 |
| SMS | 短信服务 |

### 配置示例
```yaml
gateway:
  platforms:
    - type: whatsapp
      phone_id: ${WHATSAPP_PHONE_ID}
      api_key: ${WHATSAPP_API_KEY}
    - type: signal
      signal_service: ${SIGNAL_SERVICE}
```

## LLM 文档生成

机器可读的文档索引。

### 生成的文件
- `llms.txt` - 精简索引 (~17 KB)
- `llms-full.txt` - 完整文档 (~1.8 MB)

### 用途
- LLM 上下文加载
- AI 代码助手集成
- 自动化文档处理

## 构建与部署

### 命令行二进制

```bash
# 构建 CLI (当前平台)
make build

# 多平台交叉编译
make build-all

# 安装
make install
```

### 多平台支持

go-magic 支持多种操作系统和架构的交叉编译。

#### 支持的平台

| 操作系统 | 架构 | 支持状态 |
|---------|------|----------|
| **Linux** | amd64, 386, armv6, arm64, riscv64, ppc64le, s390x | ✅ 完全支持 |
| **macOS** | amd64, arm64 (Apple Silicon) | ✅ 完全支持 |
| **Windows** | amd64, 386, arm64 | ✅ 完全支持 |
| **FreeBSD** | amd64, 386, arm | ✅ 完全支持 |
| **OpenBSD** | amd64, 386, arm | ✅ 完全支持 |
| **NetBSD** | amd64, 386, arm | ✅ 完全支持 |

#### 交叉编译脚本

```bash
# 列出所有支持的平台
./scripts/build-cross.sh list

# 构建通用平台 (Linux/macOS/Windows amd64+arm64)
./scripts/build-cross.sh common

# 构建所有平台
./scripts/build-cross.sh all

# 指定平台构建
./scripts/build-cross.sh linux-amd64 darwin-arm64 windows-amd64

# 构建并压缩
./scripts/build-cross.sh all --compress

# 生成校验和
./scripts/build-cross.sh all --checksum
```

#### Makefile 跨平台目标

```bash
make build-all      # 构建所有通用平台
make build-cross    # 构建所有支持平台
make build-linux    # 仅构建 Linux
make build-macos    # 仅构建 macOS
make build-windows  # 仅构建 Windows
```

#### GitHub Actions 自动发布

发布 tag 时自动构建并发布：
- Linux: amd64, 386, armv6, arm64, riscv64, ppc64le, s390x
- macOS: amd64, arm64
- Windows: 386, amd64, arm64
- Docker: linux/amd64, linux/arm64

### Web Dashboard

```bash
# 开发模式
make web-dev

# 构建前端
make web-build

# 启动 Web 服务
make web-start
```

### Docker 部署

```bash
# 单容器部署
docker-compose up -d

# 自定义端口
PORT=9000 docker-compose up -d
```

### 一键安装

```bash
# 默认安装 (下载二进制)
curl -fsSL https://raw.githubusercontent.com/magicwubiao/go-magic/main/scripts/install.sh | bash

# Homebrew 安装 (macOS/Linux)
curl -fsSL https://raw.githubusercontent.com/magicwubiao/go-magic/main/scripts/install.sh | bash -s -- --method homebrew

# Docker 安装
curl -fsSL https://raw.githubusercontent.com/magicwubiao/go-magic/main/scripts/install.sh | bash -s -- --method docker

# APT 安装 (Debian/Ubuntu)
curl -fsSL https://raw.githubusercontent.com/magicwubiao/go-magic/main/scripts/install.sh | bash -s -- --method apt
```

#### Windows 安装

**Scoop:**
```powershell
scoop bucket add magic https://github.com/magicwubiao/scoop-bucket
scoop install magic
```

**PowerShell:**
```powershell
irm https://raw.githubusercontent.com/magicwubiao/go-magic/main/scripts/windows/install.ps1 | iex
```

#### macOS Homebrew

```bash
brew tap magicwubiao/tap
brew install go-magic
```

#### Linux APT (Debian/Ubuntu)

```bash
curl -fsSL https://packages.magicwubiao.com/keys/public.gpg | sudo gpg --dearmor -o /etc/apt/trusted.gpg.d/magicwubiao.gpg
echo "deb [signed-by=/etc/apt/trusted.gpg.d/magicwubiao.gpg] https://packages.magicwubiao.com stable main" | sudo tee /etc/apt/sources.list.d/magicwubiao.list
sudo apt update && sudo apt install -y go-magic
```

### Web 套壳应用

```bash
# 构建桌面应用
./scripts/build.sh --platform all

# 或使用 Python 服务器
python3 scripts/web_wrapper.py --port 5000 --browser
```

## 多媒体处理

### Gateway 多媒体支持

Gateway 支持处理各种多媒体消息类型，包括语音、图片、视频和文件。

#### 平台统一消息格式
```go
type Message struct {
    ID        string                 `json:"id"`
    ChatID    string                 `json:"chat_id"`
    Text      string                 `json:"text"`
    Timestamp time.Time              `json:"timestamp"`
    From      User                   `json:"from"`
    Media     *Media                 `json:"media,omitempty"`
    Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type Media struct {
    Type     string `json:"type"`      // voice, photo, video, document, image
    URL      string `json:"url"`
    FileID   string `json:"file_id,omitempty"`
    MimeType string `json:"mime_type,omitempty"`
    Size     int64  `json:"size,omitempty"`
    Width    int    `json:"width,omitempty"`
    Height   int    `json:"height,omitempty"`
    Duration int    `json:"duration,omitempty"`
    Caption  string `json:"caption,omitempty"`
}
```

#### Telegram 多媒体处理
```go
// 支持的媒体类型
- voice: 语音消息 → 自动转录为文本
- photo: 图片消息 → Vision 模型分析
- video: 视频消息 → 提取缩略图
- document: 文件消息 → 保存到本地

// 配置示例
telegram:
  token: ${TELEGRAM_BOT_TOKEN}
  allowed_users:
    - user123
  voice_auto_transcribe: true
  photo_auto_analyze: true
```

#### Discord 多媒体处理
```go
// 支持的媒体类型
- 附件: 图片/音频/视频/文件
- 嵌入: Rich Embed 结构
- 表情: Emoji 响应

// 配置示例
discord:
  bot_token: ${DISCORD_BOT_TOKEN}
  guild_id: ${DISCORD_GUILD_ID}
```

### Voice 命令

```bash
# 语音模式（推送讲话）
magic voice listen

# 文字转语音
magic voice speak "Hello, world!"

# 测试语音配置
magic voice test

# 带参数使用
magic voice speak --provider openai --voice alloy "Hello"
```

### Vision 命令

```bash
# 分析图片
magic vision analyze image.png

# 分析网络图片
magic vision analyze https://example.com/image.jpg

# 比较两张图片
magic vision compare image1.png image2.png
```

### 扩展消息平台

支持的消息平台：

| 平台 | 类型 | 支持功能 |
|------|------|----------|
| WhatsApp | Business API | 文本、图片、音频、视频、文档 |
| Signal | REST API | 文本消息 |
| Matrix | Matrix Protocol | 文本、图片、文件 |
| Email | SMTP | 文本、附件 |
| SMS | 第三方 API | 文本消息 |

#### WhatsApp 配置
```yaml
whatsapp:
  phone_id: ${WHATSAPP_PHONE_ID}
  api_key: ${WHATSAPP_API_KEY}
  webhook_path: /webhook/whatsapp
```

#### Signal 配置
```yaml
signal:
  service_url: https://signal.example.com
  username: ${SIGNAL_USERNAME}
  password: ${SIGNAL_PASSWORD}
```

#### Matrix 配置
```yaml
matrix:
  homeserver: https://matrix.example.com
  user_id: @user:matrix.example.com
  access_token: ${MATRIX_ACCESS_TOKEN}
```

