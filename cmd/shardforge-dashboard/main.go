// Command shardforge-dashboard runs a local observability dashboard for
// ShardForgeDB components.
//
// # Local only
//
// This dashboard is local only. It does NOT implement networking between
// database nodes, RPC, distributed deployment, Raft, consensus, automatic
// leader election, or quorum replication. All components run in a single OS
// process.
//
// Usage:
//
//	shardforge-dashboard [flags]
//
// Flags:
//
//	--addr        Listen address (default 127.0.0.1:8080)
//	--demo        Start a demo with a local replica store
//	--run-chaos   Run chaos scenarios before starting the server (requires --demo)
//	--help        Show help
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/YashPatel2395/ShardForgeDB/internal/dashboard"
	"github.com/YashPatel2395/ShardForgeDB/internal/replica"
)

func main() {
	fs := flag.NewFlagSet("shardforge-dashboard", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:8080", "TCP address to listen on")
	demo := fs.Bool("demo", false, "run a local demo with an in-process replica store")
	runChaos := fs.Bool("run-chaos", false, "run chaos scenarios before starting (requires --demo)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `shardforge-dashboard — local observability dashboard for ShardForgeDB

This tool is LOCAL ONLY. It does not implement networking between database
nodes, RPC, Raft, consensus, or distributed deployment. All components run
inside a single OS process.

Usage:
  shardforge-dashboard [flags]

Flags:
`)
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Examples:
  shardforge-dashboard --demo
  shardforge-dashboard --demo --run-chaos
  shardforge-dashboard --addr 127.0.0.1:9090 --demo
`)
	}
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(1)
	}

	if !*demo {
		fmt.Fprintln(os.Stderr, "No mode selected. Use --demo to run a local demo.")
		fmt.Fprintln(os.Stderr, "Run --help for usage.")
		os.Exit(1)
	}

	// ── Demo mode ────────────────────────────────────────────────────────────

	dir, err := os.MkdirTemp("", "shardforge-dashboard-*")
	if err != nil {
		log.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	log.Printf("Opening demo replica store in %s", dir)
	st, err := replica.Open(replica.Options{Dir: dir, ReplicaCount: 3})
	if err != nil {
		log.Fatalf("replica.Open: %v", err)
	}
	defer st.Close()

	// Seed some keys.
	for i := 0; i < 20; i++ {
		k := fmt.Sprintf("demo-key-%03d", i)
		v := fmt.Sprintf("demo-val-%03d", i)
		if _, err := st.Put([]byte(k), []byte(v)); err != nil {
			log.Fatalf("Put: %v", err)
		}
	}
	if err := st.ReplicateAll(); err != nil {
		log.Fatalf("ReplicateAll: %v", err)
	}
	log.Printf("Seeded 20 keys and replicated to all followers")

	// ── Chaos scenarios ───────────────────────────────────────────────────────

	var scenarioCollectors []dashboard.Collector

	if *runChaos {
		log.Println("Running chaos scenarios...")

		for _, run := range []struct {
			name string
			fn   func(dashboard.ReplicaScenarioTarget) dashboard.ScenarioResult
		}{
			{"FollowerPauseScenario", dashboard.RunFollowerPauseScenario},
			{"FollowerLagScenario", dashboard.RunFollowerLagScenario},
			{"FollowerCatchupScenario", dashboard.RunFollowerCatchupScenario},
		} {
			// Open a fresh store for each scenario so they don't interfere.
			sdir, err := os.MkdirTemp("", "chaos-"+run.name+"-*")
			if err != nil {
				log.Fatalf("MkdirTemp for %s: %v", run.name, err)
			}
			defer os.RemoveAll(sdir)

			sst, err := replica.Open(replica.Options{Dir: sdir, ReplicaCount: 3})
			if err != nil {
				log.Fatalf("replica.Open for %s: %v", run.name, err)
			}
			defer sst.Close()

			target := dashboard.ReplicaScenarioTarget{Store: sst, FollowerID: 1}
			result := run.fn(target)
			log.Printf("  %s → %s", result.Name, result.Status)
			if result.Error != "" {
				log.Printf("    error: %s", result.Error)
			}
			scenarioCollectors = append(scenarioCollectors, dashboard.NewScenarioCollector(result))
		}
	}

	// ── Build multi-collector ─────────────────────────────────────────────────

	collectors := []dashboard.Collector{
		dashboard.NewReplicaCollector("demo-replica", st),
	}
	collectors = append(collectors, scenarioCollectors...)
	multi := dashboard.NewMultiCollector("ShardForgeDB Demo Dashboard", collectors...)

	// ── Start server ──────────────────────────────────────────────────────────

	srv, err := dashboard.NewServer(dashboard.Options{
		Addr:  *addr,
		Title: "ShardForgeDB Demo Dashboard",
	}, multi)
	if err != nil {
		log.Fatalf("dashboard.NewServer: %v", err)
	}

	// Handle Ctrl+C.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("Shutting down dashboard...")
		_ = srv.Close()
	}()

	log.Printf("Dashboard running at http://%s/", *addr)
	log.Printf("  Status:  http://%s/status", *addr)
	log.Printf("  Healthz: http://%s/healthz", *addr)
	log.Printf("  Events:  http://%s/events", *addr)
	log.Println("Press Ctrl+C to stop.")

	if err := srv.Start(); err != nil {
		log.Fatalf("server: %v", err)
	}
}
