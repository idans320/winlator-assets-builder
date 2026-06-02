package main

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/idans/winlator-cmod-builder/research/internal/pm4"
	"github.com/idans/winlator-cmod-builder/research/internal/stub"
)

type RenderPass struct {
	Name      string
	Draws     int
	Blits     int
	MRTs      int
	DepthOnly bool
	Compute   bool
	Description string
}

type PassStats struct {
	Name            string
	TotalDwords     int
	PKT4Packets     int
	PKT7Packets     int
	RegWrites       int
	A8XXRegWrites   int
	Barriers        int
	Draws           int
	RedundantWrites uint64
	BarriersSkipped uint64
	CpuMsEstimate   float64
}

func main() {
	scene := buildScene()
	rng := rand.New(rand.NewSource(42))

	configs := []struct {
		name string
		cfg  stub.ExperimentConfig
	}{
		{"baseline", stub.ExperimentConfig{}},
		{"dirty_shadow", stub.ExperimentConfig{RedundantWriteFilter: true, BarrierBypass: true}},
		{"reg_blast", stub.ExperimentConfig{RedundantWriteFilter: true, BarrierBypass: true}},
		{"yolo_sync", stub.ExperimentConfig{BarrierBypass: true}},
		{"all_on", stub.ExperimentConfig{RedundantWriteFilter: true, BarrierBypass: true}},
	}

	fmt.Println("╔══════════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║   REALISTIC 3D SCENE STREAM — Fake GPU with Fake KGSL                   ║")
	fmt.Println("║   JC2/DXVK → Turnip PM4 Pattern Simulation                             ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════════════════╣")
	fmt.Printf("║   Render Passes: %d   Total Draws: %d   Total Blits: %d               ║\n",
		len(scene), sumDraws(scene), sumBlits(scene))
	fmt.Println("╠══════════════════════════════════════════════════════════════════════════╣")

	for _, cfg := range configs {
		var allStats []PassStats
		totalStart := time.Now()

		for _, pass := range scene {
			mem := stub.NewMemoryTracker()
			engine := stub.NewGPUEngine(mem, cfg.cfg)

			entries := buildPass(pass, rng, cfg.name == "reg_blast")

			passStart := time.Now()
			for _, e := range entries {
				pkts, s := pm4.Decode(e.Dwords)
				e.Packets = pkts
				engine.ProcessSubmit([]*pm4.SubmissionEntry{e})
				_ = s
			}
			passElapsed := time.Since(passStart)

			stats := engine.Stats()
			ps := PassStats{
				Name:            pass.Name,
				TotalDwords:     totalDwords(entries),
				PKT4Packets:     countByType(entries, pm4.PacketPKT4),
				PKT7Packets:     countByType(entries, pm4.PacketPKT7),
				RegWrites:       sumRegWrites(entries),
				A8XXRegWrites:   sumA8XXWrites(entries),
				Barriers:        sumBarriers(entries),
				Draws:           entryDraws(entries),
				RedundantWrites: stats.RedundantWritesDropped,
				BarriersSkipped: stats.BarriersSkipped,
				CpuMsEstimate:   float64(passElapsed.Microseconds()) / 1000.0,
			}
			allStats = append(allStats, ps)
		}

		totalElapsed := time.Since(totalStart)
		totalRedundant := uint64(0)
		totalBarriersSkipped := uint64(0)
		totalPKT4 := 0
		totalPKT7 := 0
		for _, ps := range allStats {
			totalRedundant += ps.RedundantWrites
			totalBarriersSkipped += ps.BarriersSkipped
			totalPKT4 += ps.PKT4Packets
			totalPKT7 += ps.PKT7Packets
		}

		effPKT4 := totalPKT4 - int(totalRedundant)
		effPKT7 := totalPKT7 - int(totalBarriersSkipped)

		pctSavedP4 := float64(0.0)
		pctSavedP7 := float64(0.0)
		if cfg.name != "baseline" {
			if totalPKT4 > 0 {
				pctSavedP4 = float64(totalRedundant) / float64(totalPKT4) * 100
			}
			if totalPKT7 > 0 {
				pctSavedP7 = float64(totalBarriersSkipped) / float64(totalPKT7) * 100
			}
		}

		fmt.Printf("║                                                                          ║\n")
		fmt.Printf("║  %-15s  PKT4:%-6d PKT7:%-6d  dwords:%-7d  cpu:%.1fms    ║\n",
			cfg.name, totalPKT4, totalPKT7, totalRawDwords(allStats), float64(totalElapsed.Microseconds())/1000.0)
		if cfg.name != "baseline" {
			fmt.Printf("║  %-15s  ↓ eff PKT4:%-6d  redundant:%-4d (%.1f%%)  barr:%-3d→%-3d (%.1f%%)  ║\n",
				"", effPKT4, totalRedundant, pctSavedP4, totalBarriersSkipped, effPKT7, pctSavedP7)
		}
		fmt.Printf("║                                                                          ║\n")

		// Per-pass details for all_on
		if cfg.name == "all_on" {
			fmt.Printf("║  Per-pass breakdown (all_on):                                            ║\n")
			for _, ps := range allStats {
				fmt.Printf("║    %-28s dw:%-5d p4:%-3d p7:%-3d bar:%-2d redun:%-2d skip:%-2d ║\n",
					ps.Name, ps.TotalDwords, ps.PKT4Packets, ps.PKT7Packets, ps.Barriers, ps.RedundantWrites, ps.BarriersSkipped)
			}
		}
	}

	fmt.Println("╚══════════════════════════════════════════════════════════════════════════╝")
}

