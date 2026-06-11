package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/YashPatel2395/ShardForgeDB/internal/node"
)

// newExplainNodeCmd returns the "explain-node" subcommand tree.
//
// Usage:
//
//	shardforge explain-node --addr <url> get  <key>
//	shardforge explain-node --addr <url> put  <key> <value>
//	shardforge explain-node --addr <url> delete <key>
//	shardforge explain-node --addr <url> scan [<start> <end>]
//
// All explain-node commands call the HTTP node at --addr, run the requested
// operation via the /explain/* endpoints, and print a JSON trace of the real
// execution path.
//
// SCOPE: calls a single networked node over HTTP. No distributed tracing.
// No Raft. No cross-node trace propagation.
func newExplainNodeCmd() *cobra.Command {
	var addr string
	var timeout time.Duration

	explainNode := &cobra.Command{
		Use:   "explain-node",
		Short: "Explain the real execution trace of an operation via a live HTTP node",
		Long: `explain-node calls a running ShardForgeDB node over HTTP and prints
a step-by-step JSON trace of the actual engine execution path.

Every trace step is produced by the code that actually performed the operation
on the node. No steps are fabricated or pre-scripted.

SCOPE: single networked node via HTTP only.
No distributed tracing. No cross-node trace propagation.

Examples:
  shardforge explain-node --addr http://localhost:9101 put  mykey myvalue
  shardforge explain-node --addr http://localhost:9101 get  mykey
  shardforge explain-node --addr http://localhost:9101 delete mykey
  shardforge explain-node --addr http://localhost:9101 scan a z`,
	}

	explainNode.PersistentFlags().StringVar(&addr, "addr", "",
		"HTTP base URL of the target node (required), e.g. http://127.0.0.1:9101")
	explainNode.PersistentFlags().DurationVar(&timeout, "timeout", 10*time.Second,
		"HTTP request timeout")

	explainNode.AddCommand(newExplainNodeGetCmd(&addr, &timeout))
	explainNode.AddCommand(newExplainNodePutCmd(&addr, &timeout))
	explainNode.AddCommand(newExplainNodeDeleteCmd(&addr, &timeout))
	explainNode.AddCommand(newExplainNodeScanCmd(&addr, &timeout))

	return explainNode
}

// newNodeClient builds a node.Client from addr and timeout, validating that addr is non-empty.
func newNodeClient(addr string, timeout time.Duration) (*node.Client, error) {
	if addr == "" {
		return nil, fmt.Errorf("--addr is required")
	}
	return node.NewClient(addr, timeout), nil
}

// printNodeTrace serialises v to indented JSON and writes it to cmd's stdout.
func printNodeTrace(cmd *cobra.Command, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}

func newExplainNodeGetCmd(addr *string, timeout *time.Duration) *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Explain a GET operation via HTTP node",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newNodeClient(*addr, *timeout)
			if err != nil {
				return err
			}
			resp, err := c.ExplainGet(cmd.Context(), []byte(args[0]))
			if err != nil {
				return err
			}
			return printNodeTrace(cmd, resp)
		},
	}
}

func newExplainNodePutCmd(addr *string, timeout *time.Duration) *cobra.Command {
	return &cobra.Command{
		Use:   "put <key> <value>",
		Short: "Explain a PUT operation via HTTP node",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newNodeClient(*addr, *timeout)
			if err != nil {
				return err
			}
			resp, err := c.ExplainPut(cmd.Context(), []byte(args[0]), []byte(args[1]))
			if err != nil {
				return err
			}
			return printNodeTrace(cmd, resp)
		},
	}
}

func newExplainNodeDeleteCmd(addr *string, timeout *time.Duration) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <key>",
		Short: "Explain a DELETE operation via HTTP node",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newNodeClient(*addr, *timeout)
			if err != nil {
				return err
			}
			resp, err := c.ExplainDelete(cmd.Context(), []byte(args[0]))
			if err != nil {
				return err
			}
			return printNodeTrace(cmd, resp)
		},
	}
}

func newExplainNodeScanCmd(addr *string, timeout *time.Duration) *cobra.Command {
	return &cobra.Command{
		Use:   "scan [<start> <end>]",
		Short: "Explain a SCAN operation via HTTP node",
		Args:  cobra.RangeArgs(0, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newNodeClient(*addr, *timeout)
			if err != nil {
				return err
			}
			var start, end []byte
			if len(args) >= 1 {
				start = []byte(args[0])
			}
			if len(args) >= 2 {
				end = []byte(args[1])
			}
			resp, err := c.ExplainScan(cmd.Context(), start, end)
			if err != nil {
				return err
			}
			return printNodeTrace(cmd, resp)
		},
	}
}
