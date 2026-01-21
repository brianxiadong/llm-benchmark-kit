# LLM Benchmark Kit Makefile
# Build configuration for cross-compilation and Docker

BINARY_NAME := llm-benchmark-kit
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE) -s -w"

# Go build settings
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
CGO_ENABLED := 0

.PHONY: all build build-linux build-darwin build-all clean test lint help docker

all: build

## Build for current platform
build:
	@echo "Building $(BINARY_NAME) for $(GOOS)/$(GOARCH)..."
	CGO_ENABLED=$(CGO_ENABLED) go build $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/$(BINARY_NAME)

## Build for Linux amd64
build-linux-amd64:
	@echo "Building $(BINARY_NAME) for linux/amd64..."
	CGO_ENABLED=$(CGO_ENABLED) GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-linux-amd64 ./cmd/$(BINARY_NAME)

## Build for Linux arm64
build-linux-arm64:
	@echo "Building $(BINARY_NAME) for linux/arm64..."
	CGO_ENABLED=$(CGO_ENABLED) GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-linux-arm64 ./cmd/$(BINARY_NAME)

## Build for macOS amd64
build-darwin-amd64:
	@echo "Building $(BINARY_NAME) for darwin/amd64..."
	CGO_ENABLED=$(CGO_ENABLED) GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-darwin-amd64 ./cmd/$(BINARY_NAME)

## Build for macOS arm64
build-darwin-arm64:
	@echo "Building $(BINARY_NAME) for darwin/arm64..."
	CGO_ENABLED=$(CGO_ENABLED) GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-darwin-arm64 ./cmd/$(BINARY_NAME)

## Build for all platforms
build-all: build-linux-amd64 build-linux-arm64 build-darwin-amd64 build-darwin-arm64
	@echo "All builds complete!"
	@ls -la bin/

## Run tests
test:
	go test -v -race ./...

## Run tests with coverage
test-coverage:
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

## Run linter
lint:
	@which golangci-lint > /dev/null || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run ./...

## Build Docker image
docker:
	docker build -t $(BINARY_NAME):$(VERSION) .
	docker tag $(BINARY_NAME):$(VERSION) $(BINARY_NAME):latest

## Build multi-arch Docker image
docker-multi:
	docker buildx build --platform linux/amd64,linux/arm64 -t $(BINARY_NAME):$(VERSION) .

## Clean build artifacts
clean:
	rm -rf bin/
	rm -f coverage.out coverage.html

## Run with example
run-example:
	@echo "Running example benchmark..."
	go run ./cmd/$(BINARY_NAME) -url https://api.openai.com/v1/chat/completions -model gpt-3.5-turbo -token $(OPENAI_API_KEY) -total-requests 5 -concurrency 2

## Show help
help:
	@echo "LLM Benchmark Kit Build System"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  build              Build for current platform"
	@echo "  build-linux-amd64  Build for Linux amd64"
	@echo "  build-linux-arm64  Build for Linux arm64"
	@echo "  build-darwin-amd64 Build for macOS amd64"
	@echo "  build-darwin-arm64 Build for macOS arm64"
	@echo "  build-all          Build for all platforms"
	@echo "  test               Run tests"
	@echo "  test-coverage      Run tests with coverage"
	@echo "  lint               Run linter"
	@echo "  docker             Build Docker image"
	@echo "  docker-multi       Build multi-arch Docker image"
	@echo "  clean              Clean build artifacts"
	@echo "  run-example        Run example benchmark"
	@echo "  help               Show this help"
