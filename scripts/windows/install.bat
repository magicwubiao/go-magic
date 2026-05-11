@echo off
REM go-magic Windows Install Script
REM Usage: .\install.bat

echo ===================================
echo  go-magic Install Script
echo ===================================
echo.

REM Get project root directory
set PROJECT_ROOT=%~dp0..
cd /d "%PROJECT_ROOT%"

REM Check Go installation
where go >nul 2>&1
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Go is not installed or not in PATH
    echo Please install Go from: https://go.dev/dl/
    pause
    exit /b 1
)

for /f "tokens=*" %%i in ('go version') do set GO_VERSION=%%i
echo [OK] Go installed:
echo     %GO_VERSION%
echo.

REM Download dependencies
echo [1/3] Downloading dependencies...
go mod download
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Failed to download dependencies
    pause
    exit /b 1
)
echo [OK] Dependencies downloaded
echo.

REM Build project
echo [2/3] Building project...
go build -ldflags="-s -w" -o magic.exe ./cmd/magic
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Build failed
    pause
    exit /b 1
)
echo [OK] Build completed: magic.exe
echo.

REM Installation
echo [3/3] Installing...
set INSTALL_DIR=%USERPROFILE%\go-magic
if not exist "%INSTALL_DIR%" mkdir "%INSTALL_DIR%"
copy magic.exe "%INSTALL_DIR%\magic.exe" >nul
copy README.md "%INSTALL_DIR%\README.md" >nul 2>nul
copy LICENSE "%INSTALL_DIR%\LICENSE" >nul 2>nul
echo [OK] Installed to: %INSTALL_DIR%
echo.
echo ===================================
echo  Installation complete!
echo ===================================
echo.
echo To use, add to PATH:
echo   set PATH=%%PATH%%;%INSTALL_DIR%
echo.
echo Or run directly:
echo   %INSTALL_DIR%\magic.exe
echo.

REM Cleanup
del magic.exe
pause
