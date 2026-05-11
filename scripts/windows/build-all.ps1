# go-magic Windows Build All Platforms Script
# Usage: .\build-all.ps1

Write-Host "===================================" -ForegroundColor Cyan
Write-Host " go-magic Cross-Platform Build" -ForegroundColor Cyan
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

# Platforms to build
$Platforms = @(
    @{OS="linux"; Arch="amd64"; Output="magic-linux-amd64"},
    @{OS="linux"; Arch="arm64"; Output="magic-linux-arm64"},
    @{OS="darwin"; Arch="amd64"; Output="magic-darwin-amd64"},
    @{OS="darwin"; Arch="arm64"; Output="magic-darwin-arm64"},
    @{OS="windows"; Arch="amd64"; Output="magic-windows-amd64.exe"},
    @{OS="windows"; Arch="386"; Output="magic-windows-386.exe"}
)

$Count = $Platforms.Count
$Current = 0

foreach ($Platform in $Platforms) {
    $Current++
    Write-Host "[$Current/$Count] Building $($Platform.OS)-$($Platform.Arch)..." -ForegroundColor Yellow
    
    $Env:GOOS = $Platform.OS
    $Env:GOARCH = $Platform.Arch
    
    go build -ldflags="-s -w" -o "$DistDir\$($Platform.Output)" ./cmd/magic
    
    if ($LASTEXITCODE -eq 0) {
        $Size = (Get-Item "$DistDir\$($Platform.Output)").Length / 1MB
        Write-Host "  [OK] $DistDir\$($Platform.Output) ($([math]::Round($Size, 1)) MB)" -ForegroundColor Green
    } else {
        Write-Host "  [ERROR] Failed to build $($Platform.OS)-$($Platform.Arch)" -ForegroundColor Red
    }
    
    Remove-Item Env:GOOS
    Remove-Item Env:GOARCH
}

Write-Host ""
Write-Host "===================================" -ForegroundColor Cyan
Write-Host " All builds complete!" -ForegroundColor Green
Write-Host "===================================" -ForegroundColor Cyan
Write-Host ""

# List outputs
Write-Host "Built binaries:" -ForegroundColor Gray
Get-ChildItem $DistDir | ForEach-Object {
    $Size = $_.Length / 1MB
    Write-Host "  $($_.Name) ($([math]::Round($Size, 1)) MB)" -ForegroundColor Gray
}
