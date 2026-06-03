package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"

	"github.com/idans/winlator-cmod-builder/research/internal/fp16cache"
	"github.com/idans/winlator-cmod-builder/research/internal/pm4"
	"github.com/idans/winlator-cmod-builder/research/internal/stub"
)

// ================================================================
// FP16 Buffer Cache Simulator
// ================================================================
//
// Models DXVK buffer traffic across N frames with M draw calls each.
// Each draw call has K unique buffers. Buffers carry content hashes.
// If FP16-compressible and not cached, the host packs F32→F16 (CPU cost).
// SIEVE cache avoids re-packing identical data across frames.
//
// The tool also feeds submissions to the GPU stub engine to measure
// combined effect: CPU packing cost vs GPU ALU savings from FP16.

// ================================================================

type SimConfig struct {
	Frames          int     `json:"frames"`
	DrawsPerFrame   int     `json:"draws_per_frame"`
	BufsPerDraw     int     `json:"bufs_per_draw"`
	BufSizeBytes    int     `json:"buf_size_bytes"`
	Fp16OkFraction  float64 `json:"fp16_ok_fraction"`
	CacheCapacity   int     `json:"cache_capacity"`
	PackCostUs      float64 `json:"pack_cost_us"`
	CacheHitCostUs  float64 `json:"cache_hit_cost_us"`
	DuplicateRate   float64 `json:"duplicate_rate"`
	UseSieveCache   bool    `json:"use_sieve_cache"`
	Fp16HalfCost    bool    `json:"fp16_half_cost"`
	GpuSubmitCostUs float64 `json:"gpu_submit_cost_us"`
}

type SimResult struct {
	Config SimConfig `json:"config"`

	TotalBuffers       int     `json:"total_buffers"`
	Fp16Compressible   int     `json:"fp16_compressible"`
	CachedHits         uint64  `json:"cached_hits"`
	CachedMisses       uint64  `json:"cached_misses"`
	CacheHitRate       float64 `json:"cache_hit_rate"`
	Evictions          uint64  `json:"evictions"`

	PackOpsNoCache     float64 `json:"pack_ops_no_cache"`
	PackOpsWithCache   uint64  `json:"pack_ops_with_cache"`
	PackOpsSaved       float64 `json:"pack_ops_saved"`
	PackOpsSavedPct    float64 `json:"pack_ops_saved_pct"`

	CpuTimeNoCacheUs   float64 `json:"cpu_time_no_cache_us"`
	CpuTimeWithCacheUs float64 `json:"cpu_time_with_cache_us"`
	CpuTimeSavedUs     float64 `json:"cpu_time_saved_us"`
	CpuTimePerFrameUs  float64 `json:"cpu_time_per_frame_us"`

	GpuFp16Ops         uint64  `json:"gpu_fp16_ops"`
	GpuFp32Ops         uint64  `json:"gpu_fp32_ops"`
	GpuWeightedALU     float64 `json:"gpu_weighted_alu"`
	GpuAluSavingsPct   float64 `json:"gpu_alu_savings_pct"`

	NetPerf            string  `json:"net_perf"`
	NetGainUsPerFrame  float64 `json:"net_gain_us_per_frame"`

	CachePeakOccupancy  int     `json:"cache_peak_occupancy"`
	CacheFinalOccupancy int     `json:"cache_final_occupancy"`
}

