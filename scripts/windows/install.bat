@echo off
REM ============================================================================
REM go-magic Windows Install Script (build from source + install to user dir)
REM Usage: scripts\windows\install.bat
REM
REM Output: %USERPROFILE%\go-magic\magic.exe
REM NOTE: keep this file ASCII-only (cmd uses the OEM codepage).
REM ============================================================================
setlocal

REM Project root = two levels up from scripts\windows\ (this file's folder).
REM The previous version stripped only one level and ended up in scripts\,
REM so the "cmd\magic" check always failed.
set "SCRIPT_DIR=%~dp0"
pushd "%SCRIPT_DIR%..\.."
set "PROJECT_ROOT=%CD%"
popd
cd /d "%PROJECT_ROOT%"

echo ===================================
echo  go-magic Install (from source)
echo ===================================
echo.
echo [INFO] Project root: %PROJECT_ROOT%

if not exist "go.mod" (
    echo [ERROR] go.mod not found - wrong project root?
    echo Current directory: %CD%
    pause
    exit /b 1
)

where go >nul 2>&1
if errorlevel 1 (
    echo [ERROR] go not found in PATH. Install Go 1.26+: https://go.dev/dl/
    pause
    exit /b 1
)

for /f "tokens=*" %%i in ('go version') do set "GO_VERSION=%%i"
echo [ OK ] %GO_VERSION%

REM Version source: git tag (same as CI). No hardcoded version.
set "VERSION=dev"
for /f "delims=" %%v in ('git describe --tags --exact-match 2^>nul') do set "VERSION=%%v"
if "%VERSION%"=="dev" (
    for /f "delims=" %%v in ('git describe --tags --always 2^>nul') do set "VERSION=%%v"
)
echo [ OK ] Version: %VERSION%
echo.

echo [1/4] Building Web UI ^(required by go:embed dist^)...
if exist "internal\server\dist\index.html" (
    echo [ OK ] Web UI already built
    goto :deps
)

where npm >nul 2>&1
if errorlevel 1 (
    echo [ERROR] npm not found. Install Node.js 22+: https://nodejs.org/
    pause
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
    pause
    exit /b 1
)
call npm run build
if errorlevel 1 (
    echo [ERROR] npm run build failed
    popd
    pause
    exit /b 1
)
popd
echo [ OK ] Web UI built
echo.

:deps
echo [2/4] Downloading Go dependencies...
go mod download
if errorlevel 1 (
    echo [ERROR] go mod download failed
    pause
    exit /b 1
)
echo [ OK ] Dependencies downloaded
echo.

echo [3/4] Building magic.exe...
set "CGO_ENABLED=0"
go build -ldflags "-s -w -X main.Version=%VERSION%" -o magic.exe .\cmd\magic
if errorlevel 1 (
    echo [ERROR] Build failed
    echo Current directory: %CD%
    pause
    exit /b 1
)
echo [ OK ] Build completed: magic.exe
echo.

echo [4/4] Installing to user directory...
set "INSTALL_DIR=%USERPROFILE%\go-magic"
if not exist "%INSTALL_DIR%" mkdir "%INSTALL_DIR%"
copy /y magic.exe "%INSTALL_DIR%\magic.exe" >nul
if errorlevel 1 (
    echo [ERROR] Failed to copy magic.exe
    pause
    exit /b 1
)
del magic.exe >nul 2>&1
echo [ OK ] Installed: %INSTALL_DIR%\magic.exe
echo.

echo ===================================
echo  Installation Complete
echo ===================================
echo.
echo Run:
echo   %INSTALL_DIR%\magic.exe setup
echo   %INSTALL_DIR%\magic.exe chat
echo   %INSTALL_DIR%\magic.exe server
echo.
echo To use "magic" from anywhere, add this to PATH:
echo   %INSTALL_DIR%
echo.
echo Config dir: %USERPROFILE%\.magic
echo.

pause
endlocal
