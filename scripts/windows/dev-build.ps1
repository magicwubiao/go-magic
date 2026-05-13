# go-magic Windows Dev Build Script
# Usage: .\dev-build.ps1

Write-Host "===================================" -ForegroundColor Cyan
Write-Host " go-magic Dev Build (with debug)" -ForegroundColor Cyan
Write-Host "===================================" -ForegroundColor Cyan
Write-Host ""

# Get project root
$ProjectRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
Set-Location $ProjectRoot

Write-Host "[INFO] Project root: $PWD" -ForegroundColor Gray

# Check for uncommitted changes
$Status = git status --porcelain
if ($Status) {
    Write-Host "[WARN] You have uncommitted changes:" -ForegroundColor Yellow
    Write-Host $Status -ForegroundColor Gray
    Write-Host ""
}

# Build with debug symbols and no optimization
Write-Host "[INFO] Building for development..." -ForegroundColor Yellow
go build -gcflags="all=-N -l" -o magic-dev.exe ./cmd/magic

if ($LASTEXITCODE -eq 0) {
    $Size = (Get-Item "magic-dev.exe").Length / 1MB
    Write-Host "[OK] Dev build complete: magic-dev.exe ($([math]::Round($Size, 1)) MB)" -ForegroundColor Green
    Write-Host ""
    Write-Host "Run with: .\magic-dev.exe" -ForegroundColor Cyan
    Write-Host "For debugging, use: dlv debug ./cmd/magic" -ForegroundColor Gray
} else {
    Write-Host "[ERROR] Build failed" -ForegroundColor Red
    exit 1
}