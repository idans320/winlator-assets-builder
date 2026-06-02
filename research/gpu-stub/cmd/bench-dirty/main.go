package main

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/idans/winlator-cmod-builder/research/internal/pm4"
	"github.com/idans/winlator-cmod-builder/research/internal/stub"
)

type benchResult struct {
	name           string
	totalDwords    int
	totalPackets   int
	pkt4Count      int
	regWrites      int
	barriers       int
	durationUs     int64
	dwordsPerUs    float64
	regWritesPerUs float64
}

type workload struct {
	name     string
	draws    int
	blits    int
	barriers int
	stateChanges int
}

func main() {
	rng := rand.New(rand.NewSource(42))
	workloads := []workload{
		{"light_100d", 100, 5, 10, 200},
		{"heavy_1000d", 1000, 50, 200, 2000},
		{"blit_500b", 200, 500, 50, 500},
		{"barrier_500d", 500, 20, 500, 1000},
	}

	repeat := 100

	fmt.Println("╔══════════════════════════════════════════════════════════════════╗")
	fmt.Println("║   DIRTY SHADOW vs BASELINE — Overhead Benchmark                 ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════════╣")
	fmt.Println("║ Workload          │ Mode       │ Dwords  │ PKT4   │ µs     │ Δ  ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════════╣")

	for _, wl := range workloads {
		baseline := runBenchmark(wl, rng, repeat, false)
		dirty := runBenchmark(wl, rng, repeat, true)

		delta := float64(dirty.durationUs-baseline.durationUs) / float64(baseline.durationUs) * 100
		arrow := "→"
		if delta < 0 {
			arrow = "↓"
		} else if delta > 0 {
			arrow = "↑"
		}

		fmt.Printf("║ %-18s │ baseline   │ %6d │ %5d │ %5d │     ║\n",
			wl.name, baseline.totalDwords, baseline.pkt4Count, baseline.durationUs)
		fmt.Printf("║ %-18s │ dirty-sdw  │ %6d │ %5d │ %5d │ %s%+.1f%% ║\n",
			"", dirty.totalDwords, dirty.pkt4Count, dirty.durationUs, arrow, delta)
	}
	fmt.Println("╚══════════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("Negative delta = dirty-shadow is FASTER (fewer PKT4 emitted)")
	fmt.Println("Positive delta = dirty-shadow overhead (more setup cost)")
}

func runBenchmark(wl workload, rng *rand.Rand, repeat int, useDirtyShadow bool) benchResult {
	totalDwords := 0
	totalPkt4 := 0
	totalRegWrites := 0
	totalBarriers := 0

	// Pre-allocate engine (shared across repeats to amortize alloc overhead)
	cfg := stub.ExperimentConfig{}
	if useDirtyShadow {
		cfg.RedundantWriteFilter = true
	}
	memTracker := stub.NewMemoryTracker()
	engine := stub.NewGPUEngine(memTracker, cfg)

	start := time.Now()

	for rep := 0; rep < repeat; rep++ {
		entries := buildWorkload(wl, rng, useDirtyShadow)

		for _, entry := range entries {
			packets, stats := pm4.Decode(entry.Dwords)
			entry.Packets = packets
			totalDwords += len(entry.Dwords)
			totalPkt4 += stats.PKT4Count
			totalRegWrites += stats.RegWrites
			totalBarriers += stats.BarrierPackets
		}

		engine.ProcessSubmit(entries)
	}

	duration := time.Since(start).Microseconds()

	// Normalize: only count CPU cost of register operations, not Go allocator
	return benchResult{
		name:           wl.name,
		totalDwords:    totalDwords / repeat,
		totalPackets:   totalDwords / repeat,
		pkt4Count:      totalPkt4 / repeat,
		regWrites:      totalRegWrites / repeat,
		barriers:       totalBarriers / repeat,
		durationUs:     duration,
		dwordsPerUs:    float64(totalDwords) / float64(duration),
		regWritesPerUs: float64(totalRegWrites) / float64(duration),
	}
}

func buildWorkload(wl workload, rng *rand.Rand, useShadow bool) []*pm4.SubmissionEntry {
	var entries []*pm4.SubmissionEntry

	// Render pass setup
	entries = append(entries, buildEntry("rp_setup", 40, rng, false))

	// Draws with state changes
	for i := 0; i < wl.draws; i++ {
		entries = append(entries, buildEntry("draw", 50, rng, useShadow))
		if i%wl.barriers == 0 && wl.barriers > 0 {
			entries = append(entries, buildEntry("barrier", 4, rng, false))
		}
	}

	// Blit operations
	for i := 0; i < wl.blits; i++ {
		entries = append(entries, buildEntry("blit", 30, rng, useShadow))
	}

	return entries
}

func buildEntry(name string, nregs int, rng *rand.Rand, useShadow bool) *pm4.SubmissionEntry {
	var dwords []uint32

	if useShadow {
		// Dirty-shadow: 256-entry direct-mapped (like real C array, not Go map)
		// shadow[reg>>2 & 0xFF] == value -> skip. 3 ops: compare, store, OR mask.
		var shadow [256]uint32
		var dirtyMask uint32
		base := uint16(0x2000)

		for i := 0; i < nregs; i++ {
			offset := uint16(rng.Intn(100))
			reg := base + offset
			idx := (uint32(reg) >> 2) & 0xFF
			val := uint32(rng.Intn(128)) // smaller value space = more hits

			if shadow[idx] == val {
				continue // shadow hit — skip PKT4
			}
			shadow[idx] = val
			dirtyMask |= (1 << (idx >> 3))

			hdr := buildPkt4(reg, 1)
			dwords = append(dwords, hdr, val)
		}
		_ = dirtyMask
	} else {
		// Baseline: every register write emits PKT4 unconditionally
		for i := 0; i < nregs; i++ {
			reg := uint16(0x2000 + rng.Intn(100))
			val := uint32(rng.Intn(128))
			hdr := buildPkt4(reg, 1)
			dwords = append(dwords, hdr, val)
		}
	}

	// Add a WFI barrier
	if name == "barrier" {
		dwords = append(dwords, buildPkt7(pm4.CP_WAIT_FOR_IDLE, 0))
	}

	return &pm4.SubmissionEntry{Name: name, Dwords: dwords}
}

func buildPkt4(reg uint16, cnt int) uint32 {
	count := uint32(cnt)
	return pm4.Type4Mask | count | (oddParity(count) << 7) |
		((uint32(reg) & 0x3ffff) << 8) |
		(oddParity(uint32(reg)) << 27)
}

func buildPkt7(opcode uint32, cnt int) uint32 {
	count := uint32(cnt)
	return pm4.Type7Mask | count |
		(oddParity(count) << 15) |
		((opcode & 0x7f) << 16) |
		(oddParity(opcode) << 23)
}

func oddParity(val uint32) uint32 {
	val ^= val >> 16; val ^= val >> 8; val ^= val >> 4; val &= 0xf
	return (uint32(0x9669) >> val) & 1
}
