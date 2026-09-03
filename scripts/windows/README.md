# Windows Build Guide

## Prerequisites

1. **Install Go** (required)
   - Download from: https://go.dev/dl/
   - Minimum version: Go 1.21+

2. **Install Git** (optional, for version detection)
   - Download from: https://git-scm.com/download/win

## Quick Build

### Option 1: PowerShell (Recommended)

```powershell
# Clone the repository
git clone https://github.com/magicwubiao/go-magic.git
cd go-magic

# Run the build script
.\scripts\windows\build.ps1
```

### Option 2: Command Prompt (CMD)

```cmd
git clone https://github.com/magicwubiao/go-magic.git
cd go-magic
.\scripts\windows\build.bat
```

### Option 3: Manual Build

```cmd
go build -ldflags="-s -w" -o magic.exe .\cmd\magic
```

## Output

After successful build, executables will be in the `build\` directory:

- `build\magic-windows-amd64.exe` - 64-bit Windows
- `build\magic-windows-386.exe` - 32-bit Windows

## Usage

```cmd
# Start web dashboard
magic-windows-amd64.exe server

# Start gateway (all enabled platforms: Telegram, Discord, Teams, ...)
magic-windows-amd64.exe gateway start

# Interactive chat
magic-windows-amd64.exe chat

# Show help
magic-windows-amd64.exe --help
```

## Troubleshooting

### "This app can't run on your PC"

Make sure you downloaded the correct architecture (amd64 for 64-bit, 386 for 32-bit).

Check your Windows version:
- Settings > System > About > System type

### "Go is not recognized"

Restart your terminal/command prompt after installing Go, or add Go to your PATH:
```powershell
$env:Path += ";C:\Program Files\Go\bin"
```

### Build Errors

If you encounter build errors, make sure you have the latest Go version:
```powershell
go version
go update
```
