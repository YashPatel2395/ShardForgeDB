BINARY           := shardforge
BENCH_BINARY     := shardforge-bench
DASHBOARD_BINARY := shardforge-dashboard
NODE_BINARY      := shardforge-node
GATEWAY_BINARY   := shardforge-gateway
PROXY_BINARY     := shardforge-proxy
CLUSTER_BINARY   := shardforge-cluster
BUILD_DIR        := bin
CMD              := ./cmd/shardforge
BENCH_CMD        := ./cmd/shardforge-bench
DASHBOARD_CMD    := ./cmd/shardforge-dashboard
NODE_CMD         := ./cmd/shardforge-node
GATEWAY_CMD      := ./cmd/shardforge-gateway
PROXY_CMD        := ./cmd/shardforge-proxy
CLUSTER_CMD      := ./cmd/shardforge-cluster
GO               := go
GOFLAGS          :=

.PHONY: all build test fmt vet lint clean bench bench-engine bench-vector bench-shard bench-replica bench-dashboard bench-node bench-gateway bench-proxy bench-cluster bench-replnet bench-ops bench-trace bench-report dashboard node node-demo node-demo-down replica-demo replica-demo-down gateway-help gateway-demo gateway-config-demo proxy proxy-help proxy-route-demo cluster-validate cluster-help cluster-example replica-config-demo replica-status-demo ops-health-demo ops-simulate-failure-demo ops-rebalance-plan-demo smoke demo release-check final-smoke cluster-demo-up cluster-demo-smoke cluster-demo-down repl-demo-up repl-demo-smoke repl-demo-down repl-restart-demo-up repl-restart-demo-smoke repl-restart-demo-down help

all: fmt vet build

## build: compile all 7 binaries into bin/ (shardforge, bench, dashboard, node, gateway, proxy, cluster)
build:
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY) $(CMD)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(BENCH_BINARY) $(BENCH_CMD)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(DASHBOARD_BINARY) $(DASHBOARD_CMD)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(NODE_BINARY) $(NODE_CMD)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(GATEWAY_BINARY) $(GATEWAY_CMD)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(PROXY_BINARY) $(PROXY_CMD)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(CLUSTER_BINARY) $(CLUSTER_CMD)

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

## bench-cluster: run Go benchmarks for the cluster package only
bench-cluster:
	$(GO) test -bench=. -benchmem ./internal/cluster/...

## bench-replnet: run Go benchmarks for the replnet package only
bench-replnet:
	$(GO) test -bench=. -benchmem ./internal/replnet/...

## bench-ops: run Go benchmarks for the ops package only
bench-ops:
	$(GO) test -bench=. -benchmem ./internal/ops/...

## bench-trace: run Go tests for the trace package (no benchmarks in Phase 21)
bench-trace:
	$(GO) test -race -count=1 ./internal/trace/...

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

## gateway-config-demo: show routing using config file (ring-only, no network call)
gateway-config-demo:
	./bin/shardforge-gateway --config configs/local-3node.json route user:1

## proxy: run shardforge-proxy locally (listens on 127.0.0.1:9200, routes to 3 local nodes)
proxy:
	$(GO) run $(PROXY_CMD) --addr 127.0.0.1:9200 --nodes http://127.0.0.1:9101,http://127.0.0.1:9102,http://127.0.0.1:9103

## proxy-help: print shardforge-proxy help and scope disclaimer
proxy-help:
	./bin/shardforge-proxy --help

## proxy-route-demo: show which node handles user:1 (requires proxy running on 9200)
proxy-route-demo:
	curl http://127.0.0.1:9200/route/user:1

## cluster-validate: run cluster package tests (validates all config files)
cluster-validate:
	$(GO) test -race -count=1 ./internal/cluster/...

## cluster-help: print shardforge-cluster help and scope disclaimer
cluster-help:
	./bin/shardforge-cluster --help

## cluster-example: print a 3-node example config to stdout
cluster-example:
	./bin/shardforge-cluster example-local-3node

## cluster-example-replica: print a 3-node read-replica example config to stdout
cluster-example-replica:
	./bin/shardforge-cluster example-read-replica-3node

## node: run shardforge-node locally (node-1 on 127.0.0.1:9101)
node:
	$(GO) run $(NODE_CMD) --node-id node-1 --addr 127.0.0.1:9101 --data-dir /tmp/shardforge-node-1

