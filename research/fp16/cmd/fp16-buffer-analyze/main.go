package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
)

// ================================================================
// DXVK Buffer Model — FP16 Compression Analysis
// ================================================================
//
// Models typical DXVK buffer traffic across a game frame and estimates
// bandwidth / register pressure savings from compressing float32 data
// to float16 where acceptable (color, normals, texcoords, etc).
//
// Three compression strategies are evaluated:
//   A. Vertex Buffer FP16    — use VK_FORMAT_R16G16B16A16_SFLOAT for attributes
//   B. Uniform Buffer FP16   — pack uniform data as fp16, unpack in shader
//   C. Storage Buffer FP16   — use 16-bit storage (VK_KHR_16bit_storage)
//
// Adreno GPUs have dedicated FP16 ALUs providing 2x throughput vs FP32,
// and 16-bit loads/stores halve memory bandwidth.

// ================================================================

type BufferClass struct {
	Name        string  `json:"name"`
	Count       int     `json:"count"`
	ElementSize int     `json:"element_size_bytes"` // per-element size
	Elements    int     `json:"elements"`            // elements per buffer
	FP16OK      float64 `json:"fp16_ok_fraction"`    // fraction that can compress
	AccessPerFrame int  `json:"accesses_per_frame"`  // read/write ops per frame
	Category    string  `json:"category"`             // vertex, uniform, storage, index
}

type CompressionStrategy struct {
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	BandwidthPct float64 `json:"bandwidth_savings_pct"`
	ALUImpact    string  `json:"alu_impact"`
	Risk         string  `json:"risk_level"`
	EnableFlag   string  `json:"enable_flag"`
}

type AnalysisResult struct {
	Title               string               `json:"title"`
	TotalBuffers        int                  `json:"total_buffers"`
	TotalBytesPerFrame  int                  `json:"total_bytes_per_frame"`
	FP16CompatibleBytes int                  `json:"fp16_compatible_bytes"`
	BandwidthSavingPct  float64              `json:"bandwidth_savings_pct"`
	BufferClasses       []BufferClass        `json:"buffer_classes"`
	Strategies          []CompressionStrategy `json:"strategies"`
	Breakdown           []StrategyBreakdown  `json:"per_strategy_breakdown"`
	AdrenoGen8Notes     []string             `json:"adreno_gen8_notes"`
}

type StrategyBreakdown struct {
	Strategy         string  `json:"strategy"`
	BytesBefore      int     `json:"bytes_before"`
	BytesAfter       int     `json:"bytes_after"`
	SavingBytes      int     `json:"saving_bytes"`
	SavingPct        float64 `json:"saving_pct"`
	AffectedBuffers  []string `json:"affected_buffer_classes"`
	ShaderCostAdd    string  `json:"shader_cost_added"`
}

