# Makefile for dcfh (Directory Cache File Hash)

# Default target
.PHONY: all
all: build

# Build all binaries
.PHONY: build
build: dcfh dcfhfind dcfhfix

# Build the dcfh binary
.PHONY: dcfh
dcfh: generate-dcfh
	go build -o dcfh ./cmd/dcfh

# Build the dcfhfind binary
.PHONY: dcfhfind
dcfhfind: generate-dcfhfind
	go build -o dcfhfind ./cmd/dcfhfind

# Build the dcfhfix binary
.PHONY: dcfhfix
dcfhfix: generate-dcfhfix
	go build -o dcfhfix ./cmd/dcfhfix

# Generate version information for dcfh
.PHONY: generate-dcfh
generate-dcfh:
	cd cmd/dcfh && go generate

# Generate version information for dcfhfind
.PHONY: generate-dcfhfind
generate-dcfhfind:
	cd cmd/dcfhfind && go generate

# Generate version information for dcfhfix
.PHONY: generate-dcfhfix
generate-dcfhfix:
	cd cmd/dcfhfix && go generate

# Generate version information for all binaries
.PHONY: generate
generate: generate-dcfh generate-dcfhfind generate-dcfhfix

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
	rm -f dcfh dcfhfind dcfhfix
	rm -f cmd/dcfh/constants_version.go
	rm -f cmd/dcfhfind/constants_version.go
	rm -f cmd/dcfhfix/constants_version.go

# Install all binaries to GOBIN
.PHONY: install
install: build
	cp dcfh $(shell go env GOBIN)/dcfh
	cp dcfhfind $(shell go env GOBIN)/dcfhfind
	cp dcfhfix $(shell go env GOBIN)/dcfhfix

# Install dcfh only
.PHONY: install-dcfh
install-dcfh: dcfh
	cp dcfh $(shell go env GOBIN)/dcfh

# Install dcfhfind only
.PHONY: install-dcfhfind
install-dcfhfind: dcfhfind
	cp dcfhfind $(shell go env GOBIN)/dcfhfind

# Install dcfhfix only
.PHONY: install-dcfhfix
install-dcfhfix: dcfhfix
	cp dcfhfix $(shell go env GOBIN)/dcfhfix

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
	@echo "  all         - Build all binaries (default)"
	@echo "  build       - Build all binaries (dcfh, dcfhfind, and dcfhfix)"
	@echo "  dcfh        - Build only the dcfh binary"
	@echo "  dcfhfind    - Build only the dcfhfind binary"
	@echo "  dcfhfix     - Build only the dcfhfix binary"
	@echo "  generate    - Generate version information"
	@echo "  test        - Run all tests"
	@echo "  test-verbose- Run all tests with verbose output"
	@echo "  test-cmd    - Run CLI tests only"
	@echo "  test-pkg    - Run package tests only"
	@echo "  clean       - Clean all build artifacts"
	@echo "  install     - Install all binaries to GOBIN"
	@echo "  install-dcfh - Install only dcfh to GOBIN"
	@echo "  install-dcfhfind - Install only dcfhfind to GOBIN"
	@echo "  install-dcfhfix - Install only dcfhfix to GOBIN"
	@echo "  lint        - Run linting (requires golangci-lint)"
	@echo "  fmt         - Format code"
	@echo "  tidy        - Run go mod tidy"
	@echo "  dev         - Format, tidy, and test"
	@echo "  help        - Show this help message"