# go-magic Makefile

.PHONY: help build build-cli build-web build-docker build-all build-cross install clean test lint run docker docker-run docs

# Variables
VERSION := $(shell git describe --tags --always 2>/dev/null || echo "dev")
BUILD_DIR := ./dist
CROSS_DIR := ./build
GO := go
GOFLAGS := -ldflags="-s -w -X main.Version=$(VERSION)"

# Go version for cross-compilation
GO_VERSION := 1.25

# Help
help:
	@echo "go-magic Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make build           Build CLI binary (current platform)"
	@echo "  make build-cli       Build CLI binary only"
	@echo "  make build-web       Build Web UI only"
	@echo "  make build-docker    Build Docker image"
	@echo "  make build-all       Build all common platforms (Linux/macOS/Windows)"
	@echo "  make build-cross     Build all supported platforms (cross-compile)"
	@echo "  make install         Install to ~/.local/bin"
	@echo "  make run             Run from source"
	@echo "  make docker          Build and run Docker"
	@echo "  make docker-run      Run Docker container (background)"
	@echo "  make test            Run tests"
	@echo "  make lint            Run linter"
	@echo "  make clean           Clean build artifacts"
	@echo "  make docs            Generate documentation"
	@echo ""
	@echo "Cross-platform targets:"
	@echo "  make build-linux     Build Linux binaries"
	@echo "  make build-macos     Build macOS binaries"
	@echo "  make build-windows   Build Windows binaries"
	@echo ""
	@echo "Variables:"
	@echo "  VERSION=$(VERSION)"
	@echo "  BUILD_DIR=$(BUILD_DIR)"
	@echo "  CROSS_DIR=$(CROSS_DIR)"

# Build current platform
build: build-cli build-web

build-cli:
	@echo "Building CLI for current platform..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GO) build $(GOFLAGS) -o $(BUILD_DIR)/magic ./cmd/magic

build-web:
	@echo "Building Web UI..."
	@if [ -d "web" ] && [ -f "web/package.json" ]; then \
		cd web && pnpm install 2>/dev/null || npm install --legacy-peer-deps 2>/dev/null; \
		pnpm build 2>/dev/null || npm run build 2>/dev/null; \
		cd - > /dev/null; \
	fi

build-docker:
	@echo "Building Docker image..."
	@docker build -t magicwubiao/go-magic:$(VERSION) .

# Cross-platform build
build-all:
	@echo "Building for all common platforms..."
	@./scripts/build-cross.sh common --dir $(CROSS_DIR) --compress --checksum

build-cross:
	@echo "Building for all supported platforms..."
	@./scripts/build-cross.sh all --dir $(CROSS_DIR) --compress --checksum

# Platform-specific builds
build-linux:
	@echo "Building for Linux..."
	@GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) -o $(BUILD_DIR)/magic-linux-amd64 ./cmd/magic
	@GOOS=linux GOARCH=arm64 $(GO) build $(GOFLAGS) -o $(BUILD_DIR)/magic-linux-arm64 ./cmd/magic
	@echo "Linux builds complete"

build-macos:
	@echo "Building for macOS..."
	@GOOS=darwin GOARCH=amd64 $(GO) build $(GOFLAGS) -o $(BUILD_DIR)/magic-darwin-amd64 ./cmd/magic
	@GOOS=darwin GOARCH=arm64 $(GO) build $(GOFLAGS) -o $(BUILD_DIR)/magic-darwin-arm64 ./cmd/magic
	@echo "macOS builds complete"

build-windows:
	@echo "Building for Windows..."
	@GOOS=windows GOARCH=amd64 $(GO) build $(GOFLAGS) -o $(BUILD_DIR)/magic-windows-amd64.exe ./cmd/magic
	@GOOS=windows GOARCH=386 $(GO) build $(GOFLAGS) -o $(BUILD_DIR)/magic-windows-386.exe ./cmd/magic
	@echo "Windows builds complete"

# Install
install: build-cli
	@echo "Installing to ~/.local/bin..."
	@mkdir -p ~/.local/bin
	@cp $(BUILD_DIR)/magic ~/.local/bin/magic
	@chmod +x ~/.local/bin/magic
	@echo "Installed to ~/.local/bin/magic"

# Clean
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@rm -rf $(CROSS_DIR)
	@rm -rf web/dist web/.output web/build
	@find . -name "*.test" -delete 2>/dev/null || true
	@find . -name "coverage.out" -delete 2>/dev/null || true
	@find . -name "coverage.html" -delete 2>/dev/null || true

# Test
test:
	$(GO) test -v -race -cover ./...

test-short:
	$(GO) test -short ./...

# Lint
lint:
	@which golangci-lint > /dev/null || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run ./...

# Run
run:
	$(GO) run ./cmd/magic

# Docker
docker: build-docker
	docker run --rm -it \
		-v ~/.magic:/root/.magic \
		magicwubiao/go-magic:$(VERSION)

docker-run:
	docker run -d -p 8642:8642 \
		--name go-magic \
		-v ~/.magic:/root/.magic \
		-e GO_MAGIC_PROFILE=default \
		magicwubiao/go-magic:$(VERSION)

docker-stop:
	docker stop go-magic 2>/dev/null || true
	docker rm go-magic 2>/dev/null || true

# Docs
docs:
	$(GO) run ./cmd/magic docs generate

# Version
version:
	@echo "Version: $(VERSION)"
	@$(GO) version

# Info
info:
	@echo "Project: go-magic"
	@echo "Version: $(VERSION)"
	@echo "Go Version: $(GO_VERSION)+"
	@echo "Build Dir: $(BUILD_DIR)"
	@echo "Cross Build Dir: $(CROSS_DIR)"
	@echo ""
	@echo "Supported platforms:"
	@echo "  Linux:   386, amd64, armv6, arm64, riscv64, ppc64le, s390x"
	@echo "  macOS:   amd64, arm64"
	@echo "  Windows: 386, amd64, arm64"
	@echo "  BSD:     freebsd, openbsd, netbsd"
