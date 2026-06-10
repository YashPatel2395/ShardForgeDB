// shardforge-proxy — stateless HTTP routing proxy for ShardForgeDB.
//
// Scope:
//   - Stateless client-side routing only. The proxy stores no data.
//   - Every HTTP request is routed to exactly one independent shardforge-node
//     process using Phase 15 internal/gateway consistent-hash routing.
//   - No Raft. No consensus. No quorum replication. No automatic leader election.
//   - No distributed sharding inside nodes. No networked replication.
//   - No automatic failover. No retry to another node.
//     If the routed node is unavailable, the operation fails immediately.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/cluster"
	"github.com/YashPatel2395/ShardForgeDB/internal/gateway"
	"github.com/YashPatel2395/ShardForgeDB/internal/proxy"
)

const disclaimer = `
shardforge-proxy — stateless HTTP routing proxy
===============================================
SCOPE: client-side routing over independent shardforge-node processes.
No Raft. No consensus. No replication. No failover. No retry to another node.

Each request is routed to exactly one backend node via consistent hashing.
If the routed node is unavailable, the operation fails — no other node is tried.
Retrying to a different node is UNSAFE without replication: a key written to
node-A cannot be found on node-B.
`

func main() {
	os.Exit(run(os.Args[1:]))
}

// run parses args and executes the proxy server.
// It delegates to runWithWriters using os.Stdout and os.Stderr.
func run(args []string) int {
	return runWithWriters(args, os.Stdout, os.Stderr)
}

// runWithWriters is the testable core of the CLI.
// stdout receives normal output; stderr receives errors and usage.
func runWithWriters(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("shardforge-proxy", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "path to cluster config JSON file (use instead of --nodes)")
	addr := fs.String("addr", "", "proxy listen address (host:port); overrides config when set")
	nodes := fs.String("nodes", "", "comma-separated list of node base URLs (use instead of --config)\n    e.g. http://127.0.0.1:9101,http://127.0.0.1:9102,http://127.0.0.1:9103")
	vn := fs.Int("virtual-nodes", 0, "number of virtual ring points per node (0 = use config or default 128)")
	timeoutStr := fs.String("timeout", "5s", "per-request HTTP timeout to backend nodes (e.g. 5s, 1m)")

	fs.Usage = func() {
		fmt.Fprint(stderr, disclaimer)
		fmt.Fprintf(stderr, "\nUsage: shardforge-proxy --nodes <urls> [flags]\n       shardforge-proxy --config <path> [flags]\n\nFlags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(stderr, `
Endpoints:
  GET    /healthz              proxy liveness (does not check backend nodes)
  GET    /status               proxy status, gateway stats, scope flags
  GET    /route/{key}          show which node handles key (no network call)
  PUT    /kv/{key}             write key/value to routed node
  GET    /kv/{key}             read key from routed node
  DELETE /kv/{key}             delete key from routed node
  GET    /scan-node/{nodeID}   scan a single named node (per-node only)
  POST   /flush-all            flush all configured nodes
  POST   /compact-all          compact all configured nodes
  GET    /nodes/health         health check all configured nodes

Examples (--nodes):
  shardforge-proxy --nodes http://127.0.0.1:9101,http://127.0.0.1:9102,http://127.0.0.1:9103

Examples (--config):
  shardforge-proxy --config configs/local-3node-with-proxy.json
  shardforge-proxy --config configs/local-3node-with-proxy.json --addr 0.0.0.0:9200
`)
	}

	if err := fs.Parse(args); err != nil {
		return 1
	}

	// Reject using both --config and --nodes to avoid ambiguity.
	if *configPath != "" && *nodes != "" {
		fmt.Fprintln(stderr, "error: use either --config or --nodes, not both")
		return 1
	}
	if *configPath == "" && *nodes == "" {
		fmt.Fprintln(stderr, "error: --nodes or --config is required")
		fs.Usage()
		return 1
	}

	timeout, err := time.ParseDuration(*timeoutStr)
	if err != nil {
		fmt.Fprintf(stderr, "error: invalid --timeout %q: %v\n", *timeoutStr, err)
		return 1
	}

	var opts proxy.Options

	if *configPath != "" {
		cfg, err := cluster.Load(*configPath)
		if err != nil {
			fmt.Fprintf(stderr, "error: load config: %v\n", err)
			return 1
		}
		popts, err := cluster.ProxyOptions(cfg, timeout)
		if err != nil {
			fmt.Fprintf(stderr, "error: proxy options: %v\n", err)
			return 1
		}
		opts = popts
		// --addr overrides config addr when explicitly provided.
		if *addr != "" {
			opts.Addr = *addr
		}
		// --virtual-nodes overrides config when explicitly provided (non-zero).
		if *vn > 0 {
			opts.VirtualNodes = *vn
		}
	} else {
		nodeCfgs := buildNodeConfigs(*nodes)
		if len(nodeCfgs) == 0 {
			fmt.Fprintln(stderr, "error: --nodes must contain at least one valid URL")
			return 1
		}
		listenAddr := *addr
		if listenAddr == "" {
			listenAddr = "127.0.0.1:9200"
		}
		virtualNodes := *vn
		if virtualNodes <= 0 {
			virtualNodes = 128
		}
		opts = proxy.Options{
			Addr:         listenAddr,
			Nodes:        nodeCfgs,
			VirtualNodes: virtualNodes,
			Timeout:      timeout,
		}
	}

	srv, err := proxy.Open(opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: proxy open: %v\n", err)
		return 1
	}

	fmt.Fprint(stdout, disclaimer)
	fmt.Fprintf(stdout, "listening on http://%s\n", srv.Addr())
	fmt.Fprintf(stdout, "routing over %d node(s) with %d virtual ring points\n",
		len(opts.Nodes), opts.VirtualNodes)
	for _, n := range opts.Nodes {
		fmt.Fprintf(stdout, "  %s → %s\n", n.ID, n.BaseURL)
	}
	fmt.Fprintln(stdout)

	// Start server in background, handle signals for clean shutdown.
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		fmt.Fprintf(stdout, "\nreceived signal %s — shutting down\n", sig)
		if err := srv.Close(); err != nil {
			fmt.Fprintf(stderr, "error: shutdown: %v\n", err)
			return 1
		}
		return 0
	case err := <-errCh:
		if err != nil {
			fmt.Fprintf(stderr, "error: server: %v\n", err)
			return 1
		}
		return 0
	}
}

// buildNodeConfigs parses a comma-separated list of base URLs into NodeConfig slice.
// Node IDs are assigned compactly and deterministically as node-1, node-2, ...
// based on the order of valid (non-empty) URLs, ignoring empty comma entries.
func buildNodeConfigs(raw string) []gateway.NodeConfig {
	parts := strings.Split(raw, ",")
	cfgs := make([]gateway.NodeConfig, 0, len(parts))
	for _, p := range parts {
		u := strings.TrimSpace(p)
		if u == "" {
			continue
		}
		cfgs = append(cfgs, gateway.NodeConfig{
			ID:      fmt.Sprintf("node-%d", len(cfgs)+1),
			BaseURL: u,
		})
	}
	return cfgs
}
