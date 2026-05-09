# Build demo-binary plugin for multiple platforms
# Usage: .\build.ps1

$ErrorActionPreference = "Stop"
$PluginDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$OutputDir = Join-Path $PluginDir "bin"

if (-not (Test-Path $OutputDir)) {
    New-Item -ItemType Directory -Path $OutputDir | Out-Null
}

Write-Host "Building demo-binary plugin..." -ForegroundColor Cyan

$platforms = @(
    @{GOOS="windows"; GOARCH="amd64"; Output="demo-binary.exe"}
    @{GOOS="linux"; GOARCH="amd64"; Output="demo-binary-linux"}
    @{GOOS="linux"; GOARCH="arm64"; Output="demo-binary-linux-arm64"}
    @{GOOS="darwin"; GOARCH="amd64"; Output="demo-binary-mac"}
    @{GOOS="darwin"; GOARCH="arm64"; Output="demo-binary-arm64"}
)

foreach ($p in $platforms) {
    $outPath = Join-Path $OutputDir $p.Output
    Write-Host "  Building $($p.GOOS)/$($p.GOARCH) -> $($p.Output)..." -ForegroundColor Yellow

    $env:GOOS = $p.GOOS
    $env:GOARCH = $p.GOARCH

    go build -ldflags "-s -w" -o $outPath (Join-Path $PluginDir "main.go")

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
Write-Host "Done! Binaries in: $OutputDir" -ForegroundColor Cyan
Get-ChildItem $OutputDir | ForEach-Object {
    $size = [math]::Round($_.Length / 1KB, 1)
    Write-Host ("  {0,-30} {1,8} KB" -f $_.Name, $size)
}
