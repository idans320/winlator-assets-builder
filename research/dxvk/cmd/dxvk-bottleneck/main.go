package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/idans/winlator-cmod-builder/research/internal/dxvk"
)

type BottleneckReport struct {
	Title                string              `json:"title"`
	SourceFile           string              `json:"source_file"`
	HotPathRanking       []RankedHotPath     `json:"hot_path_ranking"`
	OptimizationTargets  []RankedTarget      `json:"optimization_targets"`
	QuickWins            []string            `json:"quick_wins"`
	ArchitectureNotes    []string            `json:"architecture_notes"`
}

type RankedHotPath struct {
	File         string   `json:"file"`
	Category     string   `json:"category"`
	EjectCount   int      `json:"eject_count"`
	HotPathCount int      `json:"hotpath_count"`
	Density      float64  `json:"density"`
	Suggestions  []string `json:"suggestions"`
}

type RankedTarget struct {
	Name     string   `json:"name"`
	Impact   string   `json:"impact"`
	Priority int      `json:"priority"`
	Hits     int      `json:"hits"`
	Files    int      `json:"files"`
	Action   string   `json:"action"`
	Details  []string `json:"details"`
}

func main() {
	inputFile := flag.String("input", "output/dxvk_analysis.json", "Input JSON from dxvk-analyzer")
	outputFile := flag.String("output", "output/dxvk_bottleneck.json", "Output JSON")
	flag.Parse()

	data, err := os.ReadFile(*inputFile)
	if err != nil {
		panic(err)
	}

	var analysis struct {
		VulkanEjects    []struct {
			File     string `json:"file"`
			Line     int    `json:"line"`
			Category string `json:"category"`
		} `json:"vulkan_ejects"`
		ContextHotPaths []struct {
			File     string `json:"file"`
			Line     int    `json:"line"`
			Category string `json:"category"`
			Call     string `json:"call"`
		} `json:"context_hot_paths"`
		OptTargets []struct {
			Name  string `json:"name"`
			Count int    `json:"count"`
			Files int    `json:"files"`
		} `json:"optimization_targets"`
		FileStats []struct {
			Path        string `json:"path"`
			VkEjects    int    `json:"vk_ejects"`
			HotPaths    int    `json:"hot_paths"`
			OptPatterns int    `json:"opt_patterns"`
			TotalLines  int    `json:"total_lines"`
		} `json:"file_stats"`
	}
	json.Unmarshal(data, &analysis)

	report := &BottleneckReport{
		Title:      "DXVK Optimization Bottleneck Report",
		SourceFile: *inputFile,
	}

	// Build per-file aggregated hot path ranking
	type fileAgg struct {
		path     string
		ejects   int
		hotPaths int
		lines    int
		cats     map[string]int
	}
	fileAggs := make(map[string]*fileAgg)
	for _, e := range analysis.VulkanEjects {
		if _, ok := fileAggs[e.File]; !ok {
			fileAggs[e.File] = &fileAgg{path: e.File, cats: make(map[string]int)}
		}
		fileAggs[e.File].ejects++
		fileAggs[e.File].cats[e.Category]++
	}
	for _, hp := range analysis.ContextHotPaths {
		if _, ok := fileAggs[hp.File]; !ok {
			fileAggs[hp.File] = &fileAgg{path: hp.File, cats: make(map[string]int)}
		}
		fileAggs[hp.File].hotPaths++
	}
	for _, fs := range analysis.FileStats {
		if fa, ok := fileAggs[fs.Path]; ok {
			fa.lines = fs.TotalLines
		}
	}

	var paths []RankedHotPath
	for _, fa := range fileAggs {
		density := 0.0
		if fa.lines > 0 {
			density = float64(fa.ejects+fa.hotPaths) / float64(fa.lines) * 1000 // per 1000 lines
		}
		topCat := "mixed"
		topCount := 0
		for cat, count := range fa.cats {
			if count > topCount {
				topCat = cat
				topCount = count
			}
		}
		suggestions := suggestForFile(fa.path, topCat)
		paths = append(paths, RankedHotPath{
			File:         fa.path,
			Category:     topCat,
			EjectCount:   fa.ejects,
			HotPathCount: fa.hotPaths,
			Density:      density,
			Suggestions:  suggestions,
		})
	}
	sort.Slice(paths, func(i, j int) bool {
		return paths[i].Density > paths[j].Density
	})
	report.HotPathRanking = paths

	// Build ranked optimization targets
	targetWeights := map[string]struct {
		impact  string
		prio    int
		action  string
		details []string
	}{
		"cs-chunk-flush": {
			impact: "HIGH",
			prio:   1,
			action: "Increase CS chunk size from 16KB; batch contiguous CS commands before flush",
			details: []string{
				"Currently 16384-byte chunks trigger flush on overflow",
				"Consider batch-coalescing small state updates into fewer submissions",
				"Add DXVK_CS_CHUNK_SIZE=65536 for testing",
			},
		},
		"dxbc-compile": {
			impact: "HIGH",
			prio:   2,
			action: "Extend DxbcCompiler to emit native FP16 types for half/min16float; cache compiled SPIR-V blobs by content hash",
			details: []string{
				"DxbcCompiler is 8467 lines; emitFloat16() path not implemented",
				"SPIR-V shader cache hits could skip recompilation for stable shaders",
				"Add Float16 capability in emitFloatConvert path",
			},
		},
		"spirv-emit": {
			impact: "HIGH",
			prio:   3,
			action: "Batch SpirvCodeBuffer writes; pre-size buffer for common instruction patterns; deduplicate type/const emission",
			details: []string{
				"SpirvModule generates 4117 lines of SPIR-V — every constf32/constf64 call emits an OpConstant",
				"Type deduplication already exists via defType cache, but constant dedup does not",
				"Pre-size m_code to avg shader size to avoid repeated realloc",
			},
		},
		"renderpass-spill": {
			impact: "MEDIUM",
			prio:   4,
			action: "Minimize render pass spills by deferring non-draw operations to end of pass",
			details: []string{
				"spillRenderPass occurs when non-draw operations interrupt an active RP",
				"Deferred clears + buffer copies should batch after RP ends",
				"Consider VK_KHR_dynamic_rendering to reduce RP begin/end overhead",
			},
		},
		"barrier-emit": {
			impact: "MEDIUM",
			prio:   5,
			action: "Coalesce adjacent pipeline barriers; use single barrier with combined stage/access masks",
			details: []string{
				"cmdPipelineBarrier called per-buffer/per-image — batch into one call",
				"EmitGraphicsBarrier → checkGraphicsHazards often triggers multiple barriers in sequence",
			},
		},
		"buffer-invalidation": {
			impact: "MEDIUM",
			prio:   6,
			action: "Track buffer invalidation regions to skip re-upload of unchanged data",
			details: []string{
				"Dirty-region tracking could reduce uploadBuffer calls",
				"invalidateBuffer currently forces full re-upload",
			},
		},
		"copy-operation": {
			impact: "LOW",
			prio:   7,
			action: "Use SDMA (system DMA) queue for buffer copies when available",
			details: []string{
				"DxvkCommandList already has SdmaBuffer/SdmaBarriers support",
				"Ensure CopyBuffer routes through SDMA path when possible",
			},
		},
		"present-latency": {
			impact: "LOW",
			prio:   8,
			action: "Tune DxvkLatencyTracker frame pacing; consider reflex-style adaptive polling",
			details: []string{
				"beginLatencyTracking/endLatencyTracking incur timestamp queries",
				"On Adreno, timestamp queries have 52ns granularity — minimal overhead",
			},
		},
	}

	var targets []RankedTarget
	for _, ot := range analysis.OptTargets {
		tw, ok := targetWeights[ot.Name]
		if !ok {
			continue
		}
		targets = append(targets, RankedTarget{
			Name:     ot.Name,
			Impact:   tw.impact,
			Priority: tw.prio,
			Hits:     ot.Count,
			Files:    ot.Files,
			Action:   tw.action,
			Details:  tw.details,
		})
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Priority < targets[j].Priority })
	report.OptimizationTargets = targets

	// Quick wins
	report.QuickWins = []string{
		"1. CS chunk size: bump DxvkCsChunk from 16KB to 64KB for fewer flushes (impact: CS thread CPU, risk: low)",
		"2. FP16 shader types: wire constf16() into DxbcCompiler for half/min16float (impact: GPU ALU throughput, risk: medium)",
		"3. State cache dedup: extend m_gpLookupCache to use hash-based pre-check before full pipeline compile (impact: CPU per-draw, risk: low)",
		"4. Barrier coalesce: batch emitGraphicsBarrier calls into single cmdPipelineBarrier with combined masks (impact: CPU + GPU command stream, risk: low)",
		"5. Render pass batching: defer clear/copy ops to end of frame to minimize spillRenderPass calls (impact: GPU render pass overhead, risk: low)",
		"6. Uniform FP16 packing: SIEVE-cached pushConstants path already patched — enable via DXVK_FP16_UNIFORM=1 (impact: UBO bandwidth, risk: low)",
		"7. Vertex FP16: wire fp16CompressibleVertexFormat into D3D9/D3D11 vertex buffer binding (impact: VB bandwidth, risk: low)",
		"8. Buffer invalidation tracking: dirty-region bitmap to skip re-upload of stable buffer ranges (impact: CPU upload, risk: medium)",
	}

	// Architecture notes
	report.ArchitectureNotes = []string{
		"DXVK translation pipeline: D3D API → EmitCs (CS chunk) → DxvkContext → DxvkCommandList (vkCmd*) → Vulkan driver",
		"Hot file: dxvk_context.cpp (9464 lines, 105 hot paths, 186 VK ejection sites) — THE optimization target",
		"CS thread decoupling: D3D device owns a DxvkCsThread that processes chunks async from application thread",
		"State caching: m_gpLookupCache[4096] and m_cpLookupCache[256] are direct-mapped hash tables for pipeline state",
		"Render pass management: Dynamic rendering start/spill managed by DxvkContext; spill triggers on non-draw operations",
		"Memory: DxvkMemoryAllocator manages heaps, types, chunks; buffer sub-allocation via DxvkPageAllocator/DxvkPoolAllocator",
		"FP16 patches applied: constf16() in SpirvModule, SieveCache<Ω> in util/sieve_cache.h, fp16PackData() in dxvk_uniform_fp16.h",
	}

	out, _ := json.MarshalIndent(report, "", "  ")
	os.WriteFile(*outputFile, out, 0644)

	fmt.Fprintf(os.Stderr, "Wrote bottleneck report to %s\n", *outputFile)

	// Print summary
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "╔════════════════════════════════════════════════════╗")
	fmt.Fprintln(os.Stderr, "║     DXVK BOTTLENECK ANALYSIS                      ║")
	fmt.Fprintln(os.Stderr, "╠════════════════════════════════════════════════════╣")
	fmt.Fprintf(os.Stderr, "║ Source: %d files, %d eject sites, %d hot paths    ║\n",
		len(analysis.FileStats), len(analysis.VulkanEjects), len(analysis.ContextHotPaths))
	fmt.Fprintln(os.Stderr, "╠════════════════════════════════════════════════════╣")
	fmt.Fprintln(os.Stderr, "║ RANKED OPTIMIZATION TARGETS                        ║")
	for _, t := range report.OptimizationTargets {
		fmt.Fprintf(os.Stderr, "║ [%s] %-28s (hits=%d, files=%d) ║\n", t.Impact, t.Name, t.Hits, t.Files)
	}
	fmt.Fprintln(os.Stderr, "╠════════════════════════════════════════════════════╣")
	fmt.Fprintln(os.Stderr, "║ QUICK WINS                                         ║")
	for _, w := range report.QuickWins {
		fmt.Fprintf(os.Stderr, "║ %s ║\n", w[:min(len(w), 50)])
	}
	fmt.Fprintln(os.Stderr, "╚════════════════════════════════════════════════════╝")
}

func suggestForFile(file string, category string) []string {
	suggestions := map[string][]string{
		"dxvk/dxvk_context.cpp": {
			"Primary optimization target — 105 hot paths, 186 VK ejection sites",
			"Focus: state dedup (m_gpLookupCache), barrier coalescing, render pass batching",
			"Consider splitting into smaller translation units for parallel compilation",
		},
		"d3d11/d3d11_context.cpp": {
			"D3D11→DXVK bridge — 5927 lines, 30 hot paths",
			"SRV/RTV/UAV hazard resolution (ResolveSrvHazards) is a known CPU hotspot",
			"Batch contiguous resource bindings to reduce descriptor set updates",
		},
		"d3d9/d3d9_device.cpp": {
			"Fixed-function pipeline emulation — 8995 lines",
			"D3D9DeviceFlag dirty tracking with 30+ flags — evaluate which flags are set most often",
			"Fixed-function shader compilation (D3D9FFShaderModuleSet) — cache by state hash",
		},
	}
	if s, ok := suggestions[file]; ok {
		return s
	}
	return []string{"Review hot path → ejection mapping for optimization opportunities"}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Ensure patterns package is importable
var _ = dxvk.ContextMethodCategories
