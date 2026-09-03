# go-magic Windows Build Script (PowerShell)
# Run with: .\build.ps1

param(
    [switch]$Clean,
    [string]$Output = "build"
)

$ErrorActionPreference = "Stop"

# Get project root
$ProjectRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
Set-Location $ProjectRoot

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  go-magic Windows Build Script" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# Check Go installation
try {
    $goVersion = go version
    Write-Host "[OK] Go installed: $goVersion" -ForegroundColor Green
} catch {
    Write-Host "[ERROR] Go is not installed or not in PATH" -ForegroundColor Red
    Write-Host "Please install Go from: https://go.dev/dl/" -ForegroundColor Yellow
    exit 1
}

# Clean if requested
if ($Clean -and (Test-Path $Output)) {
    Write-Host "[CLEAN] Removing old builds..." -ForegroundColor Yellow
    Remove-Item -Recurse -Force $Output
}

# Create output directory
if (-not (Test-Path $Output)) {
    New-Item -ItemType Directory -Path $Output | Out-Null
}

# Web is built directly to internal/server/dist by vite

# Get version
$version = "dev"
try {
    $gitVersion = git describe --always --tags 2>$null
    if ($gitVersion) { $version = $gitVersion }
} catch {}

# Build Windows amd64
Write-Host ""
Write-Host "[BUILD] Building Windows amd64..." -ForegroundColor Cyan
$ldflags = "-s -w -X main.Version=$version"
go build -ldflags $ldflags -o "$Output\magic-windows-amd64.exe" .\cmd\magic
if ($LASTEXITCODE -eq 0) {
    $size = (Get-Item "$Output\magic-windows-amd64.exe").Length / 1MB
    Write-Host "[OK] $Output\magic-windows-amd64.exe ($('{0:N1}' -f $size) MB)" -ForegroundColor Green
} else {
    Write-Host "[ERROR] Build failed" -ForegroundColor Red
    exit 1
}

# Build Windows 386
Write-Host ""
Write-Host "[BUILD] Building Windows 386..." -ForegroundColor Cyan
go build -ldflags $ldflags -o "$Output\magic-windows-386.exe" .\cmd\magic
if ($LASTEXITCODE -eq 0) {
    $size = (Get-Item "$Output\magic-windows-386.exe").Length / 1MB
    Write-Host "[OK] $Output\magic-windows-386.exe ($('{0:N1}' -f $size) MB)" -ForegroundColor Green
} else {
    Write-Host "[ERROR] Build failed" -ForegroundColor Red
    exit 1
}

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  Build Complete!" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "Output files:" -ForegroundColor White
Get-ChildItem "$Output\magic-windows*.exe" | ForEach-Object {
    $size = $_.Length / 1MB
    Write-Host "  $($_.Name) ($('{0:N1}' -f $size) MB)" -ForegroundColor White
}
Write-Host ""
Write-Host "Usage:" -ForegroundColor Yellow
Write-Host "  .\$Output\magic-windows-amd64.exe server" -ForegroundColor White
Write-Host "  .\$Output\magic-windows-amd64.exe gateway start" -ForegroundColor White
Write-Host ""