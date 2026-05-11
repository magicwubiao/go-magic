# go-magic Windows Build Script
# Usage: .\build.ps1

Write-Host "===================================" -ForegroundColor Cyan
Write-Host " go-magic Build Script" -ForegroundColor Cyan
Write-Host "===================================" -ForegroundColor Cyan
Write-Host ""

# Get project root
$ProjectRoot = Split-Path -Parent $PSScriptRoot
Set-Location $ProjectRoot

Write-Host "[INFO] Project root: $PWD" -ForegroundColor Gray

# Create dist directory
$DistDir = "dist"
if (-not (Test-Path $DistDir)) {
    New-Item -ItemType Directory -Path $DistDir | Out-Null
}

# Build for Windows AMD64
Write-Host "[INFO] Building for Windows AMD64..." -ForegroundColor Yellow
go build -ldflags="-s -w" -o "$DistDir\magic-windows-amd64.exe" ./cmd/magic

if ($LASTEXITCODE -eq 0) {
    $Size = (Get-Item "$DistDir\magic-windows-amd64.exe").Length / 1MB
    Write-Host "[OK] Built: $DistDir\magic-windows-amd64.exe ($([math]::Round($Size, 1)) MB)" -ForegroundColor Green
} else {
    Write-Host "[ERROR] Build failed" -ForegroundColor Red
    exit 1
}

Write-Host ""
Write-Host "===================================" -ForegroundColor Cyan
Write-Host " Build complete!" -ForegroundColor Green
Write-Host "===================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "Output: $DistDir\magic-windows-amd64.exe" -ForegroundColor Gray
