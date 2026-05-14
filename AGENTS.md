# Go Magic Agent

高性能、超轻量级的 Go 实现的 AI Agent。

## 技术栈
- **语言**：Go 1.25+
- **数据库**：SQLite
- **前端**：React + TypeScript + Vite
- **包管理**：go modules

## 关键入口
- **主入口**：`cmd/magic/main.go`
- **构建**：`make build`
- **运行**：`go run cmd/magic/main.go <command>`

## Web Dashboard
- **启动**：`magic server --port 5000`
- **前端**：`web/` 目录
- **嵌入目录**：`internal/server/dist/`

## 常用命令
```bash
magic chat              # 聊天
magic server            # 启动 Web 服务
magic sessions list     # 会话管理
magic skills list       # 技能列表
magic tools list        # 工具列表
magic doctor            # 诊断
```

## API 端点
- `/api/sessions` - 会话管理
- `/api/chat` - 聊天（支持 tool_calls）
- `/api/tools/toolsets` - 工具集
- `/api/skills` - 技能
- `/api/dashboard/themes` - 主题
- `/api/system/info` - 系统信息

## 预览
- **支持**：预览型 web 项目
- **Web Dashboard**：`http://localhost:5000`
- **预览构建**：`bash scripts/coze-preview-build.sh`
- **预览运行**：`bash scripts/coze-preview-run.sh`
- **端口绑定**：`0.0.0.0:5000`

## 环境要求
- **Go 版本**：1.25+（需要使用 GOTOOLCHAIN=local 或安装 Go 1.25）
- **前端依赖**：pnpm
- **Go 代理**：需要配置 GOPROXY（建议使用 goproxy.cn）

## .coze 配置
- **项目类型**：web
- **运行时**：golang-1.25
- **部署类型**：service/web
- **入口**：`cmd/magic/main.go`

## REPL 命令行人机交互

参考 DeepSeek-TUI 优化了交互体验：

### 渲染组件
- **MarkdownRenderer** (`repl_renderer.go`) - Markdown 流式渲染，支持标题、代码块、列表、链接等
- **ToolCallRenderer** - 工具调用状态显示，带进度条和耗时统计
- **CostRenderer** - 实时成本/Token 统计显示
- **ThinkingRenderer** - 思考过程渲染器
- **StatusBarRenderer** - 底部状态栏渲染

### 补全系统
- **Completer** (`repl_completion.go`) - 命令/上下文自动补全
- 支持 `/` 命令补全、`@` 技能/工具补全、`model:` 模型补全
- 文件路径补全、命令历史补全

### 快捷键
| 键 | 功能 |
|---|---|
| Tab | 自动补全 |
| ↑↓ | 历史记录导航 |
| Ctrl+C | 中断生成 |
| Ctrl+L | 清屏 |
| Shift+Tab | 循环切换思考模式 |

### 命令
| 命令 | 功能 |
|------|------|
| `/help` | 显示帮助 |
| `/new` | 新对话 |
| `/clear` | 清屏 |
| `/usage` | 使用统计 |
| `/tools` | 工具列表 |
| `/skills` | 技能列表 |
| `/stream` | 切换流式输出 |
| `/think [off/high/max]` | 设置思考模式 |
| `/context` | 上下文信息 |
| `/save` / `/load` | 会话保存/加载 |
| `/stop` | 停止生成 |
| `/undo` / `/retry` | 撤销/重试 |
