// shardforge-node — real networked database node for ShardForgeDB.
//
// Scope: each node is an independent single-process key-value store backed by its own
// Engine directory. Nodes communicate over HTTP/JSON. This is NOT Raft, NOT consensus,
// NOT quorum replication, NOT distributed sharding. It is a networked node runtime
// foundation — the first step toward a real distributed database.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/YashPatel2395/ShardForgeDB/internal/node"
)

const disclaimer = `
shardforge-node — ShardForgeDB networked node runtime
======================================================
SCOPE: Each node is an independent single-process database backed by a local
Engine. Nodes serve an HTTP/JSON API. This is NOT Raft, NOT consensus,
NOT quorum replication, NOT distributed sharding, NOT automatic failover.
This is a networked node runtime foundation — each node owns its own
data directory and serves key-value operations over the network.
`

func main() {
	fs := flag.NewFlagSet("shardforge-node", flag.ExitOnError)

	nodeID := fs.String("node-id", "", "unique node identifier (required)")
	addr := fs.String("addr", "127.0.0.1:9101", "TCP address to listen on")
	dataDir := fs.String("data-dir", "", "directory for node-local Engine data (required)")
	walSync := fs.Bool("wal-sync", false, "enable fsync after every WAL append (slower, more durable)")
	memTableMax := fs.Uint64("memtable-max-bytes", 0, "MemTable flush threshold in bytes (0 = default 64 MiB)")

	fs.Usage = func() {
		fmt.Fprint(os.Stderr, disclaimer)
		fmt.Fprintf(os.Stderr, "\nUsage: shardforge-node [flags]\n\nFlags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Examples:
  shardforge-node --node-id node-1 --addr 127.0.0.1:9101 --data-dir ./data/node-1
  shardforge-node --node-id node-2 --addr 127.0.0.1:9102 --data-dir ./data/node-2
`)
	}

	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(1)
	}

	if *nodeID == "" {
		fmt.Fprintln(os.Stderr, "error: --node-id is required")
		fs.Usage()
		os.Exit(1)
	}
	if *dataDir == "" {
		fmt.Fprintln(os.Stderr, "error: --data-dir is required")
		fs.Usage()
		os.Exit(1)
	}

	fmt.Print(disclaimer)
	fmt.Printf("node-id  : %s\n", *nodeID)
	fmt.Printf("addr     : %s\n", *addr)
	fmt.Printf("data-dir : %s\n", *dataDir)
	fmt.Printf("wal-sync : %v\n", *walSync)
	fmt.Println()

	srv, err := node.Open(node.Options{
		NodeID:           *nodeID,
		Addr:             *addr,
		DataDir:          *dataDir,
		WALSyncOnWrite:   *walSync,
		MemTableMaxBytes: *memTableMax,
	})
	if err != nil {
		log.Fatalf("node open: %v", err)
	}

	// Handle Ctrl+C / SIGTERM for clean shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-quit
		fmt.Printf("\n[%s] shutting down...\n", *nodeID)
		if err := srv.Close(); err != nil {
			log.Printf("close error: %v", err)
		}
		os.Exit(0)
	}()

	fmt.Printf("[%s] listening on http://%s\n", *nodeID, *addr)
	fmt.Printf("[%s] endpoints: GET /healthz  GET /status  PUT/GET/DELETE /kv/{key}  GET /scan  POST /flush  POST /compact\n\n", *nodeID)

	if err := srv.Start(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
