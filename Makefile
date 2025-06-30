# Makefile for dcfh (Directory Cache File Hash)

# Default target
.PHONY: all
all: build

# Build the dcfh binary
.PHONY: build
build: generate
	go build -o dcfh ./cmd

# Generate version information
.PHONY: generate
generate:
	cd cmd && go generate

# Run all tests
.PHONY: test
test: generate
	go test ./...

# Run tests with verbose output
.PHONY: test-verbose
test-verbose: generate
	go test -v ./...

# Run only CLI tests
.PHONY: test-cmd
test-cmd: generate
	go test -v ./cmd/...

# Run only package tests
.PHONY: test-pkg
test-pkg: generate
	go test -v ./pkg/...

# Clean build artifacts
.PHONY: clean
clean:
	rm -f dcfh
	rm -f cmd/constants_version.go

# Install the binary to GOBIN
.PHONY: install
install: build
	cp dcfh $(shell go env GOBIN)/dcfh

# Run linting (requires golangci-lint)
.PHONY: lint
lint:
	golangci-lint run

# Format code
.PHONY: fmt
fmt:
	go fmt ./...

# Run go mod tidy
.PHONY: tidy
tidy:
	go mod tidy

# Development target - format, tidy, test
.PHONY: dev
dev: fmt tidy test

# Help target
.PHONY: help
help:
	@echo "Available targets:"
	@echo "  all         - Build the dcfh binary (default)"
	@echo "  build       - Build the dcfh binary"
	@echo "  generate    - Generate version information"
	@echo "  test        - Run all tests"
	@echo "  test-verbose- Run all tests with verbose output"
	@echo "  test-cmd    - Run CLI tests only"
	@echo "  test-pkg    - Run package tests only"
	@echo "  clean       - Clean build artifacts"
	@echo "  install     - Install binary to GOBIN"
	@echo "  lint        - Run linting (requires golangci-lint)"
	@echo "  fmt         - Format code"
	@echo "  tidy        - Run go mod tidy"
	@echo "  dev         - Format, tidy, and test"
	@echo "  help        - Show this help message"