# go-magic cross-platform build script (PowerShell)
# Usage: .\scripts\windows\build-all.ps1 [-Output dist] [-Version v0.5.19] [-NoWeb]
#
# The platform matrix matches CI (.github/workflows/release.yml) exactly, with
# the same artifact names:
#   go-magic-linux-amd64 / -arm64
#   go-magic-darwin-amd64 / -arm64
#   go-magic-windows-amd64.exe / -arm64.exe

param(
    [string]$Output = "dist",
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
Write-Host " go-magic Cross-Platform Build" -ForegroundColor Cyan
Write-Host "=======================================" -ForegroundColor Cyan
Write-Host ""
Write-GmInfo "repository root: $RepoRoot"
Write-GmInfo "version: $version"

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-GmError "go not found; please install Go 1.26+: https://go.dev/dl/"
    exit 1
}

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

if (-not (Test-Path $Output)) {
    New-Item -ItemType Directory -Path $Output | Out-Null
}

# Same as the CI release matrix
$platforms = @(
    @{ OS = "linux";   Arch = "amd64" },
    @{ OS = "linux";   Arch = "arm64" },
    @{ OS = "darwin";  Arch = "amd64" },
    @{ OS = "darwin";  Arch = "arm64" },
    @{ OS = "windows"; Arch = "amd64" },
    @{ OS = "windows"; Arch = "arm64" }
)

$total = $platforms.Count
$index = 0
$failures = @()

foreach ($p in $platforms) {
    $index++
    $ext = if ($p.OS -eq "windows") { ".exe" } else { "" }
    $name = "go-magic-$($p.OS)-$($p.Arch)$ext"
    $out = Join-Path $Output $name

    Write-Host ""
    Write-GmInfo "[$index/$total] building $($p.OS)/$($p.Arch) -> $name"

    $code = Invoke-GmGoBuild -RepoRoot $RepoRoot -GoOs $p.OS -GoArch $p.Arch -Version $version -Output $out
    if ($code -eq 0 -and (Test-Path $out)) {
        $sizeMb = [math]::Round((Get-Item $out).Length / 1MB, 1)
        Write-GmOk "$name ($sizeMb MB)"
    } else {
        Write-GmError "$($p.OS)/$($p.Arch) build failed"
        $failures += "$($p.OS)/$($p.Arch)"
    }
}

Write-Host ""
Write-Host "=======================================" -ForegroundColor Cyan
if ($failures.Count -gt 0) {
    Write-GmError "failed platforms: $($failures -join ', ')"
    exit 1
}
Write-Host " All builds complete!" -ForegroundColor Green
Write-Host "=======================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "Artifacts ($Output):" -ForegroundColor White
Get-ChildItem "$Output\go-magic-*" -ErrorAction SilentlyContinue | ForEach-Object {
    Write-Host "  $($_.Name) ($([math]::Round($_.Length / 1MB, 1)) MB)" -ForegroundColor White
}
Write-Host ""
Write-Host "Tip: these file names map one-to-one onto the Release assets" -ForegroundColor Gray
