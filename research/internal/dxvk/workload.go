package dxvk

import (
	"fmt"
	"math/rand"
	"strings"

	"github.com/idans/winlator-cmod-builder/research/internal/fp16cache"
	"github.com/idans/winlator-cmod-builder/research/internal/pm4"
	"github.com/idans/winlator-cmod-builder/research/internal/stub"
)

// ================================================================
// D3D9/D3D10 → DXVK Workload Model
// ================================================================

type SceneConfig struct {
	Name          string  `json:"name"`
	API           string  `json:"api"`
	DrawCalls     int     `json:"draw_calls"`
	StatePerDraw  int     `json:"state_per_draw"`
	TextureBinds  int     `json:"texture_binds"`
	UBOBinds      int     `json:"ubo_binds"`
	Fp16Fraction  float64 `json:"fp16_fraction"`
	DuplicateRate float64 `json:"duplicate_rate"`
	PipelineSwaps int     `json:"pipeline_swaps"`
	Description   string  `json:"description"`
}

type D3DState struct {
	RenderStates map[int]int
	Textures     [8]uint64
	Indices      uint64
	UBOData      [8][]byte
}

func NewD3DState() *D3DState {
	s := &D3DState{RenderStates: make(map[int]int)}
	for i := range s.UBOData {
		s.UBOData[i] = make([]byte, 64)
	}
	return s
}

type D3DAction struct {
	Type  string  `json:"type"`
	Cost  float64 `json:"cost_us"`
	Dirty bool    `json:"dirty"`
}

type VkAction struct {
	Type   string               `json:"type"`
	Count  int                  `json:"count,omitempty"`
	Cost   float64              `json:"cost_us"`
	IsFp16 bool                 `json:"is_fp16,omitempty"`
	Entry  *pm4.SubmissionEntry `json:"-"`
}

type FrameTrace struct {
	Frame           int        `json:"frame"`
	D3DActions      []D3DAction `json:"d3d_actions"`
	VkActions       []VkAction  `json:"vk_actions"`
	CpuCostUs       float64    `json:"cpu_cost_us"`
	GpuCostUs       float64    `json:"gpu_cost_us"`
	StateDedups     int        `json:"state_dedups"`
	Fp16PackOps     uint64     `json:"fp16_pack_ops"`
	Fp16CacheHits   uint64     `json:"fp16_cache_hits"`
	Fp16CacheMisses uint64     `json:"fp16_cache_misses"`
}

type WorkloadResult struct {
	Scene             SceneConfig          `json:"scene"`
	Frames            int                  `json:"frames"`
	TotalD3DCalls     int                  `json:"total_d3d_calls"`
	TotalVkCalls      int                  `json:"total_vk_calls"`
	StateDedups       int                  `json:"state_dedups"`
	CpuCostTotalUs    float64              `json:"cpu_cost_total_us"`
	CpuCostPerFrameUs float64              `json:"cpu_cost_per_frame_us"`
	GpuCostTotalUs    float64              `json:"gpu_cost_total_us"`
	GpuCostPerFrameUs float64              `json:"gpu_cost_per_frame_us"`
	Fp16PackOps       uint64               `json:"fp16_pack_ops"`
	Fp16CacheHits     uint64               `json:"fp16_cache_hits"`
	Fp16CacheMisses   uint64               `json:"fp16_cache_misses"`
	Fp16CacheHitRate  float64              `json:"fp16_cache_hit_rate"`
	Engine            *stub.GPUEngine      `json:"-"`
	Config            stub.ExperimentConfig `json:"-"`
}

// ================================================================
// Translation cost model
// ================================================================

var d3dCosts = map[string]float64{
	"SetRenderState": 0.05, "SetTexture": 0.12, "SetStreamSource": 0.08,
	"SetIndices": 0.06, "DrawPrimitive": 0.25, "DrawIndexedPrimitive": 0.30,
	"BeginScene": 0.02, "EndScene": 0.05, "Present": 0.50,
}