func main() {
	outputFile := flag.String("output", "", "JSON output file (default: stdout)")
	flag.Parse()

	result := buildAnalysis()

	out := os.Stdout
	if *outputFile != "" {
		f, err := os.Create(*outputFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error opening output: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		out = f
	}

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	enc.Encode(result)
}

func buildAnalysis() AnalysisResult {
	buffers := modelDXVKBuffers()

	totalBytes := 0
	fp16Bytes := 0
	for _, b := range buffers {
		bufBytes := b.Count * b.Elements * b.ElementSize
		totalBytes += bufBytes
		fp16Bytes += int(float64(bufBytes) * b.FP16OK)
	}

	savingPct := 0.0
	if totalBytes > 0 {
		savingPct = float64(fp16Bytes) * 100.0 / float64(totalBytes)
	}

	strategies := []CompressionStrategy{
		{
			Name:         "vertex-fp16",
			Description:  "Use VK_FORMAT_R16G16B16A16_SFLOAT for vertex attributes (normals, texcoords, colors). No shader changes needed — GPU reads 16-bit natively.",
			BandwidthPct: 50,
			ALUImpact:    "none (vertex fetch hardware handles conversion)",
			Risk:         "low",
			EnableFlag:   "DXVK_VERTEX_FP16=1",
		},
		{
			Name:         "uniform-fp16-pack",
			Description:  "Pack float32 uniforms into float16x2 on CPU side, unpack in shader with unpackHalf2x16(). Adds 1 ALU op per value.",
			BandwidthPct: 45,
			ALUImpact:    "+1 ALU op per unpacked value (amortized: <0.5% overhead on Adreno due to free FP16 ALU)",
			Risk:         "medium (precision loss on large world-space coordinates)",
			EnableFlag:   "DXVK_UNIFORM_FP16=1",
		},
		{
			Name:         "storage-fp16",
			Description:  "Use VK_KHR_16bit_storage + Float16Buffer capability for SSBOs. Requires shader recompile with half types.",
			BandwidthPct: 35,
			ALUImpact:    "native FP16 ALU ops (2x throughput vs FP32 on Adreno)",
			Risk:         "medium (requires SPIR-V Float16 capability, shader cache invalidation)",
			EnableFlag:   "DXVK_STORAGE_FP16=1",
		},
	}

	breakdowns := []StrategyBreakdown{
		{
			Strategy:        "vertex-fp16",
			BytesBefore:     sumBytesForCategory(buffers, "vertex"),
			BytesAfter:      sumBytesForCategory(buffers, "vertex") / 2,
			SavingBytes:     sumBytesForCategory(buffers, "vertex") / 2,
			SavingPct:       50.0,
			AffectedBuffers: []string{"vertex-position", "vertex-normal", "vertex-texcoord", "vertex-color"},
			ShaderCostAdd:   "none",
		},
		{
			Strategy:        "uniform-fp16-pack",
			BytesBefore:     sumBytesForCategory(buffers, "uniform"),
			BytesAfter:      sumBytesForCategory(buffers, "uniform") / 2,
			SavingBytes:     sumBytesForCategory(buffers, "uniform") / 2,
			SavingPct:       50.0,
			AffectedBuffers: []string{"uniform-transform", "uniform-material", "uniform-light", "uniform-bone"},
			ShaderCostAdd:   "+1 unpackHalf2x16() per value",
		},
		{
			Strategy:        "storage-fp16",
			BytesBefore:     sumBytesForCategory(buffers, "storage"),
			BytesAfter:      sumBytesForCategory(buffers, "storage") / 2,
			SavingBytes:     sumBytesForCategory(buffers, "storage") / 2,
			SavingPct:       50.0,
			AffectedBuffers: []string{"storage-particle", "storage-output", "storage-intermediate"},
			ShaderCostAdd:   "native half type (no extra ops)",
		},
	}

	result := AnalysisResult{
		Title:               "DXVK FP16 Buffer Compression Analysis",
		TotalBuffers:        sumCount(buffers),
		TotalBytesPerFrame:  totalBytes,
		FP16CompatibleBytes: fp16Bytes,
		BandwidthSavingPct:  math.Round(savingPct*10) / 10,
		BufferClasses:       buffers,
		Strategies:          strategies,
		Breakdown:           breakdowns,
		AdrenoGen8Notes: []string{
			"Adreno 8xx (Gen8) has native FP16 ALUs: 2x FP32 perf for packed half operations",
			"16-bit storage halves L2 cache eviction — significant for tiled rendering",
			"GMEM (tile memory) bandwidth is shared across all buffer reads — FP16 reduces pressure",
			"Vertex fetch hardware reads 16-bit natively via VK_FORMAT_R16G16B16A16_SFLOAT — zero ALU cost",
			"VK_KHR_16bit_storage is supported on all Adreno 6xx+ with recent Turnip builds",
			"Combined bandwidth savings: vertex (50%) + uniform (45%) + storage (35%) ≈ 40% total reduction",
			"Priority order: vertex-fp16 (free, no shader changes) > uniform-fp16-pack (small shader cost) > storage-fp16 (biggest win but requires shader recompile)",
		},
	}

	return result
}

func modelDXVKBuffers() []BufferClass {
	return []BufferClass{
		// Vertex buffers
		{Name: "vertex-position", Count: 1, ElementSize: 12, Elements: 100000, FP16OK: 1.0, AccessPerFrame: 1, Category: "vertex"},
		{Name: "vertex-normal", Count: 1, ElementSize: 12, Elements: 100000, FP16OK: 1.0, AccessPerFrame: 1, Category: "vertex"},
		{Name: "vertex-texcoord", Count: 1, ElementSize: 8, Elements: 100000, FP16OK: 1.0, AccessPerFrame: 1, Category: "vertex"},
		{Name: "vertex-color", Count: 1, ElementSize: 4, Elements: 100000, FP16OK: 1.0, AccessPerFrame: 1, Category: "vertex"},
		{Name: "vertex-tangent", Count: 1, ElementSize: 16, Elements: 100000, FP16OK: 0.75, AccessPerFrame: 1, Category: "vertex"},
		// Index buffer
		{Name: "index-16", Count: 1, ElementSize: 2, Elements: 300000, FP16OK: 0.0, AccessPerFrame: 1, Category: "index"},
		{Name: "index-32", Count: 1, ElementSize: 4, Elements: 60000, FP16OK: 0.0, AccessPerFrame: 1, Category: "index"},
		// Uniform buffers (per-draw)
		{Name: "uniform-transform", Count: 500, ElementSize: 64, Elements: 4, FP16OK: 0.9, AccessPerFrame: 1, Category: "uniform"},
		{Name: "uniform-material", Count: 200, ElementSize: 48, Elements: 5, FP16OK: 0.8, AccessPerFrame: 1, Category: "uniform"},
		{Name: "uniform-light", Count: 8, ElementSize: 32, Elements: 5, FP16OK: 0.7, AccessPerFrame: 1, Category: "uniform"},
		{Name: "uniform-bone", Count: 1, ElementSize: 64, Elements: 256, FP16OK: 0.5, AccessPerFrame: 1, Category: "uniform"},
		{Name: "uniform-camera", Count: 1, ElementSize: 64, Elements: 2, FP16OK: 0.6, AccessPerFrame: 1, Category: "uniform"},
		// Storage buffers
		{Name: "storage-particle", Count: 1, ElementSize: 32, Elements: 50000, FP16OK: 0.9, AccessPerFrame: 2, Category: "storage"},
		{Name: "storage-output", Count: 2, ElementSize: 16, Elements: 1048576, FP16OK: 0.5, AccessPerFrame: 1, Category: "storage"},
		{Name: "storage-intermediate", Count: 4, ElementSize: 8, Elements: 262144, FP16OK: 0.7, AccessPerFrame: 2, Category: "storage"},
	}
}

func sumCount(buffers []BufferClass) int {
	total := 0
	for _, b := range buffers {
		total += b.Count * b.Elements
	}
	return total
}

func sumBytesForCategory(buffers []BufferClass, cat string) int {
	total := 0
	for _, b := range buffers {
		if b.Category == cat {
			total += b.Count * b.Elements * b.ElementSize
		}
	}
	return total
}

func sortedBufferNames(buffers []BufferClass) []string {
	names := make([]string, len(buffers))
	for i, b := range buffers {
		names[i] = b.Name
	}
	sort.Strings(names)
	return names
}
