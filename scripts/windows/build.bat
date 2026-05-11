@echo off
setlocal enabledelayedexpansion

echo ========================================
echo   go-magic Windows Build Script
echo ========================================
echo.

REM Check Go installation
where go >nul 2>&1
if %errorlevel% neq 0 (
    echo [ERROR] Go is not installed or not in PATH
    echo Please install Go from: https://go.dev/dl/
    exit /b 1
)

REM Get Go version
for /f "tokens=*" %%i in ('go version') do set GO_VERSION=%%i
echo [INFO] Go version: %GO_VERSION%

REM Set output directory
set OUTPUT_DIR=build
if not exist %OUTPUT_DIR% mkdir %OUTPUT_DIR%

REM Build Windows amd64
echo.
echo [BUILD] Building Windows amd64...
go build -ldflags="-s -w -X main.Version=dev" -o %OUTPUT_DIR%\magic-windows-amd64.exe .\cmd\magic
if %errorlevel% equ 0 (
    echo [OK] %OUTPUT_DIR%\magic-windows-amd64.exe
) else (
    echo [ERROR] Build failed
    exit /b 1
)

REM Build Windows 386
echo.
echo [BUILD] Building Windows 386...
go build -ldflags="-s -w -X main.Version=dev" -o %OUTPUT_DIR%\magic-windows-386.exe .\cmd\magic
if %errorlevel% equ 0 (
    echo [OK] %OUTPUT_DIR%\magic-windows-386.exe
) else (
    echo [ERROR] Build failed
    exit /b 1
)

echo.
echo ========================================
echo   Build Complete!
echo ========================================
echo.
echo Output files:
dir %OUTPUT_DIR%\magic-windows*.exe
echo.
echo Usage:
echo   build\magic-windows-amd64.exe server
echo   build\magic-windows-amd64.exe gateway start
echo.