var renderStates = []int{7, 8, 14, 19, 22, 27, 34, 35, 52, 53, 168}

// UBO stability tiers: probability that this slot's content changes per draw.
// Higher = more stable (less likely to change).
var uboTiers = []float64{0.3, 0.1, 0.02, 0.005, 0.001, 0.0005, 0.0001, 0.0}

// ================================================================
// Generator
// ================================================================

type D3DWorkloadGenerator struct {
	rng            *rand.Rand
	state          *D3DState
	fp16Cache      *fp16cache.SieveCache
	drawCounter    uint64
	uboCurrentHash [8]uint64
}

func NewD3DWorkloadGenerator(seed int64) *D3DWorkloadGenerator {
	g := &D3DWorkloadGenerator{
		rng:       rand.New(rand.NewSource(seed)),
		state:     NewD3DState(),
		fp16Cache: fp16cache.NewSieveCache(256),
	}
	for i := range g.uboCurrentHash {
		g.uboCurrentHash[i] = uint64(seed + int64(i)*10007)
	}
	return g
}

func (g *D3DWorkloadGenerator) GenerateFrame(scene SceneConfig, frame int, engine *stub.GPUEngine) *FrameTrace {
	t := &FrameTrace{Frame: frame}

	t.addD3D("BeginScene")

	for d := 0; d < scene.DrawCalls; d++ {
		g.drawCounter++

		// Mutate UBO hashes with per-draw probability based on stability tier
		for s := 0; s < len(uboTiers) && s < maxInt(scene.UBOBinds, 4); s++ {
			if g.rng.Float64() < uboTiers[s] {
				g.uboCurrentHash[s] = uint64(frame)*1000000 + uint64(s)*10007 + g.drawCounter
			}
		}

		// State changes with duplicate rate
		for s := 0; s < scene.StatePerDraw; s++ {
			cost := d3dCosts["SetRenderState"]
			dirty := true
			idx := g.rng.Intn(len(renderStates))
			st := renderStates[idx]

			if g.rng.Float64() < scene.DuplicateRate {
				if _, ok := g.state.RenderStates[st]; ok {
					cost *= 0.3
					dirty = false
				}
			}
			if dirty {
				g.state.RenderStates[st] = g.rng.Intn(100)
			}
			t.addD3DDirty("SetRenderState", cost, dirty)
			if !dirty {
				t.StateDedups++
			}
		}

		// Texture binds with handle reuse
		for tex := 0; tex < scene.TextureBinds; tex++ {
			stage := tex % 8
			handle := resHandle(frame, d, tex, scene.DuplicateRate, g.rng)
			cost := d3dCosts["SetTexture"]
			dirty := true
			if g.state.Textures[stage] == handle {
				cost *= 0.3
				dirty = false
				t.StateDedups++
			}
			g.state.Textures[stage] = handle
			t.addD3DDirty("SetTexture", cost, dirty)
		}

		// Vertex/index binds
		t.addD3DDirty("SetStreamSource", d3dCosts["SetStreamSource"], true)
		t.addD3DDirty("SetIndices", d3dCosts["SetIndices"], true)

		// UBO binds
		uboCount := scene.UBOBinds
		if uboCount == 0 && scene.Fp16Fraction > 0 {
			uboCount = maxInt(scene.TextureBinds, 2)
		}
		ubosFp16 := false
		for u := 0; u < uboCount; u++ {
			if g.uboBind(t, u, scene) {
				ubosFp16 = true
			}
		}

		// Draw
		dc := d3dCosts["DrawPrimitive"]
		if g.rng.Intn(100) < 30 {
			dc = d3dCosts["DrawIndexedPrimitive"]
		}
		t.addD3DDirty("DrawPrimitive", dc, true)

		// GPU stub entry
		e := g.drawVkEntry(scene)
		e.IsFp16Compute = ubosFp16
		e.Packets, _ = pm4.Decode(e.Dwords)
		engine.ProcessEntry(e)
		t.addVk("draw", 1, estDrawCost(scene), e)
	}

	t.addD3D("EndScene")
	t.addD3D("Present")

	for _, a := range t.D3DActions {
		t.CpuCostUs += a.Cost
	}
	for _, v := range t.VkActions {
		t.GpuCostUs += v.Cost
	}
	return t
}

