@echo off
echo ===================================
echo  go-magic 安装脚本
echo ===================================
echo.

REM 检查 Go 是否安装
go version >nul 2>&1
if errorlevel 1 (
    echo [错误] Go 未安装或不在 PATH 中
    echo 请先安装 Go 1.21 或更高版本
    echo 下载地址: https://go.dev/dl/
    pause
    exit /b 1
)

echo [✓] Go 已安装:
go version

echo.
echo [1/3] 下载依赖...
cd /d "%~dp0"
go mod tidy
if errorlevel 1 (
    echo [错误] 依赖下载失败
    pause
    exit /b 1
)
echo [✓] 依赖下载完成

echo.
echo [2/3] 构建项目...
go build -o magic.exe ./cmd/magic
if errorlevel 1 (
    echo [错误] 构建失败
    pause
    exit /b 1
)
echo [✓] 构建完成: magic.exe

echo.
echo [3/3] 创建配置文件...
magic.exe setup <nul 2>nul
echo [✓] 配置完成

echo.
echo ===================================
echo  安装完成!
echo ===================================
echo.
echo 使用方法:
echo   magic.exe --help       # 查看帮助
echo   magic.exe chat         # 开始聊天
echo   magic.exe doctor      # 诊断
echo.
pause
