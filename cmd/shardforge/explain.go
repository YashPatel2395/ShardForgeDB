package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/YashPatel2395/ShardForgeDB/internal/engine"
	"github.com/YashPatel2395/ShardForgeDB/internal/trace"
)

// newExplainCmd returns the "explain" subcommand tree.
//
// Usage:
//
//	shardforge explain --data-dir <dir> get  <key>
//	shardforge explain --data-dir <dir> put  <key> <value>
//	shardforge explain --data-dir <dir> delete <key>
//
// All explain commands open the engine at --data-dir, run the requested
// operation, and print a JSON trace of the real execution path.
//
// SCOPE: single-node local engine only.
// No distributed tracing, no networking, no Raft.
func newExplainCmd() *cobra.Command {
	var dataDir string

	explain := &cobra.Command{
		Use:   "explain",
		Short: "Explain the real execution trace of a key-value operation",
		Long: `Explain runs a database operation and prints a step-by-step JSON trace
of the actual execution path through the engine layers.

Every trace step is produced by the code that actually performed the operation.
No steps are fabricated or pre-scripted.

SCOPE: single-node local engine only. No distributed tracing.

Examples:
  shardforge explain --data-dir ./data put  mykey myvalue
  shardforge explain --data-dir ./data get  mykey
  shardforge explain --data-dir ./data delete mykey`,
	}

	explain.PersistentFlags().StringVar(&dataDir, "data-dir", "",
		"path to the engine data directory (required)")

	explain.AddCommand(newExplainGetCmd(&dataDir))
	explain.AddCommand(newExplainPutCmd(&dataDir))
	explain.AddCommand(newExplainDeleteCmd(&dataDir))

	return explain
}

// openEngineForExplain opens the engine at dataDir.
func openEngineForExplain(dataDir string) (*engine.Engine, error) {
	if dataDir == "" {
		return nil, fmt.Errorf("--data-dir is required")
	}
	if _, err := os.Stat(dataDir); err != nil {
		return nil, fmt.Errorf("data-dir %q: %w", dataDir, err)
	}
	return engine.Open(engine.Options{Dir: dataDir})
}

// printTrace serialises tr to indented JSON and writes it to cmd's stdout.
func printTrace(cmd *cobra.Command, tr *trace.Trace) error {
	data, err := json.MarshalIndent(tr, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal trace: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}

// newExplainGetCmd returns the "explain get <key>" subcommand.
func newExplainGetCmd(dataDir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Explain a GET operation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := openEngineForExplain(*dataDir)
			if err != nil {
				return err
			}
			defer eng.Close()

			tr, _, _, err := eng.ExplainGet([]byte(args[0]))
			if tr != nil {
				// Always print the trace, even on error (partial trace is useful).
				_ = printTrace(cmd, tr)
			}
			return err
		},
	}
}

// newExplainPutCmd returns the "explain put <key> <value>" subcommand.
func newExplainPutCmd(dataDir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "put <key> <value>",
		Short: "Explain a PUT operation",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := openEngineForExplain(*dataDir)
			if err != nil {
				return err
			}
			defer eng.Close()

			tr, err := eng.ExplainPut([]byte(args[0]), []byte(args[1]))
			if tr != nil {
				_ = printTrace(cmd, tr)
			}
			return err
		},
	}
}

// newExplainDeleteCmd returns the "explain delete <key>" subcommand.
func newExplainDeleteCmd(dataDir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <key>",
		Short: "Explain a DELETE operation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := openEngineForExplain(*dataDir)
			if err != nil {
				return err
			}
			defer eng.Close()

			tr, err := eng.ExplainDelete([]byte(args[0]))
			if tr != nil {
				_ = printTrace(cmd, tr)
			}
			return err
		},
	}
}