func main() {
	outputFile := flag.String("output", "", "JSON output file (default: stdout)")
	frames := flag.Int("frames", 100, "Game frames to simulate")
	drawsPerFrame := flag.Int("draws-per-frame", 500, "Draw calls per frame")
	bufsPerDraw := flag.Int("bufs-per-draw", 3, "Unique buffers per draw")
	bufSize := flag.Int("buf-size", 64, "Bytes per buffer")
	fp16OkPct := flag.Float64("fp16-ok-pct", 70, "%% of buffers FP16-compressible")
	cacheCap := flag.Int("cache-capacity", 256, "Max SIEVE cache entries")
	packCost := flag.Float64("pack-cost-us", 1.0, "CPU µs to pack one buffer to FP16")
	hitCost := flag.Float64("hit-cost-us", 0.05, "CPU µs for cache lookup+hit")
	dupRate := flag.Float64("duplicate-rate", 0.85, "Fraction of buffers reused across frames")
	useSieve := flag.Bool("use-sieve", true, "Enable SIEVE cache")
	fp16Half := flag.Bool("fp16-half-cost", true, "GPU FP16 ALU at 0.5x cost")
	submitCost := flag.Float64("submit-cost-us", 0.5, "CPU µs per GPU submit entry")
	seed := flag.Int64("seed", 42, "Random seed")
	flag.Parse()

	cfg := SimConfig{
		Frames:          *frames,
		DrawsPerFrame:   *drawsPerFrame,
		BufsPerDraw:     *bufsPerDraw,
		BufSizeBytes:    *bufSize,
		Fp16OkFraction:  *fp16OkPct / 100.0,
		CacheCapacity:   *cacheCap,
		PackCostUs:      *packCost,
		CacheHitCostUs:  *hitCost,
		DuplicateRate:   *dupRate,
		UseSieveCache:   *useSieve,
		Fp16HalfCost:    *fp16Half,
		GpuSubmitCostUs: *submitCost,
	}

	result := runSimulation(cfg, *seed)

	out := os.Stdout
	if *outputFile != "" {
		f, err := os.Create(*outputFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		out = f
	}

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	enc.Encode(result)

	fmt.Fprintf(os.Stderr, "\n=== FP16 Cache Simulation Results ===\n")
	fmt.Fprintf(os.Stderr, "  Frames: %d, Draws/frame: %d, Bufs/draw: %d\n",
		cfg.Frames, cfg.DrawsPerFrame, cfg.BufsPerDraw)
	fmt.Fprintf(os.Stderr, "  Total buffers: %d (%d FP16-compressible)\n",
		result.TotalBuffers, result.Fp16Compressible)
	fmt.Fprintf(os.Stderr, "  Cache: %d hits / %d misses (%.1f%% hit rate)\n",
		result.CachedHits, result.CachedMisses, result.CacheHitRate*100)
	fmt.Fprintf(os.Stderr, "  Pack ops saved: %.0f (%.1f%%)\n",
		result.PackOpsSaved, result.PackOpsSavedPct)
	fmt.Fprintf(os.Stderr, "  CPU time: %.0f us saved per frame\n",
		result.CpuTimeSavedUs/float64(cfg.Frames))
	fmt.Fprintf(os.Stderr, "  GPU ALU savings: %.1f%%\n", result.GpuAluSavingsPct)
	fmt.Fprintf(os.Stderr, "  Net result: %s (%.0f us/frame)\n",
		result.NetPerf, result.NetGainUsPerFrame)
}

func runSimulation(cfg SimConfig, seed int64) SimResult {
	rng := rand.New(rand.NewSource(seed))
	cache := fp16cache.NewSieveCache(cfg.CacheCapacity)

	// Track buffer hashes across frames for duplicate simulation
	bufferPool := make(map[uint64]bool)

	totalBuffers := 0
	fp16Compressible := 0
	var totalPacksNoCache float64
	var packOpsWithCache uint64
	var cpuTimeNoCache float64
	var cpuTimeWithCache float64
	peakOccupancy := 0

	// GPU stub engine for measuring ALU impact
	expCfg := stub.ExperimentConfig{Fp16HalfCost: cfg.Fp16HalfCost}
	memTracker := stub.NewMemoryTracker()
	gpuEngine := stub.NewGPUEngine(memTracker, expCfg)

	for frame := 0; frame < cfg.Frames; frame++ {
		for draw := 0; draw < cfg.DrawsPerFrame; draw++ {
			for b := 0; b < cfg.BufsPerDraw; b++ {
				totalBuffers++

				// Generate buffer hash — some are duplicates from prior frames
				bufHash := uint64(frame*1000000 + draw*1000 + b)
				if rng.Float64() < cfg.DuplicateRate && len(bufferPool) > 0 {
					// Pick a random existing buffer as "duplicate"
					bufHash = pickRandomKey(bufferPool, rng)
				}
				bufferPool[bufHash] = true

				// Is this buffer FP16-compressible?
				isFp16 := rng.Float64() < cfg.Fp16OkFraction
				if isFp16 {
					fp16Compressible++
				}

				// Simulate CPU pack cost without cache
				if isFp16 {
					totalPacksNoCache++
					cpuTimeNoCache += cfg.PackCostUs
				}

				// Simulate with SIEVE cache
				if cfg.UseSieveCache && isFp16 {
					cpuTimeWithCache += cfg.CacheHitCostUs
					if _, hit := cache.Get(bufHash); hit {
						packOpsWithCache++ // counted as a "successful cache lookup"
					} else {
						// Miss — pack and insert
						cpuTimeWithCache += cfg.PackCostUs
						val := make([]byte, cfg.BufSizeBytes)
						cache.Put(bufHash, val, cfg.BufSizeBytes)
					}
				} else if isFp16 {
					// No cache: always pay pack cost
					cpuTimeWithCache += cfg.PackCostUs
				}

				// GPU submit — FP16 dispatch at half ALU cost
				gpuTime := cfg.GpuSubmitCostUs
				_ = gpuTime
				gpuEngine.ProcessEntry(&pm4.SubmissionEntry{
					Name:          "uniform_buffer",
					Dwords:        []uint32{uint32(bufHash), uint32(cfg.BufSizeBytes)},
					Packets:       nil,
					IsFp16Compute: isFp16 && cfg.Fp16HalfCost,
				})
			}
		}

		if cache.Size() > peakOccupancy {
			peakOccupancy = cache.Size()
		}
	}

	// Compute GPU ALU savings
	es := gpuEngine.Stats()
	gpuAluSavingsPct := 0.0
	aluBaseline := float64(es.Fp16Ops+es.Fp32Ops) // full cost if all were FP32
	if aluBaseline > 0 {
		gpuAluSavingsPct = (aluBaseline - es.WeightedALU) / aluBaseline * 100
	}

	cpuSaved := cpuTimeNoCache - cpuTimeWithCache
	netGainPerFrame := cpuSaved / float64(cfg.Frames)
	netPerf := "win"
	if netGainPerFrame < 0 {
		netPerf = "loss"
		netGainPerFrame = -netGainPerFrame
	}

	return SimResult{
		Config:             cfg,
		TotalBuffers:       totalBuffers,
		Fp16Compressible:   fp16Compressible,
		CachedHits:         cache.Hits,
		CachedMisses:       cache.Misses,
		CacheHitRate:       cache.HitRate(),
		Evictions:          cache.Evictions,
		PackOpsNoCache:     totalPacksNoCache,
		PackOpsWithCache:   packOpsWithCache,
		PackOpsSaved:       totalPacksNoCache - float64(cache.Misses),
		PackOpsSavedPct:    (totalPacksNoCache - float64(cache.Misses)) / totalPacksNoCache * 100,
		CpuTimeNoCacheUs:   cpuTimeNoCache,
		CpuTimeWithCacheUs: cpuTimeWithCache,
		CpuTimeSavedUs:     cpuSaved,
		CpuTimePerFrameUs:  cpuTimeWithCache / float64(cfg.Frames),
		GpuFp16Ops:         es.Fp16Ops,
		GpuFp32Ops:         es.Fp32Ops,
		GpuWeightedALU:     es.WeightedALU,
		GpuAluSavingsPct:   gpuAluSavingsPct,
		NetPerf:            netPerf,
		NetGainUsPerFrame:  netGainPerFrame,
		CachePeakOccupancy:  peakOccupancy,
		CacheFinalOccupancy: cache.Size(),
	}
}

func pickRandomKey(m map[uint64]bool, rng *rand.Rand) uint64 {
	// Fast random pick: iterate and stop at random position
	target := rng.Intn(len(m))
	count := 0
	for k := range m {
		if count == target {
			return k
		}
		count++
	}
	for k := range m {
		return k
	}
	return 0
}
