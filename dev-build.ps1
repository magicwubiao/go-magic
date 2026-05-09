# Build magic agent for current platform
# Usage: .\build.ps1 -Dev

$ErrorActionPreference = "Stop"
$Version = git describe --tags --always 2>$null
if (-not $Version) { $Version = "dev" }

Write-Host "Building magic v$Version for current platform..." -ForegroundColor Cyan
go build -ldflags "-X main.Version=$Version" -o .\magic.exe .\cmd\magic\

if ($LASTEXITCODE -ne 0) {
    Write-Host "Build FAILED!" -ForegroundColor Red
    exit 1
}
Write-Host "OK -> .\magic.exe" -ForegroundColor Green
