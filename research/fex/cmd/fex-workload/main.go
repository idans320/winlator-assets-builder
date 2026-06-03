package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/idans/winlator-cmod-builder/research/internal/fex"
)

type PhaseResult struct {
	Phase          string
	NewBlocks      int
	ReusedBlocks   int
	CacheHits      int
	CacheMisses    int
	ConstHits      int
	ConstMisses    int
	SpillHits      int
	SpillMisses    int
	TSOEmits       int
	TSOCoalesced   int
	ARM64Insns     int
	JITTimeUs      float64
	ConstPoolHits  int
	TSOStripped    int
	SpillHoisted   int
}

type FullSimResult struct {
	Config       string
	Description  string
	TotalBlocks  int
	TotalInsns   int
	Warmup       PhaseResult
	Steady       PhaseResult
	HitRate      float64
	ConstRate    float64
	SpillRate    float64
	TSOSaved     int
	JITTimeTotal float64
	Breakdown    map[string]float64
}

func main() {
	root := os.Getenv("FEX_ROOT")
	if root == "" {
		root = filepath.Join(os.Getenv("HOME"), "winlator-cmod-builder", "fexcore", "workdir", "fex")
	}

	fmt.Printf("=== FEXCore Gaming JIT Optimizer ===\n\n")

	ops, _ := fex.ParseIRJSON(filepath.Join(root, "FEXCore", "Source", "Interface", "IR", "IR.json"))
	fmt.Printf("IR ops: %d  ", len(ops))

	shapes := fex.ClassifyIRShapes(ops)
	fmt.Printf("IR shape families: %d (structural template candidates)\n\n", len(shapes))

	tsos, strippable := fex.DetectTSOSites(root)
	fmt.Printf("TSO sites: %d total, %d potentially strippable (stack-based)\n\n", len(tsos), strippable)

	configs := []fex.CacheConfig{fex.GameConfig, fex.ProConfig, fex.LightGameConfig}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	var results []FullSimResult
	for _, cfg := range configs {
		sr := simulateFull(cfg, rng)
		results = append(results, sr)
	}

	fmt.Printf("%-15s %6s %9s %7s %7s %7s %7s %7s %7s\n",
		"Config", "Blocks", "ARM64", "Block%", "Const%", "TSO-strip", "Spill-hoist", "Const-pool", "Steady%")
	for _, sr := range results {
		lcRate := safeDivF(sr.Steady.ConstHits, sr.Steady.ConstHits+sr.Steady.ConstMisses)
		tsoRate := safeDivF(sr.Steady.TSOStripped, sr.Steady.TSOEmits)
		spillRate := safeDivF(sr.Steady.SpillHoisted, sr.Steady.SpillHits+sr.Steady.SpillMisses)
		constRate := safeDivF(sr.Steady.ConstPoolHits, sr.Steady.ConstPoolHits+sr.Steady.ConstMisses)

		totalInsns := sr.Warmup.ARM64Insns + sr.Steady.ARM64Insns
		jitSav := 0.0
		if totalInsns > 0 {
			jitSav = 100.0 * float64(sr.Steady.ARM64Insns) / float64(totalInsns)
		}

		fmt.Printf("%-15s %6d %9d %6.1f%% %6.1f%% %6.1f%% %6.1f%% %6.1f%% %6.1f%%\n",
			sr.Config, sr.TotalBlocks, totalInsns,
			sr.HitRate*100, lcRate*100, tsoRate*100, spillRate*100, constRate*100, jitSav)
	}

	fmt.Printf("\n=== Warmup vs Steady-State Breakdown ===\n\n")
	for _, sr := range results {
		fmt.Printf("[%s] %s\n", sr.Config, sr.Description)
		tot := sr.Steady.CacheHits + sr.Steady.CacheMisses
		fmt.Printf("  Warmup (%d frames): new=%d insns=%d jit=%.1fms\n",
			fex.GameConfig.WarmupFrames, sr.Warmup.NewBlocks, sr.Warmup.ARM64Insns, sr.Warmup.JITTimeUs/1000)
		fmt.Printf("  Steady hits:         %d/%d (%.1f%%)\n",
			sr.Steady.CacheHits, tot, safeDivF(sr.Steady.CacheHits, tot)*100)
		fmt.Printf("  Constant pool dedup: %d hits (3→1 insn each)\n", sr.Steady.ConstPoolHits)
		fmt.Printf("  TSO stack stripped:  %d / %d barriers removed\n", sr.Steady.TSOStripped, sr.Steady.TSOEmits)
		fmt.Printf("  Spill hoisted:       %d loop iterations saved\n\n", sr.Steady.SpillHoisted)
	}

	fmt.Printf("\n=== Mechanical Fix Projection ===\n\n")
	fmt.Printf("%-15s %10s %10s %10s %10s %10s\n",
		"Config", "Const-pool", "TSO-strip", "Spill-hoist", "Block-cache", "Total")
	for _, sr := range results {
		cps := sr.Steady.ConstPoolHits * 2
		tso := sr.Steady.TSOStripped * 2
		spl := sr.Steady.SpillHoisted * 132
		blk := sr.Steady.CacheHits * 6
		fmt.Printf("%-15s %10d %10d %10d %10d %10d\n",
			sr.Config, cps, tso, spl, blk, cps+tso+spl+blk)
	}

	fmt.Printf("\n=== Tuning Recommendations ===\n\n")
	fmt.Printf("  Config         Precompile  TSO-strip  Spill-hoist  Const-pool  L1-entries\n")
	fmt.Printf("  %-15s %-11s %-10s %-12s %-11s %s\n",
		"game-heavy", "YES", "YES(RSP/RBP)", "YES(self-loop)", "YES(PC-ldr)", "1M")
	fmt.Printf("  %-15s %-11s %-10s %-12s %-11s %s\n",
		"light-game", "YES", "YES(RSP/RBP)", "YES(self-loop)", "YES(PC-ldr)", "128K")
	fmt.Printf("  %-15s %-11s %-10s %-12s %-11s %s\n",
		"professional", "NO", "NO", "NO", "NO", "8K")

	out, _ := json.MarshalIndent(results, "", "  ")
	_ = os.WriteFile(filepath.Join(os.Getenv("HOME"), "winlator-cmod-builder", "research", "fex", "gaming-optimizer.json"), out, 0644)
	fmt.Printf("\n[JSON] Written to research/fex/gaming-optimizer.json\n")
}

