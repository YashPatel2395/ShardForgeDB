BINARY           := shardforge
BENCH_BINARY     := shardforge-bench
DASHBOARD_BINARY := shardforge-dashboard
NODE_BINARY      := shardforge-node
GATEWAY_BINARY   := shardforge-gateway
PROXY_BINARY     := shardforge-proxy
BUILD_DIR        := bin
CMD              := ./cmd/shardforge
BENCH_CMD        := ./cmd/shardforge-bench
DASHBOARD_CMD    := ./cmd/shardforge-dashboard
NODE_CMD         := ./cmd/shardforge-node
GATEWAY_CMD      := ./cmd/shardforge-gateway
PROXY_CMD        := ./cmd/shardforge-proxy
GO               := go
GOFLAGS          :=

.PHONY: all build test fmt vet lint clean bench bench-engine bench-vector bench-shard bench-replica bench-dashboard bench-node bench-gateway bench-proxy bench-report dashboard node node-demo node-demo-down gateway-help gateway-demo proxy proxy-help proxy-route-demo smoke demo release-check help

all: fmt vet build

## build: compile all 6 binaries into bin/ (shardforge, bench, dashboard, node, gateway, proxy)
build:
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY) $(CMD)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(BENCH_BINARY) $(BENCH_CMD)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(DASHBOARD_BINARY) $(DASHBOARD_CMD)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(NODE_BINARY) $(NODE_CMD)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(GATEWAY_BINARY) $(GATEWAY_CMD)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(PROXY_BINARY) $(PROXY_CMD)

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

## bench-dashboard: run Go benchmarks for the dashboard package only
bench-dashboard:
	$(GO) test -bench=. -benchmem ./internal/dashboard/...

## bench-node: run Go benchmarks for the node package only
bench-node:
	$(GO) test -bench=. -benchmem ./internal/node/...

## bench-gateway: run Go benchmarks for the gateway package only
bench-gateway:
	$(GO) test -bench=. -benchmem ./internal/gateway/...

## bench-proxy: run Go benchmarks for the proxy package only
bench-proxy:
	$(GO) test -bench=. -benchmem ./internal/proxy/...

## dashboard: run the local dashboard in demo mode
dashboard:
	$(GO) run $(DASHBOARD_CMD) --demo

## bench-report: run the workload benchmark suite (small scale) and write docs/BENCHMARKS.md
bench-report:
	$(GO) run $(BENCH_CMD) --scale small --out docs/BENCHMARKS.md

## gateway-help: print shardforge-gateway help and scope disclaimer
gateway-help:
	./bin/shardforge-gateway --help

## gateway-demo: show routing for a demo key against local nodes (requires node-demo running)
gateway-demo:
	./bin/shardforge-gateway --nodes http://127.0.0.1:9101,http://127.0.0.1:9102,http://127.0.0.1:9103 route user:1

## proxy: run shardforge-proxy locally (listens on 127.0.0.1:9200, routes to 3 local nodes)
proxy:
	$(GO) run $(PROXY_CMD) --addr 127.0.0.1:9200 --nodes http://127.0.0.1:9101,http://127.0.0.1:9102,http://127.0.0.1:9103

## proxy-help: print shardforge-proxy help and scope disclaimer
proxy-help:
	./bin/shardforge-proxy --help

## proxy-route-demo: show which node handles user:1 (requires proxy running on 9200)
proxy-route-demo:
	curl http://127.0.0.1:9200/route/user:1

## node: run shardforge-node locally (node-1 on 127.0.0.1:9101)
node:
	$(GO) run $(NODE_CMD) --node-id node-1 --addr 127.0.0.1:9101 --data-dir /tmp/shardforge-node-1

## node-demo: start 3-node + proxy Docker Compose demo
node-demo:
	docker compose -f deploy/docker-compose.yml up --build

## node-demo-down: tear down Docker Compose demo and remove volumes
node-demo-down:
	docker compose -f deploy/docker-compose.yml down -v

## smoke: fast smoke validation (test + vet + build + CLI checks)
smoke:
	./scripts/smoke.sh

## demo: recruiter-friendly demo sequence (build + version + bench + dashboard instructions)
demo:
	./scripts/demo.sh

## release-check: full release gate (all tests + benchmarks + build + CLI + clean tree)
release-check:
	./scripts/release_check.sh

## clean: remove build artifacts
clean:
	@rm -rf $(BUILD_DIR)

## help: print this help message
help:
	@echo "Usage: make <target>"
	@grep -E '^##' $(MAKEFILE_LIST) | sed 's/^## /  /'