func buildScene() []RenderPass {
	return []RenderPass{
		{"shadow_map", 500, 0, 1, true, false, "Directional shadow: depth-only, small RT, many draws"},
		{"depth_prepass", 800, 0, 1, true, false, "Depth pre-pass: depth-only, full res, batch draws"},
		{"gbuffer_fill", 1000, 0, 4, false, false, "G-Buffer: MRT color+normal+spec+emissive, heavy state"},
		{"deferred_lighting", 200, 0, 0, false, true, "Deferred: compute shaders + fullscreen quads, sampler binds"},
		{"forward_pass", 300, 0, 1, false, false, "Forward: transparents, blend state, per-mat changes"},
		{"post_bloom", 2, 10, 0, false, false, "Bloom: downscale blits, A8XX_TEX_SAMP reconfig chain"},
		{"post_tonemap", 1, 5, 0, false, false, "Tonemap: LUT blits + fullscreen quad"},
		{"post_fxaa", 1, 3, 0, false, false, "FXAA: luminance edge detect + blend"},
	}
}

func sumDraws(passes []RenderPass) int {
	n := 0
	for _, p := range passes {
		n += p.Draws
	}
	return n
}

func sumBlits(passes []RenderPass) int {
	n := 0
	for _, p := range passes {
		n += p.Blits
	}
	return n
}

func buildPass(pass RenderPass, rng *rand.Rand, useBlast bool) []*pm4.SubmissionEntry {
	var entries []*pm4.SubmissionEntry

	// RP begin: binning + VSC setup
	entries = append(entries, buildRPBegin(rng))

	// Per-draw submissions
	for i := 0; i < pass.Draws; i++ {
		entries = append(entries, buildDrawSubmission(pass, rng, i))

		// Pipeline barrier every N draws
		if i%10 == 0 {
			entries = append(entries, buildBarrier(rng))
		}

		// Pipeline bind every 50 draws
		if i%50 == 0 {
			entries = append(entries, buildPipelineBind(pass, rng))
		}
	}

	// Blit operations
	for i := 0; i < pass.Blits; i++ {
		entries = append(entries, buildBlitSubmission(rng, useBlast))
		entries = append(entries, buildBarrier(rng))
	}

	// RP end: resolve + flush
	entries = append(entries, buildRPEnd(rng))

	return entries
}

func buildRPBegin(rng *rand.Rand) *pm4.SubmissionEntry {
	var dwords []uint32
	// Binning pass markers (gen8 CP_SET_MARKER)
	dwords = append(dwords, buildPkt7(0x65, 1)...) // CP_SET_MARKER
	dwords = append(dwords, rng.Uint32())
	// VSC config (tu6_lazy_init_vsc)
	for i := 0; i < 4; i++ {
		dwords = append(dwords, buildPkt4(0x0c00+uint16(i), 1)...)
		dwords = append(dwords, rng.Uint32())
	}
	// GMEM bin size
	for i := 0; i < 6; i++ {
		dwords = append(dwords, buildPkt4(0x2100+uint16(i), 1)...)
		dwords = append(dwords, rng.Uint32())
	}
	return &pm4.SubmissionEntry{Name: "rp_begin", Dwords: dwords}
}

