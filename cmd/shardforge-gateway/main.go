// shardforge-gateway — client-side routing gateway CLI for ShardForgeDB.
//
// Scope:
//   - Client-side consistent-hash routing over independent shardforge-node processes.
//   - This is NOT a distributed database server.
//   - Nodes do NOT coordinate. Nodes do NOT replicate data.
//   - No Raft. No consensus. No quorum. No automatic leader election.
//   - No failover: if the routed node for a key is unavailable, the operation fails.
//     Retrying to a different node is UNSAFE because the key belongs to one node only.
//   - No shard migration. No distributed transactions. No distributed vector search.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/gateway"
)

const disclaimer = `
shardforge-gateway — ShardForgeDB client-side routing gateway
=============================================================
SCOPE: client-side routing only. Nodes are independent — they do NOT coordinate,
replicate, or share state. No Raft. No consensus. No quorum replication.
No automatic leader election. No failover. No shard migration.

Retrying to a different node is UNSAFE: without replication, a key written to
node-A cannot be found on node-B. If the routed node is unavailable, the
operation fails. Handle failures explicitly.
`

func main() {
	os.Exit(run(os.Args[1:]))
}

// run parses args and executes the requested command.
// It delegates to runWithWriters using os.Stdout and os.Stderr.
func run(args []string) int {
	return runWithWriters(args, os.Stdout, os.Stderr)
}

