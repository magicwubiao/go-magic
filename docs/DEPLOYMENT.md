# Deployment Guide

This guide covers various deployment options for go-magic.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Binary Installation](#binary-installation)
- [Source Compilation](#source-compilation)
- [Docker Deployment](#docker-deployment)
- [System Service](#system-service)
- [Environment Requirements](#environment-requirements)

## Prerequisites

- Go 1.21+ (for source compilation)
- SQLite 3
- 512MB RAM minimum (1GB recommended)
- Linux, macOS, or Windows

## Binary Installation

### Pre-built Binaries

Download pre-built binaries from the [releases page](https://github.com/magicwubiao/go-magic/releases).

### Install via Go

```bash
go install github.com/magicwubiao/go-magic/cmd/magic@latest
```

### Manual Installation

```bash
# Download the binary for your platform
curl -L https://github.com/magicwubiao/go-magic/releases/latest/download/magic-linux-amd64 -o magic
chmod +x magic
sudo mv magic /usr/local/bin/
```

## Source Compilation

### Prerequisites

- Go 1.21 or higher
- Git
- Make

### Build Steps

```bash
# Clone the repository
git clone https://github.com/magicwubiao/go-magic.git
cd go-magic

# Build the binary
make build

# Or manually
go build -o magic ./cmd/magic

# Install to system
sudo make install
```

### Cross-Compilation

```bash
# Build for Linux AMD64
GOOS=linux GOARCH=amd64 make build

# Build for Windows
GOOS=windows GOARCH=amd64 make build

# Build for macOS ARM64
GOOS=darwin GOARCH=arm64 make build
```

## Docker Deployment

### Dockerfile

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o magic ./cmd/magic

FROM alpine:latest
RUN apk --no-cache add sqlite-libs
COPY --from=builder /app/magic /usr/local/bin/
COPY --from=builder /app/config.example.json /app/config.json
WORKDIR /app
ENTRYPOINT ["magic"]
CMD ["chat"]
```

### Build Docker Image

```bash
docker build -t magic:latest .
```

### Run Container

```bash
# Interactive mode
docker run -it --rm magic:latest chat

# With config mount
docker run -it --rm -v ~/.magic:/root/.magic magic:latest chat

# With workspace mount
docker run -it --rm -v $(pwd):/workspace -w /workspace magic:latest chat
```

### Docker Compose

```yaml
version: '3.8'

services:
  magic:
    image: magic:latest
    container_name: go-magic
    volumes:
      - ~/.magic:/root/.magic
      - ./workspace:/workspace
    environment:
      - MAGIC_CONFIG=/root/.magic/config.json
    stdin_open: true
    tty: true
    restart: unless-stopped
```

Run with:
```bash
docker-compose up -d
```

## System Service

### systemd (Linux)

Create service file at `/etc/systemd/system/magic.service`:

```ini
[Unit]
Description=go-magic AI Agent
After=network.target

[Service]
Type=simple
User=your-user
WorkingDirectory=/home/your-user
ExecStart=/usr/local/bin/magic agent
Restart=on-failure
RestartSec=5
Environment=HOME=/home/your-user

[Install]
WantedBy=multi-user.target
```

Enable and start:

```bash
sudo systemctl daemon-reload
sudo systemctl enable magic
sudo systemctl start magic
```

### launchd (macOS)

Create plist at `~/Library/LaunchAgents/com.magic.agent.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.magic.agent</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/magic</string>
        <string>agent</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
</dict>
</plist>
```

Enable:

```bash
launchctl load ~/Library/LaunchAgents/com.magic.agent.plist
```

### Windows Service

Use [NSSM](https://nssm.cc/) or create a Windows Service.

## Environment Requirements

### Minimum Requirements

| Resource | Minimum | Recommended |
|----------|---------|-------------|
| CPU | 1 core | 2+ cores |
| RAM | 512 MB | 1 GB |
| Disk | 100 MB | 500 MB |
| SQLite | 3.x | 3.x |

### Network Requirements

- Outbound HTTPS (443) for LLM API access
- Optional inbound for gateway features

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `MAGIC_HOME` | Config directory | `~/.magic` |
| `MAGIC_CONFIG` | Config file path | `~/.magic/config.json` |
| `MAGIC_LOG_LEVEL` | Log level (debug, info, warn, error) | `info` |
| `MAGIC_NO_COLOR` | Disable colored output | `false` |

### Security Considerations

1. **API Keys**: Store in config file with restricted permissions
   ```bash
   chmod 600 ~/.magic/config.json
   ```

2. **Command Execution**: Enable whitelist mode for restricted environments
   ```json
   {
     "tools": {
       "command_whitelist": ["git", "ls", "cat"]
     }
   }
   ```

3. **Network Isolation**: Use firewall rules to restrict outbound connections

## Configuration

### Basic Configuration

```bash
# Initialize config
magic config set provider openai
magic config set providers.openai.api_key YOUR_KEY
magic config set cortex.enabled true
```

### Advanced Configuration

See [CONFIGURATION.md](CONFIGURATION.md) for full configuration options.

## Troubleshooting

### Build Failures

```bash
# Clean cache
go clean -cache

# Update dependencies
go mod tidy
```

### Runtime Errors

```bash
# Check logs
magic logs

# Run diagnostics
magic doctor

# Validate config
magic config validate
```
