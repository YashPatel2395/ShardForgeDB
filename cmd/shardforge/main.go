package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is set at build time via -ldflags.
var version = "0.1.0"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "shardforge",
		Short: "ShardForgeDB — an explainable distributed key-value and vector database",
		Long: `ShardForgeDB is an explainable distributed database engine
designed for key-value and vector search workloads.

Phase 1: Project Foundation
  - CLI skeleton
  - Config loading
  - Structured logging

Database internals are NOT implemented yet.`,
	}

	root.AddCommand(newVersionCmd())
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print ShardForgeDB version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("ShardForgeDB %s\n", version)
		},
	}
}
