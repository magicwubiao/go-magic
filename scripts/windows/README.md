# Windows 构建指南

## 前置条件

1. **Go 1.26+**（必需）—— <https://go.dev/dl/>
2. **Node.js 22+**（必需）—— Web UI 通过 `//go:embed dist` 打进二进制，
   而 `internal/server/dist` 被 `.gitignore` 忽略，所以**干净克隆必须先构建 Web UI**，
   否则 `go build` 会直接失败：`pattern dist: no matching files found`
   （下面的脚本都会自动完成这一步）
3. **Git**（可选，用于版本号检测）

## 快速构建

### 方式一：PowerShell（推荐）

```powershell
git clone https://github.com/magicwubiao/go-magic.git
cd go-magic

# 构建 Windows amd64 + arm64（自动先建 Web UI）
.\scripts\windows\build.ps1

# 跨平台构建（与 CI 发布矩阵一致，6 个平台）
.\scripts\windows\build-all.ps1

# 开发构建（保留调试符号，方便 dlv 调试）
.\scripts\windows\dev-build.ps1
```

### 方式二：命令提示符（CMD）

```cmd
git clone https://github.com/magicwubiao/go-magic.git
cd go-magic
.\scripts\windows\build.bat
```

### 方式三：源码编译并安装到用户目录

```cmd
.\scripts\windows\install.bat
```

### 方式四：手动构建

```cmd
cd web
npm ci
npm run build
cd ..
go build -ldflags="-s -w" -o magic.exe .\cmd\magic
```

## 产物

产物命名与 CI Release 资产**完全一致**，便于和发布的文件对应：

| 脚本 | 输出 |
| --- | --- |
| `build.ps1` / `build.bat` | `build\go-magic-windows-amd64.exe`、`build\go-magic-windows-arm64.exe` |
| `build-all.ps1` | `dist\go-magic-{linux,darwin,windows}-{amd64,arm64}[.exe]` |
| `dev-build.ps1` | `magic-dev.exe` |
| `install.bat` | `%USERPROFILE%\go-magic\magic.exe` |

> 版本号来自 `git describe`（与 CI 相同的 git tag 来源）。
> 可用 `-Version v0.5.19`（PowerShell）或环境变量 `VERSION` 覆盖；不再有硬编码的 `dev`/`1.0.0`。

## 使用

```cmd
:: 启动 Web 控制台
build\go-magic-windows-amd64.exe server

:: 启动网关（Telegram / Discord / Teams 等已启用的平台）
build\go-magic-windows-amd64.exe gateway start

:: 交互式对话
build\go-magic-windows-amd64.exe chat

:: 查看帮助 / 版本
build\go-magic-windows-amd64.exe --help
build\go-magic-windows-amd64.exe --version
```

配置目录为 `%USERPROFILE%\.magic`，与二进制所在目录无关。

## 常见问题

### “This app can't run on your PC”

架构选错了：64 位系统用 `amd64`，ARM64 设备（如 Surface Pro X）用 `arm64`。
查看方式：设置 > 系统 > 关于 > 系统类型。

### “Go is not recognized”

安装 Go 后重启终端，或临时加入 PATH：

```powershell
$env:Path += ";C:\Program Files\Go\bin"
```

### 报错 `pattern dist: no matching files found`

`internal/server/dist` 不存在（被 `.gitignore` 忽略）。先构建 Web UI：

```cmd
cd web
npm ci
npm run build
```

### 构建时提示 npm / Node 缺失

只影响 Web UI。若只是想验证 Go 代码，可先在前端产物存在的机器上构建，
或安装 Node.js 22+ 后重试（`build.ps1 -NoWeb` 可在 dist 已存在时跳过前端构建）。
