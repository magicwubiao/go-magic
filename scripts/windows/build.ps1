# go-magic Windows build script (PowerShell)
# Usage: .\scripts\windows\build.ps1 [-Clean] [-Output build] [-Version v0.5.19] [-NoWeb]
#
# Artifact names match CI: go-magic-windows-amd64.exe / go-magic-windows-arm64.exe
# The script builds the Web UI first (internal/server/dist is the go:embed target;
# if it is missing on a clean clone the compile fails)

param(
    [switch]$Clean,
    [string]$Output = "build",
    [string]$Version = "",
    [switch]$NoWeb
)

$ErrorActionPreference = "Stop"

. "$PSScriptRoot\..\lib\common.ps1"

$RepoRoot = Get-GmRepoRoot -PSScriptRootPath $PSScriptRoot
Set-Location $RepoRoot

if ($Version) { $env:VERSION = $Version }

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  go-magic Windows Build" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-GmError "go not found; please install Go 1.26+ and add it to PATH: https://go.dev/dl/"
    exit 1
}

$version = Get-GmVersion
Write-GmInfo "repository root: $RepoRoot"
Write-GmInfo "Go: $(go version)"
Write-GmInfo "version: $version"

if ($Clean -and (Test-Path $Output)) {
    Write-GmInfo "removing previous build: $Output"
    Remove-Item -Recurse -Force $Output
}
if (-not (Test-Path $Output)) {
    New-Item -ItemType Directory -Path $Output | Out-Null
}

# The Web UI must be built first, otherwise go:embed dist finds no files
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

# Same as the CI windows matrix: amd64, arm64 (386 is no longer built)
$architectures = @("amd64", "arm64")
$failures = @()

foreach ($arch in $architectures) {
    $out = Join-Path $Output "go-magic-windows-$arch.exe"
    Write-Host ""
    Write-GmInfo "building windows/$arch -> go-magic-windows-$arch.exe"

    $code = Invoke-GmGoBuild -RepoRoot $RepoRoot -GoOs "windows" -GoArch $arch -Version $version -Output $out
    if ($code -eq 0 -and (Test-Path $out)) {
        $sizeMb = [math]::Round((Get-Item $out).Length / 1MB, 1)
        Write-GmOk "$out ($sizeMb MB)"
    } else {
        Write-GmError "windows/$arch build failed"
        $failures += $arch
    }
}

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
if ($failures.Count -gt 0) {
    Write-GmError "failed platforms: $($failures -join ', ')"
    exit 1
}
Write-Host "  Build Complete!" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "Artifacts:" -ForegroundColor White
Get-ChildItem "$Output\go-magic-windows-*.exe" -ErrorAction SilentlyContinue | ForEach-Object {
    Write-Host "  $($_.Name) ($([math]::Round($_.Length / 1MB, 1)) MB)" -ForegroundColor White
}
Write-Host ""
Write-Host "Run:" -ForegroundColor Yellow
Write-Host "  .\$Output\go-magic-windows-amd64.exe server" -ForegroundColor White
Write-Host "  .\$Output\go-magic-windows-amd64.exe gateway start" -ForegroundColor White
Write-Host ""
Write-Host "Config directory: $env:USERPROFILE\.magic" -ForegroundColor Gray
