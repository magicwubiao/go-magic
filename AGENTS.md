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
