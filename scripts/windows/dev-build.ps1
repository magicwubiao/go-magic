# go-magic development build script (PowerShell, with debug symbols)
# Usage: .\scripts\windows\dev-build.ps1 [-Version v0.5.19]
#
# Difference from the release build: optimizations are disabled and debug symbols
# are kept (-gcflags="all=-N -l"), which makes dlv debugging possible.
# Artifact: magic-dev.exe

param(
    [string]$Version = "",
    [switch]$NoWeb
)

$ErrorActionPreference = "Stop"

. "$PSScriptRoot\..\lib\common.ps1"

$RepoRoot = Get-GmRepoRoot -PSScriptRootPath $PSScriptRoot
Set-Location $RepoRoot

if ($Version) { $env:VERSION = $Version }
$version = Get-GmVersion

Write-Host "=======================================" -ForegroundColor Cyan
Write-Host " go-magic Dev Build (with debug)" -ForegroundColor Cyan
Write-Host "=======================================" -ForegroundColor Cyan
Write-Host ""
Write-GmInfo "repository root: $RepoRoot"
Write-GmInfo "version: $version"

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-GmError "go not found; please install Go 1.26+: https://go.dev/dl/"
    exit 1
}

# Uncommitted changes notice (does not affect the build)
try {
    $status = & git status --porcelain
    if ($status) {
        Write-GmWarn "uncommitted changes:"
        $status | ForEach-Object { Write-Host "  $_" -ForegroundColor Gray }
    }
} catch { }

# The Web UI must be built first (go:embed dist)
if ($NoWeb) {
    if (-not (Test-GmWebDist $RepoRoot)) {
        Write-GmError "-NoWeb given but internal\server\dist\index.html is missing; go:embed dist would fail"
        exit 1
    }
    Write-GmOk "skipping the Web UI build (-NoWeb)"
} else {
    try {
        Build-GmWebDist -RepoRoot $RepoRoot
    } catch {
        Write-GmError $_.Exception.Message
        exit 1
    }
}

$out = "magic-dev.exe"
Write-GmInfo "building (debug symbols) -> $out"

$previousCgo = $env:CGO_ENABLED
try {
    $env:CGO_ENABLED = "0"
    $ldflags = Get-GmLdflags -Version $version
    & go build -gcflags="all=-N -l" -ldflags "$ldflags" -o $out ./cmd/magic
    $code = $LASTEXITCODE
} finally {
    $env:CGO_ENABLED = $previousCgo
}

if ($code -eq 0 -and (Test-Path $out)) {
    $sizeMb = [math]::Round((Get-Item $out).Length / 1MB, 1)
    Write-GmOk "build succeeded: $out ($sizeMb MB)"
    Write-Host ""
    Write-Host "Run:   .\$out" -ForegroundColor Cyan
    Write-Host "Debug: dlv debug ./cmd/magic" -ForegroundColor Gray
} else {
    Write-GmError "build failed"
    exit 1
}