func buildDrawSubmission(pass RenderPass, rng *rand.Rand, drawIdx int) *pm4.SubmissionEntry {
	var dwords []uint32

	// State setup per draw (varies with MRT count, depth, compute)
	nRegs := 30
	if pass.MRTs > 1 {
		nRegs += pass.MRTs * 10 // MRT config: RB_MRT_BUF_INFO, RB_MRT_PITCH, etc
	}
	if !pass.DepthOnly && !pass.Compute {
		nRegs += 15 // Blend state, alpha test
	}

	// Render backend state
	for i := 0; i < nRegs; i++ {
		reg := uint16(0x2000 + rng.Intn(200))
		dwords = append(dwords, buildPkt4(reg, 1)...)
		dwords = append(dwords, rng.Uint32()&0xFFFF)
	}

	// Descriptors (samplers, UBOs)
	for i := 0; i < pass.MRTs; i++ {
		for _, sreg := range []uint16{0, 1, 2} {
			dwords = append(dwords, buildPkt4(uint16(i*4+int(sreg)), 1)...)
			dwords = append(dwords, staticSamplerValue(sreg))
		}
	}

	// WFI barrier
	dwords = append(dwords, buildPkt7(0x26, 0)...) // CP_WAIT_FOR_IDLE

	// Draw command
	dwords = append(dwords, buildPkt7(0x22, 6)...) // CP_DRAW_INDX
	for i := 0; i < 6; i++ {
		dwords = append(dwords, uint32(drawIdx*100+i))
	}

	return &pm4.SubmissionEntry{Name: "draw", Dwords: dwords}
}

func buildPipelineBind(pass RenderPass, rng *rand.Rand) *pm4.SubmissionEntry {
	var dwords []uint32
	// CP_SET_DRAW_STATE with all state groups
	for i := 0; i < 8; i++ {
		dwords = append(dwords, buildPkt4(0x2000+uint16(i*3), 1)...)
		dwords = append(dwords, rng.Uint32())
	}
	// SP state
	for i := 0; i < 4; i++ {
		dwords = append(dwords, buildPkt4(0x3000+uint16(i), 1)...)
		dwords = append(dwords, rng.Uint32())
	}
	return &pm4.SubmissionEntry{Name: "pipeline_bind", Dwords: dwords}
}

func buildBlitSubmission(rng *rand.Rand, useBlast bool) *pm4.SubmissionEntry {
	var dwords []uint32

	// A8XX_TEX_SAMP reconfig — IDENTICAL values every blit (matching real driver behavior)
	// This is the 843× redundant write bug from source analysis.
	// Every blit reconfigures these samplers to the exact same values.
	for i := 0; i < 8; i++ {
		reg := uint16(i)
		dwords = append(dwords, buildPkt4(reg, 1)...)
		dwords = append(dwords, staticSamplerValue(reg))
	}

	// A8XX_TEX_MEMOBJ — base address changes, rest is identical
	dwords = append(dwords, buildPkt4(0, 1)...)
	dwords = append(dwords, rng.Uint32()|0x1000) // varying base
	for i := uint16(1); i < 4; i++ {
		dwords = append(dwords, buildPkt4(i, 1)...)
		dwords = append(dwords, staticMemobjValue(i)) // identical
	}

	// Blit constants
	for i := 0; i < 12; i++ {
		reg := uint16(0x2000 + rng.Intn(100))
		dwords = append(dwords, buildPkt4(reg, 1)...)
		dwords = append(dwords, rng.Uint32())
	}

	// CP_BLIT
	dwords = append(dwords, buildPkt7(0x2c, 6)...) // CP_BLIT
	for i := 0; i < 6; i++ {
		dwords = append(dwords, rng.Uint32())
	}

	return &pm4.SubmissionEntry{Name: "blit", Dwords: dwords}
}