func (g *D3DWorkloadGenerator) uboBind(t *FrameTrace, slot int, scene SceneConfig) bool {
	if scene.Fp16Fraction <= 0 || g.rng.Float64() >= scene.Fp16Fraction {
		return false
	}
	hash := g.uboCurrentHash[slot%len(uboTiers)]
	if _, hit := g.fp16Cache.Get(hash); hit {
		t.Fp16CacheHits++
		return true
	}
	t.Fp16CacheMisses++
	packed := make([]byte, 32)
	g.fp16Cache.Put(hash, packed, 32)
	t.Fp16PackOps++
	return true
}

func (g *D3DWorkloadGenerator) drawVkEntry(scene SceneConfig) *pm4.SubmissionEntry {
	var dw []uint32
	dw = append(dw, pkt7(pm4.CP_SET_MARKER, 2)...)
	n := 8 + g.rng.Intn(12)
	for i := 0; i < n; i++ {
		dw = append(dw, pkt4(uint16(0x2000+g.rng.Intn(100)), g.rng.Intn(2)+1)...)
	}
	dn := scene.TextureBinds
	if scene.UBOBinds > 0 {
		dn += scene.UBOBinds
	}
	for i := 0; i < dn; i++ {
		dw = append(dw, pkt4Fixed(uint16(0x0000+g.rng.Intn(4)), uint32(g.rng.Intn(128)))...)
	}
	if g.rng.Intn(100) < 40 {
		dw = append(dw, pkt7(pm4.CP_WAIT_FOR_IDLE, 0)...)
	}
	dw = append(dw, pkt7(pm4.CP_DRAW_INDX, 6)...)
	return &pm4.SubmissionEntry{Name: "d3d_draw", Dwords: dw}
}

// ================================================================
// Helpers
// ================================================================

func resHandle(frame, draw, variant int, dupRate float64, rng *rand.Rand) uint64 {
	if rng.Float64() < dupRate && frame > 2 {
		return uint64((frame-1-rng.Intn(3))*100000 + draw*100 + variant + 100)
	}
	return uint64(frame*100000 + draw*100 + variant + 100)
}

func estDrawCost(scene SceneConfig) float64 {
	base := 5.0
	base += float64(scene.TextureBinds) * 1.5
	base += float64(scene.StatePerDraw) * 0.5
	return base
}

func maxInt(a, b int) int {
	if a > b { return a }
	return b
}

func (t *FrameTrace) addD3D(name string) {
	t.D3DActions = append(t.D3DActions, D3DAction{Type: name, Cost: d3dCosts[name]})
}
func (t *FrameTrace) addD3DDirty(name string, cost float64, dirty bool) {
	t.D3DActions = append(t.D3DActions, D3DAction{Type: name, Cost: cost, Dirty: dirty})
}
func (t *FrameTrace) addVk(typ string, cnt int, cost float64, entry *pm4.SubmissionEntry) {
	t.VkActions = append(t.VkActions, VkAction{Type: typ, Count: cnt, Cost: cost, Entry: entry})
}

// ================================================================
// PM4 packet builders
// ================================================================