func simulateFull(cfg fex.CacheConfig, rng *rand.Rand) FullSimResult {
	sr := FullSimResult{Config: cfg.Name, Description: cfg.Description}

	blockCache := make(map[uint64]bool)
	constCache := make(map[string]bool)
	spillCache := make(map[string]bool)
	constPool := make(map[uint64]bool)

	warmup := simulatePhase("warmup", cfg.WarmupFrames, cfg, rng, blockCache, constCache, spillCache, constPool)
	sr.Warmup = warmup

	constCache = make(map[string]bool)
	spillCache = make(map[string]bool)
	constPool = make(map[uint64]bool)
	steady := simulatePhase("steady", cfg.SteadyFrames, cfg, rng, blockCache, constCache, spillCache, constPool)
	sr.Steady = steady

	sr.TotalBlocks = warmup.NewBlocks + warmup.ReusedBlocks + steady.NewBlocks + steady.ReusedBlocks
	sr.TotalInsns = warmup.ARM64Insns + steady.ARM64Insns
	sr.HitRate = safeDivF(steady.CacheHits, steady.CacheHits+steady.CacheMisses)
	sr.ConstRate = safeDivF(steady.ConstHits, steady.ConstHits+steady.ConstMisses)
	sr.SpillRate = safeDivF(steady.SpillHits, steady.SpillHits+steady.SpillMisses)
	sr.TSOSaved = warmup.TSOCoalesced + steady.TSOCoalesced
	sr.JITTimeTotal = warmup.JITTimeUs + steady.JITTimeUs

	return sr
}

