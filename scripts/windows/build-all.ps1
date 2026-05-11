$ErrorActionPreference = "Stop"

# Change to project root
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectRoot = Split-Path -Parent $ScriptDir
Set-Location $ProjectRoot

$VERSION = "dev"
$COMMIT = "unknown"
$DATE = "unknown"
$LDFLAGS = "-X github.com/magicwubiao/go-magic/cmd/magic.Version=$VERSION -X github.com/magicwubiao/go-magic/cmd/magic.Commit=$COMMIT -X github.com/magicwubiao/go-magic/cmd/magic.BuildDate=$DATE"

New-Item -ItemType Directory -Force -Path build | Out-Null

Write-Host "==> Building linux/amd64..."
$env:GOOS = "linux"; $env:GOARCH = "amd64"
go build -ldflags="$LDFLAGS" -o build/magic-linux-amd64 ./cmd/magic
Write-Host "    Done: build/magic-linux-amd64"

Write-Host "==> Building darwin/amd64..."
$env:GOOS = "darwin"; $env:GOARCH = "amd64"
go build -ldflags="$LDFLAGS" -o build/magic-darwin-amd64 ./cmd/magic
Write-Host "    Done: build/magic-darwin-amd64"

Write-Host "==> Building darwin/arm64..."
$env:GOOS = "darwin"; $env:GOARCH = "arm64"
go build -ldflags="$LDFLAGS" -o build/magic-darwin-arm64 ./cmd/magic
Write-Host "    Done: build/magic-darwin-arm64"

Write-Host "==> Building windows/amd64..."
$env:GOOS = "windows"; $env:GOARCH = "amd64"
go build -ldflags="$LDFLAGS" -o build/magic-windows-amd64.exe ./cmd/magic
Write-Host "    Done: build/magic-windows-amd64.exe"

Remove-Item Env:\GOOS -ErrorAction SilentlyContinue
Remove-Item Env:\GOARCH -ErrorAction SilentlyContinue

Write-Host ""
Write-Host "==> All builds completed!"
Get-ChildItem build/ | Format-Table Name, @{Name="Size(MB)"; Expression={[math]::Round($_.Length/1MB, 2)}}