func pkt4(reg uint16, cnt int) []uint32 {
	count := uint32(cnt)
	hdr := pm4.Type4Mask | count | (p(count) << 7) |
		((uint32(reg) & 0x3ffff) << 8) | (p(uint32(reg)) << 27)
	dw := []uint32{hdr}
	for i := 0; i < cnt; i++ {
		dw = append(dw, uint32(rand.Intn(256)))
	}
	return dw
}
func pkt4Fixed(reg uint16, val uint32) []uint32 {
	cnt := uint32(1)
	hdr := pm4.Type4Mask | cnt | (p(cnt) << 7) |
		((uint32(reg) & 0x3ffff) << 8) | (p(uint32(reg)) << 27)
	return []uint32{hdr, val}
}
func pkt7(opcode uint32, cnt int) []uint32 {
	count := uint32(cnt)
	hdr := pm4.Type7Mask | count | (p(count) << 15) |
		((opcode & 0x7f) << 16) | (p(opcode) << 23)
	dw := []uint32{hdr}
	for i := 0; i < cnt; i++ {
		dw = append(dw, uint32(rand.Intn(256)))
	}
	return dw
}
func p(val uint32) uint32 {
	val ^= val >> 16; val ^= val >> 8; val ^= val >> 4; val &= 0xf
	return (uint32(0x9669) >> val) & 1
}

// ================================================================
// Runner
// ================================================================

func RunWorkload(scene SceneConfig, frames int, seed int64, cfg stub.ExperimentConfig) *WorkloadResult {
	gen := NewD3DWorkloadGenerator(seed)
	memTracker := stub.NewMemoryTracker()
	engine := stub.NewGPUEngine(memTracker, cfg)
	r := &WorkloadResult{Scene: scene, Frames: frames, Engine: engine, Config: cfg}

	for f := 0; f < frames; f++ {
		trace := gen.GenerateFrame(scene, f, engine)
		r.TotalD3DCalls += len(trace.D3DActions)
		r.TotalVkCalls += len(trace.VkActions)
		r.StateDedups += trace.StateDedups
		r.Fp16PackOps += trace.Fp16PackOps
		r.Fp16CacheHits += trace.Fp16CacheHits
		r.Fp16CacheMisses += trace.Fp16CacheMisses
		r.CpuCostTotalUs += trace.CpuCostUs
		r.GpuCostTotalUs += trace.GpuCostUs
		if trace.Fp16CacheMisses > 0 {
			r.CpuCostTotalUs += float64(trace.Fp16CacheMisses) * 1.0
		}
		if trace.Fp16CacheHits > 0 {
			r.CpuCostTotalUs += float64(trace.Fp16CacheHits) * 0.05
		}
	}
	r.CpuCostPerFrameUs = r.CpuCostTotalUs / float64(frames)
	r.GpuCostPerFrameUs = r.GpuCostTotalUs / float64(frames)
	total := r.Fp16CacheHits + r.Fp16CacheMisses
	if total > 0 {
		r.Fp16CacheHitRate = float64(r.Fp16CacheHits) / float64(total)
	}
	return r
}

// ================================================================
// Scene presets
// ================================================================

var ScenePresets = []SceneConfig{
	{Name: "light_2d", API: "d3d9", DrawCalls: 100, StatePerDraw: 4, TextureBinds: 2, UBOBinds: 0, Fp16Fraction: 0.0, DuplicateRate: 0.9, PipelineSwaps: 10, Description: "Light 2D: 100 draws, 90% reuse"},
	{Name: "aaa_d3d9", API: "d3d9", DrawCalls: 1000, StatePerDraw: 12, TextureBinds: 4, UBOBinds: 0, Fp16Fraction: 0.6, DuplicateRate: 0.7, PipelineSwaps: 50, Description: "AAA D3D9: 1000 draws, 60% FP16"},
	{Name: "aaa_d3d10", API: "d3d10", DrawCalls: 800, StatePerDraw: 8, TextureBinds: 6, UBOBinds: 3, Fp16Fraction: 0.7, DuplicateRate: 0.75, PipelineSwaps: 40, Description: "AAA D3D10: 800 draws, 3 UBOs, 70% FP16"},
	{Name: "state_hvy", API: "d3d9", DrawCalls: 500, StatePerDraw: 20, TextureBinds: 2, UBOBinds: 0, Fp16Fraction: 0.4, DuplicateRate: 0.4, PipelineSwaps: 100, Description: "State-heavy: 500 draws, 40% reuse, 40% FP16"},
	{Name: "fp16_ubo", API: "d3d10", DrawCalls: 600, StatePerDraw: 6, TextureBinds: 4, UBOBinds: 4, Fp16Fraction: 0.85, DuplicateRate: 0.8, PipelineSwaps: 30, Description: "FP16 UBO: 600 draws, 4 UBOs, 85% FP16"},
}