## node-demo: start 3-node + proxy Docker Compose demo
node-demo:
	docker compose -f deploy/docker-compose.yml up --build

## node-demo-down: tear down Docker Compose demo and remove volumes
node-demo-down:
	docker compose -f deploy/docker-compose.yml down -v

## replica-demo: start 1-primary + 2-replica + proxy Docker Compose demo
replica-demo:
	docker compose -f deploy/docker-compose-replica.yml up --build

## replica-demo-down: tear down replica Docker Compose demo and remove volumes
replica-demo-down:
	docker compose -f deploy/docker-compose-replica.yml down -v

## replica-config-demo: print the read-replica 3-node example config to stdout
replica-config-demo:
	./bin/shardforge-cluster example-read-replica-3node

## replica-status-demo: show replication status from all nodes via the proxy (requires replica-demo running)
replica-status-demo:
	curl -s http://127.0.0.1:9210/replication/status

## ops-health-demo: check health of all nodes in the failure-sim config (reports unhealthy if nodes not running)
ops-health-demo:
	./bin/shardforge-cluster health configs/local-failure-sim-3node.json

## ops-simulate-failure-demo: simulate node-2 failure impact on sample keys
ops-simulate-failure-demo:
	./bin/shardforge-cluster simulate-failure configs/local-failure-sim-3node.json --down node-2 --key user:1 --key user:2 --key order:9

## ops-rebalance-plan-demo: plan manual rebalance after removing node-2
ops-rebalance-plan-demo:
	./bin/shardforge-cluster plan-rebalance configs/local-failure-sim-3node.json --remove node-2 --key user:1 --key user:2 --key order:9

## smoke: fast smoke validation (test + vet + build + CLI checks)
smoke:
	./scripts/smoke.sh

## demo: recruiter-friendly demo sequence (build + version + bench + dashboard instructions)
demo:
	./scripts/demo.sh

## release-check: full release gate (all tests + benchmarks + build + CLI + clean tree)
release-check:
	./scripts/release_check.sh

## final-smoke: Phase 24 final smoke — go mod tidy, fmt, vet, tests, build, CLI checks (including explain), config validate, ops sim
final-smoke:
	./scripts/final_smoke.sh

## cluster-demo-up: Phase 24 — start 3-node local cluster demo (nodes + proxy as local processes, no Docker)
cluster-demo-up:
	./scripts/demo_cluster_up.sh

## cluster-demo-smoke: Phase 24 — run cluster demo smoke test (health, routing, put/get, isolation, explain-node)
cluster-demo-smoke:
	./scripts/demo_cluster_smoke.sh

## cluster-demo-down: Phase 24 — stop cluster demo processes and remove demo data directories
cluster-demo-down:
	./scripts/demo_cluster_down.sh

## repl-demo-up: Phase 25 — start leader+follower replication demo (2 HTTP nodes, no Docker)
repl-demo-up:
	./scripts/repl_demo_up.sh

## repl-demo-smoke: Phase 25 — run replication demo smoke test (16 checks: health, PUT, explicit pull, idempotency, DELETE replication, role enforcement, error handling)
repl-demo-smoke:
	./scripts/repl_demo_smoke.sh

## repl-demo-down: Phase 25 — stop replication demo processes and remove demo data directories
repl-demo-down:
	./scripts/repl_demo_down.sh

## repl-restart-demo-up: Phase 26 — start durable replication demo (leader+follower, persisted journal+cursor)
repl-restart-demo-up:
	./scripts/repl_restart_demo_up.sh

## repl-restart-demo-smoke: Phase 26 — run restart recovery smoke test (proves journal+cursor survive restarts)
repl-restart-demo-smoke:
	./scripts/repl_restart_demo_smoke.sh

## repl-restart-demo-down: Phase 26 — stop restart demo processes and remove demo data directories
repl-restart-demo-down:
	./scripts/repl_restart_demo_down.sh

## clean: remove build artifacts
clean:
	@rm -rf $(BUILD_DIR)

## help: print this help message
help:
	@echo "Usage: make <target>"
	@grep -E '^##' $(MAKEFILE_LIST) | sed 's/^## /  /'
