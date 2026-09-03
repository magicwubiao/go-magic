@echo off
REM go-magic Windows Install Script
REM Usage: Run from project root or any subdirectory

echo ===================================
echo  go-magic Install Script
echo ===================================
echo.

REM Get project root directory (parent of scripts folder)
set SCRIPT_DIR=%~dp0
set PROJECT_ROOT=%SCRIPT_DIR:~0,-1%
for %%i in ("%PROJECT_ROOT%") do set PROJECT_ROOT=%%~dpi
set PROJECT_ROOT=%PROJECT_ROOT:~0,-1%

echo [INFO] Project root: %PROJECT_ROOT%
cd /d "%PROJECT_ROOT%"

REM Verify we're in the right place
if not exist "cmd\magic" (
    echo [ERROR] Cannot find cmd\magic directory
    echo Current directory: %CD%
    echo Expected: %PROJECT_ROOT%
    pause
    exit /b 1
)

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
    echo Current directory: %CD%
    pause
    exit /b 1
)
echo [OK] Build completed: magic.exe
echo.

REM Installation
echo [3/3] Installing to user directory...
set INSTALL_DIR=%USERPROFILE%\go-magic
if not exist "%INSTALL_DIR%" mkdir "%INSTALL_DIR%"
copy magic.exe "%INSTALL_DIR%\" >nul
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Failed to copy files
    pause
    exit /b 1
)
echo [OK] Installed to: %INSTALL_DIR%
echo.

echo ===================================
echo  Installation Complete!
echo ===================================
echo.
echo Run with: %INSTALL_DIR%\magic.exe
echo Or add to PATH and run: magic
echo.

REM Cleanup
del magic.exe >nul 2>&1

pause