// runWithWriters is the testable core of the CLI.
// stdout receives normal output; stderr receives errors and usage.
func runWithWriters(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("shardforge-gateway", flag.ContinueOnError)
	fs.SetOutput(stderr)
	nodes := fs.String("nodes", "", "comma-separated list of node base URLs (required)\n    e.g. http://127.0.0.1:9101,http://127.0.0.1:9102,http://127.0.0.1:9103")
	vn := fs.Int("virtual-nodes", 128, "number of virtual ring points per node")
	timeoutStr := fs.String("timeout", "5s", "per-request HTTP timeout (e.g. 5s, 1m)")

	fs.Usage = func() {
		fmt.Fprint(stderr, disclaimer)
		fmt.Fprintf(stderr, "\nUsage: shardforge-gateway --nodes <urls> <command> [args]\n\nFlags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(stderr, `
Commands:
  route   <key>         print the node selected for key (no network call)
  put     <key> <value> write key/value to the routed node
  get     <key>         read key from the routed node
  delete  <key>         delete key from the routed node
  health                check health of all configured nodes
  flush-all             flush all configured nodes
  compact-all           compact all configured nodes

Examples:
  shardforge-gateway --nodes http://127.0.0.1:9101,http://127.0.0.1:9102,http://127.0.0.1:9103 route user:1
  shardforge-gateway --nodes http://127.0.0.1:9101,http://127.0.0.1:9102,http://127.0.0.1:9103 put user:1 alice
  shardforge-gateway --nodes http://127.0.0.1:9101,http://127.0.0.1:9102,http://127.0.0.1:9103 get user:1
  shardforge-gateway --nodes http://127.0.0.1:9101,http://127.0.0.1:9102,http://127.0.0.1:9103 delete user:1
  shardforge-gateway --nodes http://127.0.0.1:9101,http://127.0.0.1:9102,http://127.0.0.1:9103 health
  shardforge-gateway --nodes http://127.0.0.1:9101,http://127.0.0.1:9102,http://127.0.0.1:9103 flush-all
  shardforge-gateway --nodes http://127.0.0.1:9101,http://127.0.0.1:9102,http://127.0.0.1:9103 compact-all
`)
	}

	if err := fs.Parse(args); err != nil {
		// flag.ContinueOnError returns ErrHelp for --help; that is non-zero but
		// the usage was already printed to stderr. Return 1 for any parse error.
		return 1
	}

	if *nodes == "" {
		fmt.Fprintln(stderr, "error: --nodes is required")
		fs.Usage()
		return 1
	}

	timeout, err := time.ParseDuration(*timeoutStr)
	if err != nil {
		fmt.Fprintf(stderr, "error: invalid --timeout %q: %v\n", *timeoutStr, err)
		return 1
	}

	nodeCfgs := buildNodeConfigs(*nodes)
	if len(nodeCfgs) == 0 {
		fmt.Fprintln(stderr, "error: --nodes must contain at least one valid URL")
		return 1
	}

	g, err := gateway.Open(gateway.Options{
		Nodes:        nodeCfgs,
		VirtualNodes: *vn,
		Timeout:      timeout,
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: gateway open: %v\n", err)
		return 1
	}
	defer g.Close()

	remaining := fs.Args()
	if len(remaining) == 0 {
		fmt.Fprintln(stderr, "error: a command is required (route, put, get, delete, health, flush-all, compact-all)")
		fs.Usage()
		return 1
	}

	cmd := remaining[0]
	cmdArgs := remaining[1:]
	ctx := context.Background()

	switch cmd {
	case "route":
		if len(cmdArgs) < 1 {
			fmt.Fprintln(stderr, "usage: route <key>")
			return 1
		}
		rn, err := g.NodeForKey([]byte(cmdArgs[0]))
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "key=%q → node_id=%s  base_url=%s\n", cmdArgs[0], rn.ID, rn.BaseURL)

	case "put":
		if len(cmdArgs) < 2 {
			fmt.Fprintln(stderr, "usage: put <key> <value>")
			return 1
		}
		if err := g.Put(ctx, []byte(cmdArgs[0]), []byte(cmdArgs[1])); err != nil {
			fmt.Fprintf(stderr, "error: put: %v\n", err)
			return 1
		}
		rn, _ := g.NodeForKey([]byte(cmdArgs[0]))
		fmt.Fprintf(stdout, "ok  key=%q stored on node=%s\n", cmdArgs[0], rn.ID)

	case "get":
		if len(cmdArgs) < 1 {
			fmt.Fprintln(stderr, "usage: get <key>")
			return 1
		}
		val, found, err := g.Get(ctx, []byte(cmdArgs[0]))
		if err != nil {
			fmt.Fprintf(stderr, "error: get: %v\n", err)
			return 1
		}
		if !found {
			fmt.Fprintf(stdout, "not found  key=%q\n", cmdArgs[0])
			return 1
		}
		rn, _ := g.NodeForKey([]byte(cmdArgs[0]))
		fmt.Fprintf(stdout, "found  key=%q  value=%q  node=%s\n", cmdArgs[0], val, rn.ID)

	case "delete":
		if len(cmdArgs) < 1 {
			fmt.Fprintln(stderr, "usage: delete <key>")
			return 1
		}
		if err := g.Delete(ctx, []byte(cmdArgs[0])); err != nil {
			fmt.Fprintf(stderr, "error: delete: %v\n", err)
			return 1
		}
		rn, _ := g.NodeForKey([]byte(cmdArgs[0]))
		fmt.Fprintf(stdout, "ok  key=%q deleted from node=%s\n", cmdArgs[0], rn.ID)

	case "health":
		results := g.HealthAll(ctx)
		exitCode := 0
		for _, nc := range nodeCfgs {
			err := results[nc.ID]
			if err == nil {
				fmt.Fprintf(stdout, "ok      %s  %s\n", nc.ID, nc.BaseURL)
			} else {
				fmt.Fprintf(stdout, "ERROR   %s  %s  %v\n", nc.ID, nc.BaseURL, err)
				exitCode = 1
			}
		}
		return exitCode

	case "flush-all":
		if err := g.FlushAll(ctx); err != nil {
			fmt.Fprintf(stderr, "error: flush-all: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "ok  flushed %d node(s)\n", len(nodeCfgs))

	case "compact-all":
		if err := g.CompactAll(ctx); err != nil {
			fmt.Fprintf(stderr, "error: compact-all: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "ok  compacted %d node(s)\n", len(nodeCfgs))

	default:
		fmt.Fprintf(stderr, "error: unknown command %q\n", cmd)
		fs.Usage()
		return 1
	}

	return 0
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
