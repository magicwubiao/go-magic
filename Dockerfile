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
#
# chromium is what the agent's browser_* tools drive (chromedp talks CDP to it),
# so it must stay installed even though the container has no display: chromedp
# launches it headless. `chromium-chromedriver` is NOT needed by chromedp (it
# speaks CDP directly over a websocket), so it is only kept for users who run
# WebDriver-based scripts themselves.
#
# The web fonts (font-noto* / ttf-dejavu) are not cosmetic: without them Chrome
# renders every CJK glyph as a blank box, which silently breaks
# `browser_vision` (screenshot -> LLM) on Chinese pages.
RUN apk add --no-cache \
    ca-certificates \
    curl \
    git \
    bash \
    openssh-client \
    chromium \
    chromium-chromedriver \
    font-noto \
    font-noto-cjk \
    ttf-dejavu

# Create non-root user
RUN addgroup -g 1000 magic && \
    adduser -u 1000 -G magic -s /bin/bash -D magic

WORKDIR /app

# Copy binary from builder (contains embedded web assets)
COPY --from=builder /magic /app/magic

# Create the config directory and a writable browser profile dir.
#
# The agent's persistent Chrome profile defaults to ~/.magic/browser-profile.
# Leaving the whole ~/.magic to the `magic-config` volume would be the obvious
# move, but a named volume is created root-owned on first use while this image
# runs as uid 1000 (USER magic) -- Chrome then cannot write the profile and the
# browser tools fail even though chromium is installed. So the profile gets its
# own directory, pre-created with the right owner, and is mounted separately in
# docker-compose.yml, so a logged-in session survives `compose up --build`.
# See docs/USAGE.zh-CN.md section 14.1 for the one-time noVNC login recipe.
RUN mkdir -p /home/magic/.magic /home/magic/.magic/browser-profile && \
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
#
# BROWSER_HEADLESS=true is required, not cosmetic: this image has no X display,
# and isSandboxedEnvironment() does not detect a bare Alpine container (no
# TAURI_ENV / FLATPAK_ID / SNAP / APPIMAGE), so without the flag the browser
# tools would try to open a GUI window and die on "cannot open display".
# It applies to every Chrome start, including the ones that reuse the persistent
# profile in ~/.magic/browser-profile, so there is no reason to turn it off.
ENV BROWSER_HEADLESS=true
ENTRYPOINT ["/app/magic"]
CMD ["server", "--port", "8642"]