// ================================================================
// Print
// ================================================================

func PrintWorkloadSummary(results []*WorkloadResult) {
	fmt.Println()
	fmt.Println("══════════════════════════════════════════════════════════════════════════════")
	fmt.Println("                   DXVK WORKLOAD SIMULATION RESULTS")
	fmt.Println("══════════════════════════════════════════════════════════════════════════════")
	fmt.Printf("%-14s │ %6s │ %7s │ %7s │ %7s │ %6s │ %7s\n",
		"Scene", "D3DCalls", "VKCalls", "CPU(us)", "GPU(us)", "Dedups", "FP16Hit%")
	fmt.Println("──────────────────────────────────────────────────────────────────────────────")
	for _, r := range results {
		cfg := "base"
		if r.Config.Fp16HalfCost {
			cfg = "fp16"
		}
		fmt.Printf("%-14s │ %6d │ %7d │ %7.0f │ %7.0f │ %6d │ %6.1f%%  [%s]\n",
			r.Scene.Name, r.TotalD3DCalls, r.TotalVkCalls,
			r.CpuCostPerFrameUs, r.GpuCostPerFrameUs,
			r.StateDedups, r.Fp16CacheHitRate*100, cfg)
	}
	fmt.Println("══════════════════════════════════════════════════════════════════════════════")
}

func PrintComparison(base, opt *WorkloadResult) {
	fmt.Println()
	fmt.Println("══════════════════════════════════════════════════════════════════════════════")
	fmt.Println("                   OPTIMIZATION COMPARISON")
	fmt.Println("══════════════════════════════════════════════════════════════════════════════")
	fmt.Printf("Scene: %s\n", base.Scene.Name)
	fmt.Println("──────────────────────────────────────────────────────────────────────────────")
	fmt.Printf("%-22s │ %10s │ %10s │ %8s\n", "Metric", "Baseline", "Optimized", "Delta")
	fmt.Println("──────────────────────────────────────────────────────────────────────────────")

	c := func(label string, b, o float64) {
		d := o - b
		pct := 0.0
		if b > 0 { pct = d / b * 100 }
		arrow := "→"
		if d < 0 { arrow = "↓" }
		fmt.Printf("%-22s │ %10.1f │ %10.1f │ %s%+.1f%%\n", label, b, o, arrow, pct)
	}
	cU := func(label string, b, o int) {
		d := o - b
		pct := 0.0
		if b > 0 { pct = float64(d) / float64(b) * 100 }
		arrow := "→"
		if d < 0 { arrow = "↓" }
		fmt.Printf("%-22s │ %10d │ %10d │ %s%+.1f%%\n", label, b, o, arrow, pct)
	}

	c("CPU/frame (us)", base.CpuCostPerFrameUs, opt.CpuCostPerFrameUs)
	c("GPU/frame (us)", base.GpuCostPerFrameUs, opt.GpuCostPerFrameUs)
	cU("State Dedups", base.StateDedups, opt.StateDedups)
	cU("FP16 Pack Ops", int(base.Fp16PackOps), int(opt.Fp16PackOps))

	es := opt.Engine.Stats()
	aluB := float64(es.Fp16Ops + es.Fp32Ops)
	aluW := es.WeightedALU
	aluS := 0.0
	if aluB > 0 { aluS = (aluB - aluW) / aluB * 100 }
	fmt.Printf("%-22s │ %10s │ %10.0f │ →%.1f%%\n", "GPU Weighted ALU", "-", aluW, aluS)
	fmt.Println("══════════════════════════════════════════════════════════════════════════════")
}

var _ = fmt.Sprintf
var _ = strings.Join
