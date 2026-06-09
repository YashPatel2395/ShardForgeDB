// Command shardforge-bench runs the ShardForgeDB workload benchmark suite
// and writes a Markdown report.
//
// Usage:
//
//	./bin/shardforge-bench [flags]
//
// Flags:
//
//	--scale    small|medium   benchmark scale (default: small)
//	--workload NAME           run a single workload; default runs all
//	--out      PATH           write Markdown report to PATH (default: stdout)
//	--seed     N              deterministic seed for value generation (default: 42)
//
// Examples:
//
//	./bin/shardforge-bench --scale small --out docs/BENCHMARKS.md
//	./bin/shardforge-bench --scale medium --out /tmp/bench-medium.md
//	./bin/shardforge-bench --workload write-heavy
//	./bin/shardforge-bench --workload compaction --scale medium
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/YashPatel2395/ShardForgeDB/internal/bench"
)

func main() {
	scaleName := flag.String("scale", "small", "benchmark scale: small or medium")
	outPath := flag.String("out", "", "output Markdown file (default: stdout)")
	workloadName := flag.String("workload", "", "single workload to run (default: all)")
	seed := flag.Int64("seed", 42, "deterministic seed for value generation")
	flag.Parse()

	// Validate scale early so the error is clear before any I/O.
	if _, ok := bench.Scales[*scaleName]; !ok {
		log.Fatalf("unknown scale %q; valid: small, medium", *scaleName)
	}

	// Build workload list.
	var workloads []string
	if *workloadName != "" {
		workloads = strings.Split(*workloadName, ",")
	}

	fmt.Fprintf(os.Stderr, "ShardForgeDB benchmark runner\n")
	fmt.Fprintf(os.Stderr, "  scale:     %s\n", *scaleName)
	if len(workloads) == 0 {
		fmt.Fprintf(os.Stderr, "  workloads: all\n")
	} else {
		fmt.Fprintf(os.Stderr, "  workloads: %s\n", strings.Join(workloads, ", "))
	}
	if *outPath != "" {
		fmt.Fprintf(os.Stderr, "  output:    %s\n", *outPath)
	} else {
		fmt.Fprintf(os.Stderr, "  output:    stdout\n")
	}
	fmt.Fprintln(os.Stderr)

	runner := &bench.Runner{
		ScaleName:     *scaleName,
		WorkloadNames: workloads,
		Seed:          *seed,
	}

	results, err := runner.Run()
	if err != nil {
		log.Fatalf("benchmark failed: %v", err)
	}

	sc := bench.Scales[*scaleName]
	cfg := bench.Config{Scale: sc, Seed: *seed}

	var w io.Writer = os.Stdout
	if *outPath != "" {
		f, err := os.Create(*outPath)
		if err != nil {
			log.Fatalf("create output file %s: %v", *outPath, err)
		}
		defer f.Close()
		w = f
	}

	if err := bench.WriteReport(w, *scaleName, cfg, results); err != nil {
		log.Fatalf("write report: %v", err)
	}

	if *outPath != "" {
		fmt.Fprintf(os.Stderr, "Report written to %s\n", *outPath)
	}
}
