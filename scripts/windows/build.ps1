# Build magic agent for multiple platforms
# Usage: .\build.ps1

$ErrorActionPreference = "Stop"
$Project = "github.com/magicwubiao/go-magic"
$OutputDir = ".\build"

if (-not (Test-Path $OutputDir)) {
    New-Item -ItemType Directory -Path $OutputDir | Out-Null
}

# Get version from git tag or use "dev"
$Version = git describe --tags --always 2>$null
if (-not $Version) { $Version = "dev" }

Write-Host "Building magic agent v$Version..." -ForegroundColor Cyan

$platforms = @(
    @{GOOS="windows"; GOARCH="amd64"; Ext=".exe"},
    @{GOOS="linux"; GOARCH="amd64"; Ext=""},
    @{GOOS="darwin"; GOARCH="amd64"; Ext=""},
    @{GOOS="darwin"; GOARCH="arm64"; Ext=""}
)

foreach ($p in $platforms) {
    $output = "$OutputDir\magic-$($p.GOOS)-$($p.GOARCH)$($p.Ext)"
    Write-Host "  Building $output..." -ForegroundColor Yellow
    
    $env:GOOS = $p.GOOS
    $env:GOARCH = $p.GOARCH
    
    go build -ldflags "-s -w -X main.Version=$Version" -o $output ./cmd/magic/
    
    if ($LASTEXITCODE -ne 0) {
        Write-Host "  FAILED!" -ForegroundColor Red
        exit 1
    }
    
    Write-Host "  OK" -ForegroundColor Green
}

# Reset env
$env:GOOS = ""
$env:GOARCH = ""

Write-Host ""
Write-Host "Build complete! Outputs:" -ForegroundColor Cyan
Get-ChildItem $OutputDir | ForEach-Object { Write-Host "  $($_.Name)" }
