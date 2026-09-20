# =============================================================================
# go-magic PowerShell shared library
# =============================================================================
# Dot-source it from scripts\windows\*.ps1:
#   . "$PSScriptRoot\..\lib\common.ps1"
#
# Single source of truth, kept in sync with the shell version:
#   - The version comes from the git tag (same as CI release.yml), never hardcoded
#   - internal/server/dist is the //go:embed dist target and is ignored by
#     .gitignore, so on a clean clone the Web UI must be built first, otherwise
#     go build fails with "pattern dist: no matching files found"
#
# Compatible with PowerShell 5.1 (bundled with Windows) and PowerShell 7+
# =============================================================================

$script:GmRepo = "magicwubiao/go-magic"

function Write-GmInfo  { param([string]$Message) Write-Host "[INFO] $Message" -ForegroundColor Cyan }
function Write-GmOk    { param([string]$Message) Write-Host "[ OK ] $Message" -ForegroundColor Green }
function Write-GmWarn  { param([string]$Message) Write-Host "[WARN] $Message" -ForegroundColor Yellow }
function Write-GmError { param([string]$Message) Write-Host "[FAIL] $Message" -ForegroundColor Red }

function Get-GmRepoRoot {
    <#
      .SYNOPSIS Returns the repository root when called from a script under scripts\windows\
    #>
    param([string]$PSScriptRootPath)
    return (Resolve-Path (Join-Path $PSScriptRootPath "..\..")).Path
}

function Get-GmVersion {
    <#
      .SYNOPSIS Single source of the version: env var VERSION > exact git tag > git describe > dev
      .DESCRIPTION Behaves like the shell gm_resolve_version; the version is never hardcoded
    #>
    if ($env:VERSION) { return $env:VERSION }

    $out = $null
    try {
        $out = & git describe --tags --exact-match 2>$null
        if ($LASTEXITCODE -eq 0 -and $out) { return "$out".Trim() }
    } catch { }

    try {
        $out = & git describe --tags --always 2>$null
        if ($LASTEXITCODE -eq 0 -and $out) { return "$out".Trim() }
    } catch { }

    return "dev"
}

function Get-GmCommit {
    try {
        $c = & git rev-parse --short HEAD 2>$null
        if ($LASTEXITCODE -eq 0 -and $c) { return "$c".Trim() }
    } catch { }
    return "unknown"
}

function Get-GmLdflags {
    param([string]$Version)
    $commit = Get-GmCommit
    $date = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
    # The variable name must be main.Version (capitalized); main.version is
    # silently ignored by Go
    return "-s -w -X main.Version=$Version -X main.Commit=$commit -X main.BuildDate=$date"
}

function Test-GmWebDist {
    param([string]$RepoRoot)
    return (Test-Path (Join-Path $RepoRoot "internal\server\dist\index.html"))
}

function Build-GmWebDist {
    <#
      .SYNOPSIS Builds the Web UI (npm ci, same as CI)
    #>
    param(
        [string]$RepoRoot,
        [switch]$Force
    )

    $dist = Join-Path $RepoRoot "internal\server\dist"
    if ((Test-GmWebDist $RepoRoot) -and (-not $Force)) {
        Write-GmOk "Web UI already present: $dist"
        return
    }

    if (-not (Get-Command npm -ErrorAction SilentlyContinue)) {
        throw "npm is missing; please install Node.js 22+ (https://nodejs.org/)"
    }

    $web = Join-Path $RepoRoot "web"
    if (-not (Test-Path (Join-Path $web "package.json"))) {
        throw "web\package.json not found; cannot build the Web UI"
    }

    Write-GmInfo "building the Web UI (required by go:embed dist)..."
    Push-Location $web
    try {
        if (Test-Path (Join-Path $web "package-lock.json")) {
            & npm ci
            if ($LASTEXITCODE -ne 0) {
                Write-GmWarn "npm ci failed; falling back to npm ci --legacy-peer-deps"
                & npm ci --legacy-peer-deps
            }
        } else {
            & npm install --legacy-peer-deps
        }
        if ($LASTEXITCODE -ne 0) { throw "npm install of dependencies failed" }

        & npm run build
        if ($LASTEXITCODE -ne 0) { throw "npm run build failed" }
    } finally {
        Pop-Location
    }

    if (-not (Test-GmWebDist $RepoRoot)) {
        throw "the build finished but $dist\index.html still does not exist (was the vite outDir changed?)"
    }
    Write-GmOk "Web UI build complete"
}

function Invoke-GmGoBuild {
    <#
      .SYNOPSIS Builds for the given platform with CGO_ENABLED=0, same as CI
    #>
    param(
        [string]$RepoRoot,
        [string]$GoOs,
        [string]$GoArch,
        [string]$Version,
        [string]$Output
    )

    $previousGoOs = $env:GOOS
    $previousGoArch = $env:GOARCH
    $previousCgo = $env:CGO_ENABLED

    try {
        $env:GOOS = $GoOs
        $env:GOARCH = $GoArch
        $env:CGO_ENABLED = "0"

        $ldflags = Get-GmLdflags -Version $Version
        & go build -ldflags "$ldflags" -o $Output ./cmd/magic
        return $LASTEXITCODE
    } finally {
        $env:GOOS = $previousGoOs
        $env:GOARCH = $previousGoArch
        $env:CGO_ENABLED = $previousCgo
    }
}
