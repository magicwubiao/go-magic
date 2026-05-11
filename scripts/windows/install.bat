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

REM Get project root directory (parent of scripts)
set PROJECT_ROOT=%~dp0..
cd /d "%PROJECT_ROOT%"

echo.
echo [1/3] Downloading dependencies...
go mod tidy
if errorlevel 1 (
    echo [ERROR] Failed to download dependencies
    pause
    exit /b 1
)
echo [OK] Dependencies downloaded

echo.
echo [2/3] Building project...
go build -o "%PROJECT_ROOT%\magic.exe" ./cmd/magic
if errorlevel 1 (
    echo [ERROR] Build failed
    pause
    exit /b 1
)
echo [OK] Build complete: %PROJECT_ROOT%\magic.exe

echo.
echo [3/3] Creating configuration...
echo NOTE: Run magic.exe --setup to configure your API keys and preferences

REM Create default config directory
if not exist "%USERPROFILE%\.go-magic" mkdir "%USERPROFILE%\.go-magic"

echo [OK] Configuration complete

echo.
echo ===================================
echo  Installation complete!
echo ===================================
echo.
echo Next steps:
echo   1. Run: magic.exe setup         # Configure API keys
echo   2. Run: magic.exe --help        # Show help
echo   3. Run: magic.exe chat          # Start chat
echo.
echo Or use from project root:
echo   go run cmd/magic/main.go chat
echo.
pause
