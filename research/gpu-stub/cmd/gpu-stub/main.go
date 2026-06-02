package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/idans/winlator-cmod-builder/research/internal/stub"
)

func main() {
	socketPath := flag.String("socket", "/tmp/tu_stub_gpu.sock", "Unix socket path")
	redundantFilter := flag.Bool("filter-redundant", false, "Experiment A: drop duplicate register writes")
	barrierBypass := flag.Bool("bypass-barriers", false, "Experiment B: no-op cache flushes")
	latencyUs := flag.Int("latency-us", 0, "Experiment C: inject N microseconds latency per submit")
	batchAnalysis := flag.Bool("batch-analysis", false, "Experiment D: record per-submit batch stats")
	flag.Parse()

	config := stub.ExperimentConfig{
		RedundantWriteFilter: *redundantFilter,
		BarrierBypass:        *barrierBypass,
		LatencyInjectUs:      time.Duration(*latencyUs),
		BatchAnalysis:        *batchAnalysis,
	}

	memTracker := stub.NewMemoryTracker()
	engine := stub.NewGPUEngine(memTracker, config)

	server := stub.NewRPCServer(engine, *socketPath)
	if err := server.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start RPC server: %v\n", err)
		os.Exit(1)
	}
	defer server.Stop()

	fmt.Printf("=== Turnip Fake GPU Engine ===\n")
	fmt.Printf("Socket:     %s\n", *socketPath)
	fmt.Printf("Experiments:\n")
	if *redundantFilter {
		fmt.Printf("  [A] Redundant write filter: ON\n")
	}
	if *barrierBypass {
		fmt.Printf("  [B] Barrier bypass: ON\n")
	}
	if *latencyUs > 0 {
		fmt.Printf("  [C] Latency injection: %d µs\n", *latencyUs)
	}
	if *batchAnalysis {
		fmt.Printf("  [D] Batch analysis: ON\n")
	}
	if !*redundantFilter && !*barrierBypass && *latencyUs == 0 && !*batchAnalysis {
		fmt.Printf("  (none active - use flags to enable)\n")
	}
	fmt.Printf("\nWaiting for Turnip submissions...\n\n")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sigCh:
			fmt.Printf("\nShutting down...\n")
			fmt.Printf("Submits processed: %d\n", engine.SubmitCount())
			fmt.Printf("Draws processed:   %d\n", engine.DrawCount())
			fmt.Printf("Dwords processed:  %d\n", engine.DwordCount())

			stats := engine.Stats()
			if stats.RedundantWritesDropped > 0 {
				fmt.Printf("Redundant writes dropped: %d\n", stats.RedundantWritesDropped)
			}
			if stats.BarriersSkipped > 0 {
				fmt.Printf("Barriers skipped: %d\n", stats.BarriersSkipped)
			}
			return

		case <-ticker.C:
			submits := engine.SubmitCount()
			draws := engine.DrawCount()
			if submits > 0 {
				fmt.Printf("[%s] submits=%d draws=%d dwords=%d\n",
					time.Now().Format("15:04:05"), submits, draws, engine.DwordCount())
			}
		}
	}
}
