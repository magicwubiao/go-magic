# 交叉编译 Windows amd64 二进制
$ErrorActionPreference = "Stop"
$version = (git describe --tags --always 2>$null)
if (-not $version) { $version = "dev" }
Write-Output "Version: $version"
New-Item -ItemType Directory -Force -Path dist | Out-Null
$env:GOOS = "windows"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"
go build -ldflags="-s -w -X main.Version=$version" -o dist/go-magic-windows-amd64.exe ./cmd/magic
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
Get-Item dist/go-magic-windows-amd64.exe | Select-Object Name, Length, LastWriteTime
