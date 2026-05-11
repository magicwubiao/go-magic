# go-magic Windows Installer (PowerShell)
# =============================================================================
# Usage:
#   powershell -ExecutionPolicy Bypass -File install.ps1
# =============================================================================

param(
    [string]$Version = "latest",
    [string]$InstallDir = "$env:LOCALAPPDATA\magic",
    [switch]$AddToPath
)

$ErrorActionPreference = "Stop"

$BINARY_NAME = "magic.exe"
$REPO = "magicwubiao/go-magic"

# Colors
function Write-Info { Write-Host "[INFO] $args" -ForegroundColor Green }
function Write-Warn { Write-Host "[WARN] $args" -ForegroundColor Yellow }
function Write-Err { Write-Host "[ERROR] $args" -ForegroundColor Red }

Write-Host ""
Write-Info "go-magic Windows Installer"
Write-Host ""

# Detect architecture
$ARCH = switch ($env:PROCESSOR_ARCHITECTURE) {
    "AMD64"   { "amd64" }
    "ARM64"   { "arm64" }
    default   { throw "Unsupported architecture: $_" }
}

$PLATFORM = "windows-$ARCH"

Write-Info "Detected platform: $PLATFORM"

# Get download URL
if ($Version -eq "latest") {
    $API_URL = "https://api.github.com/repos/$REPO/releases/latest"
    Write-Info "Getting latest version..."
    $Response = Invoke-RestMethod $API_URL
    $Version = $Response.tag_name.TrimStart("v")
    $DownloadUrl = $Response.assets | Where-Object { $_.name -eq "magic-$PLATFORM.exe" } | Select-Object -ExpandProperty browser_download_url
} else {
    $DownloadUrl = "https://github.com/$REPO/releases/download/v$Version/magic-$PLATFORM.exe"
}

if (-not $DownloadUrl) {
    Write-Err "Could not find download URL for version $Version"
    exit 1
}

Write-Info "Version: $Version"
Write-Info "Download URL: $DownloadUrl"

# Create install directory
if (-not (Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
}

$DownloadPath = Join-Path $InstallDir $BINARY_NAME

# Download
Write-Info "Downloading..."
try {
    Invoke-WebRequest -Uri $DownloadUrl -OutFile $DownloadPath -UseBasicParsing
} catch {
    Write-Err "Download failed: $_"
    exit 1
}

Write-Info "Downloaded to $DownloadPath"

# Add to PATH
if ($AddToPath) {
    $UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($UserPath -notlike "*$InstallDir*") {
        [Environment]::SetEnvironmentVariable(
            "Path",
            "$UserPath;$InstallDir",
            "User"
        )
        Write-Info "Added $InstallDir to PATH"
        Write-Warn "Please restart your terminal or run: refreshenv"
    }
}

# Verify
Write-Info "Verifying installation..."
try {
    & $DownloadPath --version
    Write-Info "Installation successful!"
} catch {
    Write-Err "Verification failed"
    exit 1
}

Write-Host ""
Write-Info "Done!"
Write-Host ""
Write-Info "To add to PATH manually, add this to your PowerShell profile:"
Write-Host "  `$env:Path += `";$InstallDir`""
