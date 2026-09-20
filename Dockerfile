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
# 8643: reserved; nothing listens on it today (it is only listed in the CORS allowed origins)
#
# Each gateway platform (DingTalk/Feishu/Discord, ...) uses its **own distinct port**
# for webhook callbacks (see SetCallbackPort in each internal/gateway platform); none
# of them go through 8643.
# When any webhook-style platform is enabled in the container, these ports must be
# mapped as well, otherwise the platform can never reach the callback (this is exactly
# why every webhook platform was unreachable under Docker before).
# Current allocation (EXPOSE is only a declaration; whether a port is actually
# reachable from outside is decided by the compose/-p mapping):
#   8091 dingtalk      8092 feishu
#   8084 discord       8085 slack
#   8087 line          8088 teams
#   8089 googlechat    8090 sms
# Note: 8080/8081 are the gateway's own loopback ports (API / health check) and are
# not exposed outside the container.
EXPOSE 8642 8643 8084 8085 8087 8088 8089 8090 8091 8092

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -sf http://localhost:8642/api/health || exit 1

# Default command - start server with web UI
#
# `magic server` defaults to port 5000, so 8642 must be passed explicitly to line up
# with the EXPOSE above / the docker-compose port mapping; otherwise the container
# listens on 5000 and the mapping goes nowhere.
ENTRYPOINT ["/app/magic"]
CMD ["server", "--port", "8642"]