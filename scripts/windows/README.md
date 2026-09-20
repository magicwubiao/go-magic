# Windows Build Guide

## Prerequisites

1. **Go 1.26+** (required) -- <https://go.dev/dl/>
2. **Node.js 22+** (required) -- the Web UI is embedded into the binary via
   `//go:embed dist`, and `internal/server/dist` is ignored by `.gitignore`, so
   **a clean clone must build the Web UI first**, otherwise `go build` fails
   outright with `pattern dist: no matching files found`
   (the scripts below all handle this step automatically)
3. **Git** (optional, used for version detection)

## Quick build

### Option 1: PowerShell (recommended)

```powershell
git clone https://github.com/magicwubiao/go-magic.git
cd go-magic

# Build Windows amd64 + arm64 (builds the Web UI first)
.\scripts\windows\build.ps1

# Cross-platform build (same 6 platforms as the CI release matrix)
.\scripts\windows\build-all.ps1

# Development build (keeps debug symbols, handy for dlv)
.\scripts\windows\dev-build.ps1
```

### Option 2: Command Prompt (CMD)

```cmd
git clone https://github.com/magicwubiao/go-magic.git
cd go-magic
.\scripts\windows\build.bat
```

### Option 3: Build from source and install into your user directory

```cmd
.\scripts\windows\install.bat
```

### Option 4: Manual build

```cmd
cd web
npm ci
npm run build
cd ..
go build -ldflags="-s -w" -o magic.exe .\cmd\magic
```

## Artifacts

Artifact names match the CI Release assets **exactly**, which makes them easy to
map onto the published files:

| Script | Output |
| --- | --- |
| `build.ps1` / `build.bat` | `build\go-magic-windows-amd64.exe`, `build\go-magic-windows-arm64.exe` |
| `build-all.ps1` | `dist\go-magic-{linux,darwin,windows}-{amd64,arm64}[.exe]` |
| `dev-build.ps1` | `magic-dev.exe` |
| `install.bat` | `%USERPROFILE%\go-magic\magic.exe` |

> The version comes from `git describe` (the same git tag source as CI).
> Override it with `-Version v0.5.19` (PowerShell) or the `VERSION` environment
> variable; there is no hardcoded `dev`/`1.0.0` anymore.

## Usage

```cmd
:: start the Web console
build\go-magic-windows-amd64.exe server

:: start the gateway (Telegram / Discord / Teams and other enabled platforms)
build\go-magic-windows-amd64.exe gateway start

:: interactive chat
build\go-magic-windows-amd64.exe chat

:: help / version
build\go-magic-windows-amd64.exe --help
build\go-magic-windows-amd64.exe --version
```

The config directory is `%USERPROFILE%\.magic`, independent of where the binary lives.

## Troubleshooting

### "This app can't run on your PC"

The wrong architecture was picked: use `amd64` on 64-bit systems and `arm64` on
ARM64 devices (such as the Surface Pro X).
To check: Settings > System > About > System type.

### "Go is not recognized"

Restart the terminal after installing Go, or add it to PATH temporarily:

```powershell
$env:Path += ";C:\Program Files\Go\bin"
```

### Error `pattern dist: no matching files found`

`internal/server/dist` does not exist (it is ignored by `.gitignore`). Build the
Web UI first:

```cmd
cd web
npm ci
npm run build
```

### npm / Node reported missing during the build

This only affects the Web UI. If you just want to verify the Go code, build on a
machine that already has the frontend artifacts, or install Node.js 22+ and retry
(`build.ps1 -NoWeb` skips the frontend build when dist already exists).