func buildRPEnd(rng *rand.Rand) *pm4.SubmissionEntry {
	var dwords []uint32
	// GMEM resolve
	dwords = append(dwords, buildPkt7(0x65, 1)...) // CP_SET_MARKER
	dwords = append(dwords, rng.Uint32())
	// Cache flush + WFI
	dwords = append(dwords, buildPkt7(0x46, 4)...) // CP_EVENT_WRITE (CACHE_FLUSH_TS)
	dwords = append(dwords, 0x1f, 0, 0, 0)
	dwords = append(dwords, buildPkt7(0x26, 0)...) // WFI
	return &pm4.SubmissionEntry{Name: "rp_end", Dwords: dwords}
}

func buildBarrier(rng *rand.Rand) *pm4.SubmissionEntry {
	var dwords []uint32
	dwords = append(dwords, buildPkt7(0x26, 0)...) // WFI
	dwords = append(dwords, buildPkt7(0x13, 0)...) // CP_WAIT_FOR_ME
	return &pm4.SubmissionEntry{Name: "barrier", Dwords: dwords}
}

func buildPkt4(reg uint16, cnt int) []uint32 {
	count := uint32(cnt)
	hdr := pm4.Type4Mask | count | (parity(count) << 7) |
		((uint32(reg) & 0x3ffff) << 8) | (parity(uint32(reg)) << 27)
	return []uint32{hdr}
}

func buildPkt7(opcode uint32, cnt int) []uint32 {
	count := uint32(cnt)
	hdr := pm4.Type7Mask | count | (parity(count) << 15) |
		((opcode & 0x7f) << 16) | (parity(opcode) << 23)
	return []uint32{hdr}
}

func parity(val uint32) uint32 {
	val ^= val >> 16; val ^= val >> 8; val ^= val >> 4; val &= 0xf
	return (uint32(0x9669) >> val) & 1
}

func staticSamplerValue(reg uint16) uint32 {
	vals := map[uint16]uint32{
		0: 0x08000000, 1: 0x00004000, 2: 0x00000080, 3: 0x00000000,
		4: 0x00000000, 5: 0x00000000, 6: 0x00000000, 7: 0x00000000,
	}
	if v, ok := vals[reg]; ok {
		return v
	}
	return 0
}

func staticMemobjValue(reg uint16) uint32 {
	vals := map[uint16]uint32{
		0: 0x01234567, 1: 0x00000001, 2: 0x01000100, 3: 0x00000001,
	}
	if v, ok := vals[reg]; ok {
		return v
	}
	return 0
}

func totalRawDwords(stats []PassStats) int {
	n := 0
	for _, s := range stats {
		n += s.TotalDwords
	}
	return n
}

func totalDwords(entries []*pm4.SubmissionEntry) int {
	n := 0
	for _, e := range entries {
		n += len(e.Dwords)
	}
	return n
}

func countByType(entries []*pm4.SubmissionEntry, t pm4.PacketType) int {
	n := 0
	for _, e := range entries {
		for _, p := range e.Packets {
			if p.Type == t {
				n++
			}
		}
	}
	return n
}

func sumRegWrites(entries []*pm4.SubmissionEntry) int {
	n := 0
	for _, e := range entries {
		for _, p := range e.Packets {
			if p.Type == pm4.PacketPKT4 {
				n += p.RegCount
			}
		}
	}
	return n
}

func sumA8XXWrites(entries []*pm4.SubmissionEntry) int {
	n := 0
	for _, e := range entries {
		for _, p := range e.Packets {
			if p.Type == pm4.PacketPKT4 && pm4.IsA8XXRegister[p.RegOffset] {
				n += p.RegCount
			}
		}
	}
	return n
}

func sumBarriers(entries []*pm4.SubmissionEntry) int {
	n := 0
	for _, e := range entries {
		for _, p := range e.Packets {
			if p.Type == pm4.PacketPKT7 && pm4.BarrierOpcodes[p.Opcode] {
				n++
			}
		}
	}
	return n
}

func entryDraws(entries []*pm4.SubmissionEntry) int {
	n := 0
	for _, e := range entries {
		for _, p := range e.Packets {
			if p.Type == pm4.PacketPKT7 && pm4.DrawOpcodes[p.Opcode] {
				n++
			}
		}
	}
	return n
}
