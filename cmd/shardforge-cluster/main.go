// shardforge-cluster — static cluster metadata utility for ShardForgeDB.
//
// Scope:
//   - Validates, prints, and generates static cluster config files.
//   - Health checks report node status only; no failover or rerouting.
//   - Failure simulation and rebalance planning are simulation/planning only.
//   - No automatic failover. No automatic rebalancing. No data movement.
//   - No shard migration. No Raft. No consensus. No quorum. No leader election.
//   - This is NOT a cluster manager, service discovery daemon, or gossip service.
//   - Config is static: it describes independent node processes; there is no
//     dynamic membership, no Raft, no consensus, no leader election.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/cluster"
	"github.com/YashPatel2395/ShardForgeDB/internal/ops"
)

const disclaimer = `
shardforge-cluster — static cluster metadata utility
=====================================================
SCOPE: static config only. No dynamic membership. No node discovery.
No gossip. No Raft. No consensus. No leader election. No replication.
No failover. No shard migration. No distributed transactions.
Health checks are diagnostic only. Failure simulation and rebalance
planning are simulation/planning only. No automatic failover or data movement.

Config files describe independent shardforge-node processes for use with
shardforge-gateway (--config) and shardforge-proxy (--config).
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	return runWithWriters(args, os.Stdout, os.Stderr)
}

// multiFlag implements flag.Value for flags that may be specified multiple times.
type multiFlag []string

func (m *multiFlag) String() string {
	if m == nil {
		return ""
	}
	return fmt.Sprintf("%v", []string(*m))
}

func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
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
  health   <path>              check /healthz on all configured nodes (diagnostic only)
  simulate-failure <path>      simulate routing impact of node failures (no live calls)
  plan-rebalance   <path>      show key movement plan for node changes (no data movement)

Examples:
  shardforge-cluster validate configs/local-3node.json
  shardforge-cluster print    configs/local-3node-with-proxy.json
  shardforge-cluster example-local-3node
  shardforge-cluster example-read-replica-3node
  shardforge-cluster health   configs/local-failure-sim-3node.json
  shardforge-cluster simulate-failure configs/local-failure-sim-3node.json --down node-2 --key user:1 --key order:9
  shardforge-cluster plan-rebalance   configs/local-failure-sim-3node.json --remove node-2 --key user:1 --key order:9
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

	case "health":
		return runHealth(cmdArgs, stdout, stderr)

	case "simulate-failure":
		return runSimulateFailure(cmdArgs, stdout, stderr)

	case "plan-rebalance":
		return runPlanRebalance(cmdArgs, stdout, stderr)

	default:
		fmt.Fprintf(stderr, "error: unknown command %q\n", cmd)
		fmt.Fprintf(stderr, "run shardforge-cluster --help for usage\n")
		return 1
	}
}

// runHealth implements: shardforge-cluster health <config-path>
// Calls GET /healthz on all configured nodes. Exits 0 even if some nodes
// are unhealthy (as long as config loaded successfully). Diagnostic only.
func runHealth(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "error: health requires a config file path")
		fmt.Fprintln(stderr, "Usage: shardforge-cluster health <config-path>")
		return 1
	}
	configPath := args[0]

	fs := flag.NewFlagSet("health", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr, `Usage: shardforge-cluster health <config-path>

Calls GET /healthz on every configured node and prints a JSON ClusterHealth report.

SCOPE: diagnostic only. No failover. No rerouting. No automatic recovery.
If nodes are unhealthy, manual operator action is required.

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args[1:]); err != nil {
		return 1
	}

	cfg, err := cluster.Load(configPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: load config: %v\n", err)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := ops.CheckClusterHealth(ctx, cfg, 5*time.Second)
	if err != nil {
		fmt.Fprintf(stderr, "error: health check: %v\n", err)
		return 1
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "error: marshal result: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, string(data))
	return 0 // exit 0 even if nodes are unhealthy — config loaded, check completed
}

// runSimulateFailure implements: shardforge-cluster simulate-failure <config-path> --down <nodeID> --key <key>
// Simulation only. Does not call live nodes. Does not move data.
func runSimulateFailure(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "error: simulate-failure requires a config file path")
		fmt.Fprintln(stderr, "Usage: shardforge-cluster simulate-failure <config-path> --down <nodeID> --key <key>")
		return 1
	}
	configPath := args[0]

	fs := flag.NewFlagSet("simulate-failure", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var downNodes multiFlag
	var sampleKeys multiFlag
	fs.Var(&downNodes, "down", "node ID to mark as down (repeatable)")
	fs.Var(&sampleKeys, "key", "sample key to test routing impact (repeatable)")
	fs.Usage = func() {
		fmt.Fprintf(stderr, `Usage: shardforge-cluster simulate-failure <config-path> --down <nodeID> --key <key>

Shows how key routing changes when the specified nodes are considered down.

SCOPE: simulation only. No live node calls. No data movement. No automatic failover.
No automatic rerouting. Manual operator action is required to handle real failures.

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args[1:]); err != nil {
		return 1
	}
	if len(sampleKeys) == 0 {
		fmt.Fprintln(stderr, "error: at least one --key is required")
		return 1
	}

	cfg, err := cluster.Load(configPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: load config: %v\n", err)
		return 1
	}

	result, err := ops.SimulateFailure(cfg, ops.FailureSimulationRequest{
		DownNodeIDs: []string(downNodes),
		SampleKeys:  []string(sampleKeys),
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: simulate failure: %v\n", err)
		return 1
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "error: marshal result: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, string(data))
	return 0
}

// runPlanRebalance implements: shardforge-cluster plan-rebalance <config-path> --remove <nodeID> --key <key>
// Planning only. Does not call live nodes. Does not move data. Does not write files.
func runPlanRebalance(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "error: plan-rebalance requires a config file path")
		fmt.Fprintln(stderr, "Usage: shardforge-cluster plan-rebalance <config-path> --remove <nodeID> --key <key>")
		return 1
	}
	configPath := args[0]

	fs := flag.NewFlagSet("plan-rebalance", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var removeNodes multiFlag
	var sampleKeys multiFlag
	fs.Var(&removeNodes, "remove", "node ID to remove from the planned config (repeatable)")
	fs.Var(&sampleKeys, "key", "sample key to show routing change for (repeatable)")
	fs.Usage = func() {
		fmt.Fprintf(stderr, `Usage: shardforge-cluster plan-rebalance <config-path> --remove <nodeID> --key <key>

Shows how key routing changes when the specified nodes are removed.

SCOPE: planning only. No live node calls. No data movement. No config file writes.
No automatic rebalancing. No shard migration. Manual operator action is required.

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args[1:]); err != nil {
		return 1
	}
	if len(sampleKeys) == 0 {
		fmt.Fprintln(stderr, "error: at least one --key is required")
		return 1
	}

	cfg, err := cluster.Load(configPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: load config: %v\n", err)
		return 1
	}

	plan, err := ops.PlanManualRebalance(cfg, []string(removeNodes), nil, []string(sampleKeys))
	if err != nil {
		fmt.Fprintf(stderr, "error: plan rebalance: %v\n", err)
		return 1
	}

	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "error: marshal result: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, string(data))
	return 0
}
