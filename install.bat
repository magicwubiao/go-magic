@echo off
echo ===================================
echo  go-magic Install Script
echo ===================================
echo.

REM Check if Go is installed
go version >nul 2>&1
if errorlevel 1 (
    echo [ERROR] Go is not installed or not in PATH
    echo Please install Go 1.21 or later
    echo Download: https://go.dev/dl/
    pause
    exit /b 1
)

echo [OK] Go installed:
go version

echo.
echo [1/3] Downloading dependencies...
cd /d "%~dp0"
go mod tidy
if errorlevel 1 (
    echo [ERROR] Failed to download dependencies
    pause
    exit /b 1
)
echo [OK] Dependencies downloaded

echo.
echo [2/3] Building project...
go build -o magic.exe ./cmd/magic
if errorlevel 1 (
    echo [ERROR] Build failed
    pause
    exit /b 1
)
echo [OK] Build complete: magic.exe

echo.
echo [3/3] Creating configuration...
magic.exe setup <nul 2>nul
echo [OK] Configuration complete

echo.
echo ===================================
echo  Installation complete!
echo ===================================
echo.
echo Usage:
echo   magic.exe --help       # Show help
echo   magic.exe chat         # Start chat
echo   magic.exe doctor       # Run diagnostics
echo.
pause