func simulatePhase(name string, frames int, cfg fex.CacheConfig, rng *rand.Rand,
	blockCache map[uint64]bool, constCache, spillCache map[string]bool, constPool map[uint64]bool) PhaseResult {

	pr := PhaseResult{Phase: name}

	blockPool := make([]uint64, 0, 5000)

	for frame := 0; frame < frames; frame++ {
		blocksPerFrame := 15 + rng.Intn(35)
		for b := 0; b < blocksPerFrame; b++ {
			var addr uint64
			if name == "warmup" {
				addr = uint64(rng.Int63n(1 << 30))
			} else if cfg.IsGame() {
				if len(blockPool) > 0 && rng.Float64() < cfg.BlockReusePct {
					addr = blockPool[rng.Intn(len(blockPool))]
				} else {
					addr = uint64(rng.Int63n(1 << 30))
					blockPool = append(blockPool, addr)
				}
			} else {
				addr = uint64(rng.Int63n(1 << 30))
				if rng.Float64() < cfg.BlockReusePct && len(blockPool) > 0 {
					addr = blockPool[rng.Intn(len(blockPool))]
				} else {
					blockPool = append(blockPool, addr)
				}
			}

			if blockCache[addr] {
				pr.ReusedBlocks++
				pr.CacheHits++
				pr.ARM64Insns += 2
				pr.JITTimeUs += 0.1
			} else {
				pr.NewBlocks++
				blockCache[addr] = true
				pr.CacheMisses++
				jitInsns := 8 + rng.Intn(25)
				pr.ARM64Insns += jitInsns
				pr.JITTimeUs += float64(jitInsns) * 0.5
			}

			if rng.Float64() < 0.25 {
				constVal := uint64(rng.Int63n(200))
				key := fmt.Sprintf("const_%d", constVal)

				if cfg.ConstPoolEnabled && constPool[constVal] && rng.Float64() < cfg.ConstReusePct {
					pr.ConstPoolHits++
					pr.ARM64Insns += 1
					pr.JITTimeUs += 0.3
				} else if constCache[key] && rng.Float64() < cfg.ConstReusePct {
					pr.ConstHits++
					pr.ARM64Insns += 3
					pr.JITTimeUs += 1.0
				} else {
					constCache[key] = true
					if cfg.ConstPoolEnabled {
						constPool[constVal] = true
					}
					pr.ConstMisses++
					pr.ARM64Insns += 3
					pr.JITTimeUs += 1.0
				}
			}

			if rng.Float64() < 0.08 {
				key := fmt.Sprintf("spill_%d", rng.Int63n(1<<30))
				if spillCache[key] && rng.Float64() < cfg.SpillReusePct {
					pr.SpillHits++
				} else {
					spillCache[key] = true
					pr.SpillMisses++
					pr.ARM64Insns += 132
					pr.JITTimeUs += 15.0
				}

				if cfg.SpillHoistEnabled && name == "steady" && rng.Float64() < cfg.SpillHoistLoopPct {
					pr.SpillHoisted++
				}
			}

			if rng.Float64() < 0.03 {
				pr.TSOEmits++
				if cfg.TSOStripEnabled && rng.Float64() < cfg.TSOStripStackPct {
					pr.TSOStripped++
				} else if rng.Float64() < cfg.TSOCoalescePct {
					pr.TSOCoalesced++
				} else {
					pr.ARM64Insns += 2
					pr.JITTimeUs += 0.3
				}
			}
		}
	}

	return pr
}

type pair struct{ N string; C int }

func topShared(m map[string]int, n int) []pair {
	var entries []pair
	for k, v := range m {
		entries = append(entries, pair{k, v})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].C > entries[j].C })
	if len(entries) > n {
		entries = entries[:n]
	}
	return entries
}

func safeDivF(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}
