# go-magic Windows 构建脚本 (PowerShell)
# 用法: .\scripts\windows\build.ps1 [-Clean] [-Output build] [-Version v0.5.19] [-NoWeb]
#
# 产物命名与 CI 一致: go-magic-windows-amd64.exe / go-magic-windows-arm64.exe
# 脚本会自动先构建 Web UI（internal/server/dist 是 go:embed 的目标，干净克隆上缺失会导致编译失败）

param(
    [switch]$Clean,
    [string]$Output = "build",
    [string]$Version = "",
    [switch]$NoWeb
)

$ErrorActionPreference = "Stop"

. "$PSScriptRoot\..\lib\common.ps1"

$RepoRoot = Get-GmRepoRoot -PSScriptRootPath $PSScriptRoot
Set-Location $RepoRoot

if ($Version) { $env:VERSION = $Version }

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  go-magic Windows Build" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-GmError "未找到 go，请先安装 Go 1.26+ 并加入 PATH: https://go.dev/dl/"
    exit 1
}

$version = Get-GmVersion
Write-GmInfo "项目根目录: $RepoRoot"
Write-GmInfo "Go: $(go version)"
Write-GmInfo "版本: $version"

if ($Clean -and (Test-Path $Output)) {
    Write-GmInfo "清理旧构建: $Output"
    Remove-Item -Recurse -Force $Output
}
if (-not (Test-Path $Output)) {
    New-Item -ItemType Directory -Path $Output | Out-Null
}

# Web UI 必须先构建，否则 go:embed dist 找不到文件
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

# 与 CI 的 windows 矩阵一致: amd64, arm64（不再构建已无人使用的 386）
$architectures = @("amd64", "arm64")
$failures = @()

foreach ($arch in $architectures) {
    $out = Join-Path $Output "go-magic-windows-$arch.exe"
    Write-Host ""
    Write-GmInfo "编译 windows/$arch -> go-magic-windows-$arch.exe"

    $code = Invoke-GmGoBuild -RepoRoot $RepoRoot -GoOs "windows" -GoArch $arch -Version $version -Output $out
    if ($code -eq 0 -and (Test-Path $out)) {
        $sizeMb = [math]::Round((Get-Item $out).Length / 1MB, 1)
        Write-GmOk "$out ($sizeMb MB)"
    } else {
        Write-GmError "windows/$arch 编译失败"
        $failures += $arch
    }
}

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
if ($failures.Count -gt 0) {
    Write-GmError "失败平台: $($failures -join ', ')"
    exit 1
}
Write-Host "  Build Complete!" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "产物:" -ForegroundColor White
Get-ChildItem "$Output\go-magic-windows-*.exe" -ErrorAction SilentlyContinue | ForEach-Object {
    Write-Host "  $($_.Name) ($([math]::Round($_.Length / 1MB, 1)) MB)" -ForegroundColor White
}
Write-Host ""
Write-Host "运行:" -ForegroundColor Yellow
Write-Host "  .\$Output\go-magic-windows-amd64.exe server" -ForegroundColor White
Write-Host "  .\$Output\go-magic-windows-amd64.exe gateway start" -ForegroundColor White
Write-Host ""
Write-Host "配置目录: $env:USERPROFILE\.magic" -ForegroundColor Gray
