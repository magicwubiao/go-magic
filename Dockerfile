# syntax=docker/dockerfile:1

# ===========================================
# go-magic Dockerfile
# Multi-stage build for small image size
# ===========================================

# Stage 1: Web Build
FROM node:22-alpine AS web-builder

WORKDIR /app/web

# Copy web source
COPY web/package*.json ./

# Install dependencies
RUN npm ci --legacy-peer-deps

COPY web/ .

# Build web assets
RUN npm run build

# Stage 2: Go Build
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code first, then the freshly built web assets on top: a stale
# local internal/server/dist/ (it is gitignored, and .dockerignore cannot be
# relied on alone) must never shadow what web-builder just produced.
COPY . .

# Copy web dist from web-builder (vite builds directly to internal/server/dist)
COPY --from=web-builder /app/internal/server/dist /app/internal/server/dist

# Build the binary with embedded web assets
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /magic \
    ./cmd/magic

# Stage 3: Runtime
FROM alpine:3.20

LABEL maintainer="go-magic"
LABEL description="High-performance AI Agent in Go"

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    curl \
    git \
    bash \
    openssh-client \
    chromium \
    chromium-chromedriver

# Create non-root user
RUN addgroup -g 1000 magic && \
    adduser -u 1000 -G magic -s /bin/bash -D magic

WORKDIR /app

# Copy binary from builder (contains embedded web assets)
COPY --from=builder /magic /app/magic

# Create config directory
RUN mkdir -p /home/magic/.magic && \
    chown -R magic:magic /home/magic

# Switch to non-root user
USER magic

# Expose ports
# 8642: Main API / Web UI
# 8643: 预留，当前没有任何服务监听它（仅登记在 CORS 允许源里）
#
# 各网关平台（钉钉/飞书/Discord 等）的 webhook 回调**各自使用独立端口**
# （见 internal/gateway 各平台 SetCallbackPort），并不经由 8643。
# 容器里启用 webhook 类平台时必须把这些端口一并映射出去，否则平台永远收不到
# 回调（这正是此前 Docker 部署下 webhook 平台全部不可达的原因）。
# 当前分配（导出仅为声明，实际是否对外开放取决于 compose/-p 的映射）：
#   8091 dingtalk      8092 feishu
#   8084 discord       8085 slack
#   8087 line          8088 teams
#   8089 googlechat    8090 sms
# 注意 8080/8081 是网关自身的回环端口（API / 健康检查），不对容器外暴露。
EXPOSE 8642 8643 8084 8085 8087 8088 8089 8090 8091 8092

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -sf http://localhost:8642/api/health || exit 1

# Default command - start server with web UI
#
# `magic server` 的默认端口是 5000，必须显式指定 8642 才能与上面的
# EXPOSE / docker-compose 的端口映射对齐，否则容器内监听 5000 → 映射失效。
ENTRYPOINT ["/app/magic"]
CMD ["server", "--port", "8642"]