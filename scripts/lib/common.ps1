# =============================================================================
# go-magic PowerShell 共享库
# =============================================================================
# 供 scripts\windows\*.ps1 点源使用:  . "$PSScriptRoot\..\lib\common.ps1"
#
# 单一事实源与 shell 版保持一致:
#   - 版本号来自 git tag（与 CI release.yml 相同），不硬编码
#   - internal/server/dist 是 //go:embed dist 的目标且被 .gitignore 忽略，
#     干净克隆上必须先构建 Web UI，否则 go build 报 "pattern dist: no matching files found"
#
# 兼容 PowerShell 5.1（Windows 自带）与 PowerShell 7+
# =============================================================================

$script:GmRepo = "magicwubiao/go-magic"

function Write-GmInfo  { param([string]$Message) Write-Host "[INFO] $Message" -ForegroundColor Cyan }
function Write-GmOk    { param([string]$Message) Write-Host "[ OK ] $Message" -ForegroundColor Green }
function Write-GmWarn  { param([string]$Message) Write-Host "[WARN] $Message" -ForegroundColor Yellow }
function Write-GmError { param([string]$Message) Write-Host "[FAIL] $Message" -ForegroundColor Red }

function Get-GmRepoRoot {
    <#
      .SYNOPSIS 由 scripts\windows\ 下的脚本调用时返回仓库根目录
    #>
    param([string]$PSScriptRootPath)
    return (Resolve-Path (Join-Path $PSScriptRootPath "..\..")).Path
}

function Get-GmVersion {
    <#
      .SYNOPSIS 版本唯一来源：环境变量 VERSION > 精确 git tag > git describe > dev
      .DESCRIPTION 与 shell 版 gm_resolve_version 行为一致，绝不硬编码版本号
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
    # 变量名必须是 main.Version（大写）；写成 main.version 会被 Go 静默忽略
    return "-s -w -X main.Version=$Version -X main.Commit=$commit -X main.BuildDate=$date"
}

function Test-GmWebDist {
    param([string]$RepoRoot)
    return (Test-Path (Join-Path $RepoRoot "internal\server\dist\index.html"))
}

function Build-GmWebDist {
    <#
      .SYNOPSIS 构建 Web UI（与 CI 一致使用 npm ci）
    #>
    param(
        [string]$RepoRoot,
        [switch]$Force
    )

    $dist = Join-Path $RepoRoot "internal\server\dist"
    if ((Test-GmWebDist $RepoRoot) -and (-not $Force)) {
        Write-GmOk "Web UI 已就绪: $dist"
        return
    }

    if (-not (Get-Command npm -ErrorAction SilentlyContinue)) {
        throw "缺少 npm，请先安装 Node.js 22+（https://nodejs.org/）"
    }

    $web = Join-Path $RepoRoot "web"
    if (-not (Test-Path (Join-Path $web "package.json"))) {
        throw "找不到 web\package.json，无法构建 Web UI"
    }

    Write-GmInfo "构建 Web UI（go:embed dist 依赖）..."
    Push-Location $web
    try {
        if (Test-Path (Join-Path $web "package-lock.json")) {
            & npm ci
            if ($LASTEXITCODE -ne 0) {
                Write-GmWarn "npm ci 失败，回退 npm ci --legacy-peer-deps"
                & npm ci --legacy-peer-deps
            }
        } else {
            & npm install --legacy-peer-deps
        }
        if ($LASTEXITCODE -ne 0) { throw "npm 安装依赖失败" }

        & npm run build
        if ($LASTEXITCODE -ne 0) { throw "npm run build 失败" }
    } finally {
        Pop-Location
    }

    if (-not (Test-GmWebDist $RepoRoot)) {
        throw "构建结束但 $dist\index.html 仍不存在（vite outDir 是否被改动？）"
    }
    Write-GmOk "Web UI 构建完成"
}

function Invoke-GmGoBuild {
    <#
      .SYNOPSIS 按平台编译，CGO_ENABLED=0 与 CI 一致
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
