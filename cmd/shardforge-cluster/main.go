// shardforge-cluster — static cluster metadata utility for ShardForgeDB.
//
// Scope:
//   - Validates, prints, and generates static cluster config files.
//   - This is NOT a cluster manager, service discovery daemon, or gossip service.
//   - Config is static: it describes independent node processes; there is no
//     dynamic membership, no Raft, no consensus, no leader election.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/YashPatel2395/ShardForgeDB/internal/cluster"
)

const disclaimer = `
shardforge-cluster — static cluster metadata utility
=====================================================
SCOPE: static config only. No dynamic membership. No node discovery.
No gossip. No Raft. No consensus. No leader election. No replication.
No failover. No shard migration. No distributed transactions.

Config files describe independent shardforge-node processes for use with
shardforge-gateway (--config) and shardforge-proxy (--config).
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	return runWithWriters(args, os.Stdout, os.Stderr)
}

func runWithWriters(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(stderr, disclaimer)
		fmt.Fprintf(stderr, `Usage: shardforge-cluster <command> [args]

Commands:
  validate <path>              load and validate a cluster config file
  print    <path>              load and pretty-print a cluster config file
  example-local-3node          print a 3-node local example config to stdout
  example-read-replica-3node   print a read-replica 3-node example config to stdout

Examples:
  shardforge-cluster validate configs/local-3node.json
  shardforge-cluster print    configs/local-3node-with-proxy.json
  shardforge-cluster example-local-3node
  shardforge-cluster example-read-replica-3node
`)
		return 1
	}

	cmd := args[0]
	cmdArgs := args[1:]

	switch cmd {
	case "validate":
		if len(cmdArgs) < 1 {
			fmt.Fprintln(stderr, "error: validate requires a config file path")
			return 1
		}
		cfg, err := cluster.Load(cmdArgs[0])
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "ok  %q is valid (version=%s, name=%q, nodes=%d)\n",
			cmdArgs[0], cfg.Version, cfg.Name, len(cfg.Nodes))
		return 0

	case "print":
		if len(cmdArgs) < 1 {
			fmt.Fprintln(stderr, "error: print requires a config file path")
			return 1
		}
		cfg, err := cluster.Load(cmdArgs[0])
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "error: marshal: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(data))
		return 0

	case "example-local-3node":
		cfg := cluster.ExampleLocal3Node()
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "error: marshal: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(data))
		return 0

	case "example-read-replica-3node":
		cfg := cluster.ExampleReadReplica3Node()
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "error: marshal: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(data))
		return 0

	default:
		fmt.Fprintf(stderr, "error: unknown command %q\n", cmd)
		fmt.Fprintf(stderr, "run shardforge-cluster --help for usage\n")
		return 1
	}
}
