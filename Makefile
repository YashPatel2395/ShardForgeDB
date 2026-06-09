BINARY     := shardforge
BUILD_DIR  := bin
CMD        := ./cmd/shardforge
GO         := go
GOFLAGS    :=

.PHONY: all build test fmt vet lint clean help

all: fmt vet build

## build: compile the shardforge binary into bin/
build:
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY) $(CMD)

## test: run all tests with race detection
test:
	$(GO) test -race -count=1 ./...

## fmt: format all Go source files
fmt:
	$(GO) fmt ./...

## vet: run go vet on all packages
vet:
	$(GO) vet ./...

## lint: run golangci-lint if installed
lint:
	@if command -v golangci-lint > /dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not found — skipping (install from https://golangci-lint.run)"; \
	fi

## clean: remove build artifacts
clean:
	@rm -rf $(BUILD_DIR)

## help: print this help message
help:
	@echo "Usage: make <target>"
	@grep -E '^##' $(MAKEFILE_LIST) | sed 's/^## /  /'
