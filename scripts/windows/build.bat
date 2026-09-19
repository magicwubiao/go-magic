@echo off
REM ============================================================================
REM go-magic Windows Build Script (Command Prompt)
REM Usage: scripts\windows\build.bat
REM
REM Output: build\go-magic-windows-amd64.exe, build\go-magic-windows-arm64.exe
REM NOTE: keep this file ASCII-only (cmd uses the OEM codepage).
REM ============================================================================
setlocal

set "SCRIPT_DIR=%~dp0"
pushd "%SCRIPT_DIR%..\.."
set "PROJECT_ROOT=%CD%"
popd
cd /d "%PROJECT_ROOT%"

echo ========================================
echo   go-magic Windows Build
echo ========================================
echo.

where go >nul 2>&1
if errorlevel 1 (
    echo [ERROR] go not found in PATH. Install Go 1.26+: https://go.dev/dl/
    exit /b 1
)

for /f "tokens=*" %%i in ('go version') do set "GO_VERSION=%%i"
echo [INFO] %GO_VERSION%

REM Version source: git tag (same as CI). No hardcoded version.
set "VERSION=dev"
for /f "delims=" %%v in ('git describe --tags --exact-match 2^>nul') do set "VERSION=%%v"
if "%VERSION%"=="dev" (
    for /f "delims=" %%v in ('git describe --tags --always 2^>nul') do set "VERSION=%%v"
)
echo [INFO] Version: %VERSION%

set "OUTPUT_DIR=build"
if not exist "%OUTPUT_DIR%" mkdir "%OUTPUT_DIR%"

REM internal/server/dist is the //go:embed target and is gitignored, so a fresh
REM clone must build the Web UI first, otherwise go build fails with
REM "pattern dist: no matching files found".
if exist "internal\server\dist\index.html" goto :build

echo [INFO] Building Web UI ^(required by go:embed dist^)...
where npm >nul 2>&1
if errorlevel 1 (
    echo [ERROR] npm not found. Install Node.js 22+: https://nodejs.org/
    exit /b 1
)

pushd web
call npm ci
if errorlevel 1 (
    echo [WARN] npm ci failed, retrying with --legacy-peer-deps
    call npm ci --legacy-peer-deps
)
if errorlevel 1 (
    echo [ERROR] npm dependency install failed
    popd
    exit /b 1
)
call npm run build
if errorlevel 1 (
    echo [ERROR] npm run build failed
    popd
    exit /b 1
)
popd

if not exist "internal\server\dist\index.html" (
    echo [ERROR] Web UI build did not produce internal\server\dist\index.html
    exit /b 1
)

:build
set "LDFLAGS=-s -w -X main.Version=%VERSION%"
set "CGO_ENABLED=0"

set "GOOS=windows"
set "GOARCH=amd64"
echo.
echo [BUILD] windows/amd64
go build -ldflags "%LDFLAGS%" -o "%OUTPUT_DIR%\go-magic-windows-amd64.exe" .\cmd\magic
if errorlevel 1 (
    echo [ERROR] windows/amd64 build failed
    exit /b 1
)
echo [ OK ] %OUTPUT_DIR%\go-magic-windows-amd64.exe

set "GOARCH=arm64"
echo.
echo [BUILD] windows/arm64
go build -ldflags "%LDFLAGS%" -o "%OUTPUT_DIR%\go-magic-windows-arm64.exe" .\cmd\magic
if errorlevel 1 (
    echo [ERROR] windows/arm64 build failed
    exit /b 1
)
echo [ OK ] %OUTPUT_DIR%\go-magic-windows-arm64.exe

set "GOOS="
set "GOARCH="
set "CGO_ENABLED="

echo.
echo ========================================
echo   Build Complete
echo ========================================
echo.
dir /b "%OUTPUT_DIR%\go-magic-windows-*.exe"
echo.
echo Run:
echo   %OUTPUT_DIR%\go-magic-windows-amd64.exe server
echo.
echo Config dir: %USERPROFILE%\.magic
echo.

endlocal
