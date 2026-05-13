# syntax=docker/dockerfile:1

# ===========================================
# go-magic Dockerfile
# Multi-stage build for small image size
# ===========================================

# Stage 1: Build
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /magic \
    ./cmd/magic

# Build web assets (if node available)
RUN if command -v node &> /dev/null; then \
    cd web && npm ci --legacy-peer-deps 2>/dev/null || true && npm run build 2>/dev/null || true; \
    fi

# Stage 2: Runtime
FROM alpine:3.19

LABEL maintainer="go-magic"
LABEL description="High-performance AI Agent in Go"

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    curl \
    git \
    bash \
    openssh-client

# Create non-root user
RUN addgroup -g 1000 magic && \
    adduser -u 1000 -G magic -s /bin/bash -D magic

WORKDIR /app

# Copy binary from builder
COPY --from=builder /magic /app/magic

# Copy web assets if they exist
COPY --from=builder /app/web/dist /app/web/dist 2>/dev/null || true

# Create config directory
RUN mkdir -p /home/magic/.magic && \
    chown -R magic:magic /home/magic

# Switch to non-root user
USER magic

# Expose ports
# 8642: Main API
# 8643: Webhook
# 8648: Web UI (if built)
EXPOSE 8642 8643 8648

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8642/api/health || exit 1

# Default command
ENTRYPOINT ["/app/magic"]
CMD ["serve"]
