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
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/node"
	"github.com/YashPatel2395/ShardForgeDB/internal/replnet"
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
	replRole := fs.String("replication-role", "", "replication role: primary, follower, or empty for standalone")
	primaryURL := fs.String("primary-url", "", "HTTP base URL of the primary node (required when --replication-role=follower)")
	// Phase 27: background sync flags (follower only).
	bgSync := fs.Bool("bg-sync", false, "enable automatic background replication pull (follower only)")
	bgSyncInterval := fs.String("bg-sync-interval", "1s", "background sync polling interval (e.g. 500ms, 1s)")
	bgSyncTimeout := fs.String("bg-sync-request-timeout", "5s", "per-request timeout for background sync HTTP calls")
	bgSyncInitialBackoff := fs.String("bg-sync-initial-backoff", "250ms", "initial backoff after first consecutive failure")
	bgSyncMaxBackoff := fs.String("bg-sync-max-backoff", "10s", "maximum backoff cap for exponential backoff")
	bgSyncJitter := fs.Float64("bg-sync-jitter-fraction", 0.1, "jitter fraction in [0, 1] added to backoff (0 = no jitter)")

	fs.Usage = func() {
		fmt.Fprint(os.Stderr, disclaimer)
		fmt.Fprintf(os.Stderr, "\nUsage: shardforge-node [flags]\n\nFlags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Examples:
  shardforge-node --node-id node-1 --addr 127.0.0.1:9101 --data-dir ./data/node-1
  shardforge-node --node-id node-1 --addr 127.0.0.1:9111 --data-dir ./data/primary --replication-role primary
  shardforge-node --node-id node-2 --addr 127.0.0.1:9112 --data-dir ./data/replica-1 --replication-role follower --primary-url http://127.0.0.1:9111
  shardforge-node --node-id node-2 --addr 127.0.0.1:9112 --data-dir ./data/replica-1 --replication-role follower --primary-url http://127.0.0.1:9111 --bg-sync --bg-sync-interval 500ms
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
	if *replRole == "follower" && *primaryURL == "" {
		fmt.Fprintln(os.Stderr, "error: --primary-url is required when --replication-role=follower")
		fs.Usage()
		os.Exit(1)
	}
	if *bgSync && *replRole != "follower" {
		fmt.Fprintln(os.Stderr, "error: --bg-sync is only valid when --replication-role=follower")
		fs.Usage()
		os.Exit(1)
	}

	// Parse background sync duration flags.
	var bgCfg node.BackgroundSyncConfig
	if *bgSync {
		parseDur := func(s, name string) node.Duration {
			d, err := time.ParseDuration(s)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: invalid %s %q: %v\n", name, s, err)
				os.Exit(1)
			}
			return node.Duration{Duration: d}
		}
		bgCfg = node.BackgroundSyncConfig{
			Enabled:        true,
			Interval:       parseDur(*bgSyncInterval, "--bg-sync-interval"),
			RequestTimeout: parseDur(*bgSyncTimeout, "--bg-sync-request-timeout"),
			InitialBackoff: parseDur(*bgSyncInitialBackoff, "--bg-sync-initial-backoff"),
			MaxBackoff:     parseDur(*bgSyncMaxBackoff, "--bg-sync-max-backoff"),
			JitterFraction: *bgSyncJitter,
		}
	}

	fmt.Print(disclaimer)
	fmt.Printf("node-id          : %s\n", *nodeID)
	fmt.Printf("addr             : %s\n", *addr)
	fmt.Printf("data-dir         : %s\n", *dataDir)
	fmt.Printf("wal-sync         : %v\n", *walSync)
	if *replRole != "" {
		fmt.Printf("replication-role : %s\n", *replRole)
	}
	if *primaryURL != "" {
		fmt.Printf("primary-url      : %s\n", *primaryURL)
	}
	if *bgSync {
		fmt.Printf("bg-sync          : enabled (interval=%s timeout=%s backoff=%s..%s jitter=%.2f)\n",
			*bgSyncInterval, *bgSyncTimeout, *bgSyncInitialBackoff, *bgSyncMaxBackoff, *bgSyncJitter)
	}
	fmt.Println()

	srv, err := node.Open(node.Options{
		NodeID:           *nodeID,
		Addr:             *addr,
		DataDir:          *dataDir,
		WALSyncOnWrite:   *walSync,
		MemTableMaxBytes: *memTableMax,
		Replication: node.ReplicationOptions{
			Role:           replnet.Role(*replRole),
			PrimaryBaseURL: *primaryURL,
			BackgroundSync: bgCfg,
		},
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
	fmt.Printf("[%s] endpoints: GET /healthz  GET /status  PUT/GET/DELETE /kv/{key}  GET /scan  POST /flush  POST /compact\n", *nodeID)
	fmt.Printf("[%s]            GET /replication/status  GET /replication/log  POST /replication/apply  POST /replication/sync\n\n", *nodeID)

	if err := srv.Start(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
