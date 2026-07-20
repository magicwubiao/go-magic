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

# Copy web dist from web-builder (vite builds directly to internal/server/dist)
COPY --from=web-builder /app/internal/server/dist /app/internal/server/dist

# Copy source code
COPY . .

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
# 8643: Webhook
EXPOSE 8642 8643

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -sf http://localhost:8642/api/health || exit 1

# Default command - start server with web UI
ENTRYPOINT ["/app/magic"]
CMD ["server"]