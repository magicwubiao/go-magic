# go-magic Makefile

.PHONY: help build build-cli build-web build-docker install clean test lint run docker docker-run docs

# Variables
VERSION := $(shell git describe --tags --always 2>/dev/null || echo "dev")
BUILD_DIR := ./dist
GO := go
GOFLAGS := -ldflags="-s -w -X main.version=$(VERSION)"

# Help
help:
	@echo "go-magic Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make build         Build CLI binary"
	@echo "  make build-cli    Build CLI binary only"
	@echo "  make build-web    Build Web UI only"
	@echo "  make build-docker Build Docker image"
	@echo "  make install      Install to ~/.local/bin"
	@echo "  make run          Run from source"
	@echo "  make docker       Build and run Docker"
	@echo "  make docker-run  Run Docker container"
	@echo "  make test         Run tests"
	@echo "  make lint         Run linter"
	@echo "  make clean        Clean build artifacts"
	@echo "  make docs         Generate documentation"
	@echo ""
	@echo "Variables:"
	@echo "  VERSION=$(VERSION)"

# Build
build: build-cli build-web

build-cli:
	@echo "Building CLI..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GO) build $(GOFLAGS) -o $(BUILD_DIR)/magic ./cmd/magic

build-web:
	@echo "Building Web UI..."
	@if [ -d "web" ] && [ -f "web/package.json" ]; then \
		cd web && npm run build 2>/dev/null || pnpm build || echo "Web build skipped"; \
		cd - > /dev/null; \
	fi

build-docker:
	@echo "Building Docker image..."
	@docker build -t magicwubiao/go-magic:$(VERSION) .

# Install
install: build-cli
	@echo "Installing to ~/.local/bin..."
	@mkdir -p ~/.local/bin
	@cp $(BUILD_DIR)/magic ~/.local/bin/magic
	@echo "Installed to ~/.local/bin/magic"

# Clean
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
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
		-v ~/.go-magic:/root/.go-magic \
		magicwubiao/go-magic:$(VERSION)

docker-run:
	docker run -d -p 8642:8642 \
		--name go-magic \
		-v ~/.go-magic:/root/.go-magic \
		-e GO_MAGIC_PROFILE=default \
		magicwubiao/go-magic:$(VERSION)

docker-stop:
	docker stop go-magic 2>/dev/null || true
	docker rm go-magic 2>/dev/null || true

# Docs
docs:
	@echo "Generating documentation..."
	@mkdir -p docs
	$(GO) run ./cmd/magic docs generate

# Version
version:
	@echo "go-magic v$(VERSION)"
