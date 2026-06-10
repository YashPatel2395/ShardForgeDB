BINARY       := shardforge
BENCH_BINARY := shardforge-bench
BUILD_DIR    := bin
CMD          := ./cmd/shardforge
BENCH_CMD    := ./cmd/shardforge-bench
GO           := go
GOFLAGS      :=

.PHONY: all build test fmt vet lint clean bench bench-engine bench-vector bench-shard bench-replica bench-report help

all: fmt vet build

## build: compile the shardforge and shardforge-bench binaries into bin/
build:
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY) $(CMD)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(BENCH_BINARY) $(BENCH_CMD)

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

## bench: run all Go benchmarks across all packages
bench:
	$(GO) test -bench=. -benchmem ./...

## bench-engine: run Go benchmarks for the engine package only
bench-engine:
	$(GO) test -bench=. -benchmem ./internal/engine/...

## bench-vector: run Go benchmarks for the vector package only
bench-vector:
	$(GO) test -bench=. -benchmem ./internal/vector/...

## bench-shard: run Go benchmarks for the shard package only
bench-shard:
	$(GO) test -bench=. -benchmem ./internal/shard/...

## bench-replica: run Go benchmarks for the replica package only
bench-replica:
	$(GO) test -bench=. -benchmem ./internal/replica/...

## bench-report: run the workload benchmark suite (small scale) and write docs/BENCHMARKS.md
bench-report:
	$(GO) run $(BENCH_CMD) --scale small --out docs/BENCHMARKS.md

## clean: remove build artifacts
clean:
	@rm -rf $(BUILD_DIR)

## help: print this help message
help:
	@echo "Usage: make <target>"
	@grep -E '^##' $(MAKEFILE_LIST) | sed 's/^## /  /'
