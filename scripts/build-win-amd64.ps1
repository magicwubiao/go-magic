# Build go-magic for windows/amd64 (PowerShell)
$ErrorActionPreference = "Stop"
Set-Location (Split-Path $PSScriptRoot -Parent)

$version = git describe --tags --always
if (-not $version) { $version = "dev" }
$commit = git rev-parse --short HEAD
if (-not $commit) { $commit = "unknown" }
$date = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")

$env:GOOS = "windows"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"

Write-Host "Version: $version  Commit: $commit  BuildDate: $date"
go build -ldflags="-s -w -X main.Version=$version -X main.Commit=$commit -X main.BuildDate=$date" -o dist\go-magic-windows-amd64.exe .\cmd\magic

if ($LASTEXITCODE -ne 0) {
    Write-Host "BUILD FAILED: exit code $LASTEXITCODE"
    exit $LASTEXITCODE
}

$f = Get-Item dist\go-magic-windows-amd64.exe
Write-Host ("BUILD OK: {0} ({1} MB)" -f $f.FullName, [math]::Round($f.Length / 1MB, 2))
