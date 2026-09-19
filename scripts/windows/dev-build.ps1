# go-magic 开发构建脚本 (PowerShell, 带调试符号)
# 用法: .\scripts\windows\dev-build.ps1 [-Version v0.5.19]
#
# 与发布构建的区别: 关闭优化并保留调试符号（-gcflags="all=-N -l"），
# 便于使用 dlv 调试。产物: magic-dev.exe

param(
    [string]$Version = "",
    [switch]$NoWeb
)

$ErrorActionPreference = "Stop"

. "$PSScriptRoot\..\lib\common.ps1"

$RepoRoot = Get-GmRepoRoot -PSScriptRootPath $PSScriptRoot
Set-Location $RepoRoot

if ($Version) { $env:VERSION = $Version }
$version = Get-GmVersion

Write-Host "=======================================" -ForegroundColor Cyan
Write-Host " go-magic Dev Build (with debug)" -ForegroundColor Cyan
Write-Host "=======================================" -ForegroundColor Cyan
Write-Host ""
Write-GmInfo "项目根目录: $RepoRoot"
Write-GmInfo "版本: $version"

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-GmError "未找到 go，请先安装 Go 1.26+: https://go.dev/dl/"
    exit 1
}

# 未提交改动提醒（不影响构建）
try {
    $status = & git status --porcelain
    if ($status) {
        Write-GmWarn "存在未提交改动:"
        $status | ForEach-Object { Write-Host "  $_" -ForegroundColor Gray }
    }
} catch { }

# Web UI 必须先构建（go:embed dist）
if ($NoWeb) {
    if (-not (Test-GmWebDist $RepoRoot)) {
        Write-GmError "--NoWeb 但 internal\server\dist\index.html 不存在，go:embed dist 会失败"
        exit 1
    }
    Write-GmOk "跳过 Web UI 构建 (-NoWeb)"
} else {
    try {
        Build-GmWebDist -RepoRoot $RepoRoot
    } catch {
        Write-GmError $_.Exception.Message
        exit 1
    }
}

$out = "magic-dev.exe"
Write-GmInfo "编译（调试符号）-> $out"

$previousCgo = $env:CGO_ENABLED
try {
    $env:CGO_ENABLED = "0"
    $ldflags = Get-GmLdflags -Version $version
    & go build -gcflags="all=-N -l" -ldflags "$ldflags" -o $out ./cmd/magic
    $code = $LASTEXITCODE
} finally {
    $env:CGO_ENABLED = $previousCgo
}

if ($code -eq 0 -and (Test-Path $out)) {
    $sizeMb = [math]::Round((Get-Item $out).Length / 1MB, 1)
    Write-GmOk "构建成功: $out ($sizeMb MB)"
    Write-Host ""
    Write-Host "运行: .\$out" -ForegroundColor Cyan
    Write-Host "调试: dlv debug ./cmd/magic" -ForegroundColor Gray
} else {
    Write-GmError "构建失败"
    exit 1
}
