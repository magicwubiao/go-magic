# Multi-stage Dockerfile for magic Agent
# Stage 1: Build
FROM golang:1.26-alpine AS builder

# Set working directory
WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o magic ./cmd/magic

# Stage 2: Runtime
FROM alpine:latest

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates

# Create non-root user
RUN adduser -D -g '' magic

# Create magic directories
RUN mkdir -p /home/magic/.magic/skills && \
    mkdir -p /home/magic/.magic/logs && \
    chown -R magic:magic /home/magic

# Copy binary from builder
COPY --from=builder /app/magic /usr/local/bin/magic

# Switch to non-root user
USER magic

# Set home directory
ENV HOME=/home/magic
ENV magic_HOME=/home/magic/.magic

# Expose port if needed (for gateway)
EXPOSE 8080

# Set working directory
WORKDIR /home/magic

# Run magic
ENTRYPOINT ["magic"]
CMD ["--help"]
