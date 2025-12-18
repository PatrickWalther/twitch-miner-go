.PHONY: build build-all clean test docker docker-push help

BINARY_NAME := twitch-miner-go
MODULE := github.com/PatrickWalther/twitch-miner-go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

LDFLAGS := -s -w -X $(MODULE)/internal/version.Version=$(VERSION)
DOCKER_REPO ?= thegame402/twitch-miner-go

# Build for current platform
build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) ./cmd/miner

# Build for all platforms
build-all: build-linux build-linux-arm64 build-windows build-darwin build-darwin-arm64

build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY_NAME)-linux-amd64 ./cmd/miner

build-linux-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY_NAME)-linux-arm64 ./cmd/miner

build-windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY_NAME)-windows-amd64.exe ./cmd/miner

build-darwin:
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY_NAME)-darwin-amd64 ./cmd/miner

build-darwin-arm64:
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY_NAME)-darwin-arm64 ./cmd/miner

# Run tests
test:
	go test -v -race ./...

# Run linter
lint:
	golangci-lint run

# Build Docker image
docker:
	docker build -t $(DOCKER_REPO):$(VERSION) -t $(DOCKER_REPO):latest .

# Push Docker image
docker-push: docker
	docker push $(DOCKER_REPO):$(VERSION)
	docker push $(DOCKER_REPO):latest

# Clean build artifacts
clean:
	rm -f $(BINARY_NAME)
	rm -rf dist/
	go clean

# Generate sample config
generate-config: build
	./$(BINARY_NAME) -generate-config

# Show help
help:
	@echo "Twitch Channel Points Miner - Build Targets"
	@echo ""
	@echo "  build           Build for current platform"
	@echo "  build-all       Build for all platforms (linux, windows, darwin)"
	@echo "  test            Run tests"
	@echo "  lint            Run linter"
	@echo "  docker          Build Docker image"
	@echo "  docker-push     Build and push Docker image"
	@echo "  clean           Clean build artifacts"
	@echo "  generate-config Generate sample configuration"
	@echo ""
	@echo "Variables:"
	@echo "  VERSION         Version string (default: git tag or 'dev')"
	@echo "  DOCKER_REPO     Docker repository (default: thegame402/twitch-miner-go)"
