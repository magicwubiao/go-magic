## 项目概述
go-magic 是一个高性能、超轻量级的 AI Agent 框架，使用 Go 语言编写，灵感来自 Nous Research 的 hermes-agent。

## 技术栈
- **后端**：Go 1.25
- **前端**：React 19 + TypeScript + Vite 7 + Tailwind CSS 4
- **数据库**：SQLite (modernc.org/sqlite)
- **包管理器**：pnpm (前端) / Go modules (后端)

## 目录结构
```
/workspace/projects/
├── cmd/magic/main.go          # 主入口
├── internal/                   # 内部包
│   └── server/                 # Web 服务器
│       └── dist/               # 嵌入式前端产物
├── pkg/                        # 公共包
├── plugins/                    # 插件系统
├── web/                        # 前端源码
│   ├── src/                    # React 组件
│   ├── dist/                   # 构建产物
│   └── package.json
├── scripts/                    # 脚本
│   ├── build.sh               # 构建脚本
│   ├── run.sh                 # 运行脚本
│   ├── coze-preview-build.sh  # Coze 预览构建
│   └── coze-preview-run.sh    # Coze 预览运行
└── build/                     # 构建产物 (magic 二进制)
```

## 关键入口
- **主程序**：`cmd/magic/main.go`
- **预览构建**：`scripts/coze-preview-build.sh`
- **预览运行**：`scripts/coze-preview-run.sh`
- **部署构建**：`build.sh all`
- **部署运行**：`scripts/run.sh`

## 运行与预览
### 环境要求
- Go 1.25+
- Node.js (前端构建)
- pnpm

### 本地预览
```bash
# 1. 安装依赖
cd web && pnpm install && cd ..
export PATH="$PATH:/usr/local/go/bin"

# 2. 构建并运行预览
bash scripts/coze-preview-build.sh
bash scripts/coze-preview-run.sh
# 服务运行在 http://localhost:5000
```

### 部署构建
```bash
export PATH="$PATH:/usr/local/go/bin"
bash build.sh all
```

## 用户偏好与长期约束
1. **Go 路径**：运行脚本中需要显式设置 `export PATH="$PATH:/usr/local/go/bin"`，因为 Go 可能不在系统默认 PATH 中
2. **端口固定**：Web 服务固定使用 5000 端口
3. **构建产物**：Go 二进制输出到 `build/magic`，Web UI 嵌入 `internal/server/dist/`
4. **预览脚本幂等性**：重复执行会先清理 5000 端口再重启
5. **Go 代理**：使用 `GOPROXY=https://goproxy.cn,direct` 避免官方代理超时

## 常见问题和预防
1. **Go 未找到**：确保 `.bashrc` 中配置了 Go 路径，或在脚本中显式设置
2. **pnpm 锁文件**：前端使用 `--frozen-lockfile`，需确保 pnpm-lock.yaml 存在
3. **前端依赖警告**：esbuild 和 unicode-animations 的构建脚本被忽略，使用 `pnpm approve-builds` 可启用
4. **Go 代理超时**：使用 `GOPROXY=https://goproxy.cn,direct` 避免官方代理超时
5. **web 分支编译错误**：`cmd/magic/chat.go` 中有未定义的 `RunTUI` 调用，需注释掉相关代码

## 分支说明
- `main`：完整功能分支
- `web`：简化版本，移除了部分功能（如 TUI），专注 Web 服务
