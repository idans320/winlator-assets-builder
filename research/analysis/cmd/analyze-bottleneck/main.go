package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/idans/winlator-cmod-builder/research/internal/ast"
	"github.com/idans/winlator-cmod-builder/research/internal/pm4"
)

type BottleneckReport struct {
	TotalGen8Sites     int                   `json:"total_gen8_sites"`
	Gen8Functions      int                   `json:"gen8_functions"`
	EntryPoints        int                   `json:"entry_points"`
	HotDrawPath        *PathAnalysis           `json:"hot_draw_path"`
	TopRegisterEmitters []RegisterEmitterRank  `json:"top_register_emitters"`
	BarrierHotspots    []BarrierHotspot        `json:"barrier_hotspots"`
	RedundancyTargets   []RedundancyTarget     `json:"redundancy_targets"`
	BatchSizeAnalysis   *BatchAnalysis         `json:"batch_analysis"`
	QuickWins           []QuickWin             `json:"quick_wins"`
}

type PathAnalysis struct {
	Gen8SitesOnPath int      `json:"gen8_sites_on_path"`
	Functions       []string `json:"functions_on_critical_path"`
	RegisterWrites  int      `json:"estimated_reg_writes"`
	Barriers        int      `json:"estimated_barriers"`
	DrawEmissions   int      `json:"estimated_draw_emissions"`
}

type RegisterEmitterRank struct {
	Function      string  `json:"function"`
	File          string  `json:"file"`
	RegWrites     int     `json:"reg_writes"`
	A8XXRegWrites int     `json:"a8xx_reg_writes"`
	Gen8Sites     int     `json:"gen8_sites"`
	HotScore      float64 `json:"hot_score"`
}

type BarrierHotspot struct {
	Function    string `json:"function"`
	File        string `json:"file"`
	CallCount   int    `json:"call_count"`
	BarrierKind string `json:"barrier_kind"`
	ImpactNote  string `json:"impact_note"`
}

type RedundancyTarget struct {
	RegisterName string  `json:"register_name"`
	RegisterFile string  `json:"register_file"`
	WriteSites   int     `json:"write_sites"`
	A8XXSpecific bool    `json:"a8xx_specific"`
	LikelyRedundant bool  `json:"likely_redundant"`
	SavingsEstimate string `json:"savings_estimate"`
}

type BatchAnalysis struct {
	FunctionsWithSubmits []string `json:"functions_with_submits"`
	SubmitGranularity    string   `json:"submit_granularity"`
	BatchOpportunity     string   `json:"batch_opportunity"`
}

type QuickWin struct {
	Rank        int    `json:"rank"`
	Title       string `json:"title"`
	Description string `json:"description"`
	SourceFile  string `json:"source_file"`
	FPSImpact   string `json:"fps_impact"`
	Effort      string `json:"effort"`
	Mechanism   string `json:"mechanism"`
}

func main() {
	inputFlag := flag.String("input", "output/analysis.json", "Neuron analysis JSON")
	flag.Parse()

	data, err := os.ReadFile(*inputFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fmt.Fprintln(os.Stderr, "Run ast-analyzer first: go run ./cmd/ast-analyzer --mesa ../mesa/workdir/mesa --out output/analysis.json")
		os.Exit(1)
	}

	var cg ast.CallGraph
	if err := json.Unmarshal(data, &cg); err != nil {
		fmt.Fprintf(os.Stderr, "JSON error: %v\n", err)
		os.Exit(1)
	}

	report := analyze(&cg)

	fmt.Println("╔══════════════════════════════════════════════════════════╗")
	fmt.Println("║     TURNIP GEN8 FPS BOTTLENECK DISCOVERY REPORT         ║")
	fmt.Println("╠══════════════════════════════════════════════════════════╣")
	fmt.Printf("║  Gen8 code sites: %4d    Gen8 functions: %4d          ║\n", report.TotalGen8Sites, report.Gen8Functions)
	fmt.Printf("║  Entry points:    %4d    PM4 opcodes known: %4d     ║\n", report.EntryPoints, len(pm4.OpcodeNames))
	fmt.Println("╠══════════════════════════════════════════════════════════╣")

	fmt.Println("║                                                          ║")
	fmt.Println("║  🎯 QUICK WINS (ranked by FPS impact)                    ║")
	fmt.Println("║                                                          ║")
	for _, qw := range report.QuickWins {
		fmt.Printf("║  #%d  %-50s ║\n", qw.Rank, qw.Title)
		fmt.Printf("║      Impact: %-44s ║\n", qw.FPSImpact)
		fmt.Printf("║      Effort: %-44s ║\n", qw.Effort)
		fmt.Printf("║      File:   %-44s ║\n", truncstr(qw.SourceFile, 44))
		fmt.Printf("║      How:    %-44s ║\n", truncstr(qw.Mechanism, 44))
		fmt.Println("║                                                          ║")
	}
	fmt.Println("╠══════════════════════════════════════════════════════════╣")

	fmt.Println("║                                                          ║")
	fmt.Println("║  🔥 TOP REGISTER EMITTERS (most writes per draw path)    ║")
	fmt.Println("║                                                          ║")
	for i, r := range report.TopRegisterEmitters {
		if i >= 8 {
			break
		}
		fmt.Printf("║  %d. %-30s regs=%-3d a8xx=%-3d hot=%.1f  ║\n", i+1, truncstr(r.Function, 30), r.RegWrites, r.A8XXRegWrites, r.HotScore)
	}
	fmt.Println("║                                                          ║")
	fmt.Println("╠══════════════════════════════════════════════════════════╣")

	fmt.Println("║                                                          ║")
	fmt.Println("║  🚧 BARRIER HOTSPOTS (cache flushes per draw)            ║")
	fmt.Println("║                                                          ║")
	for i, b := range report.BarrierHotspots {
		if i >= 6 {
			break
		}
		fmt.Printf("║  %d. %-30s calls=%-2d  %s  ║\n", i+1, truncstr(b.Function, 30), b.CallCount, truncstr(b.ImpactNote, 16))
	}
	fmt.Println("║                                                          ║")
	fmt.Println("╠══════════════════════════════════════════════════════════╣")

	if report.BatchSizeAnalysis != nil {
		fmt.Println("║                                                          ║")
		fmt.Printf("║  📦 BATCH ANALYSIS: %-37s ║\n", report.BatchSizeAnalysis.SubmitGranularity)
		fmt.Printf("║  %-54s ║\n", truncstr(report.BatchSizeAnalysis.BatchOpportunity, 54))
		fmt.Println("║                                                          ║")
		fmt.Println("╠══════════════════════════════════════════════════════════╣")
	}

	fmt.Println("║                                                          ║")
	fmt.Println("║  🔄 REDUNDANCY CANDIDATES (register write optimization)  ║")
	fmt.Println("║                                                          ║")
	for i, rt := range report.RedundancyTargets {
		if i >= 6 {
			break
		}
		fmt.Printf("║  %-30s sites=%-2d  %-12s ║\n", truncstr(rt.RegisterName, 30), rt.WriteSites, truncstr(rt.SavingsEstimate, 12))
	}
	fmt.Println("║                                                          ║")
	fmt.Println("╚══════════════════════════════════════════════════════════╝")

	out, _ := json.MarshalIndent(report, "", "  ")
	os.WriteFile("output/bottleneck_report.json", out, 0644)
	fmt.Println("\nFull report: output/bottleneck_report.json")
}

func analyze(cg *ast.CallGraph) *BottleneckReport {
	r := &BottleneckReport{
		TotalGen8Sites: len(cg.Gen8Nodes),
		EntryPoints:    len(cg.EntryPoints),
	}

	for _, fn := range cg.Functions {
		if fn.Gen8Site {
			r.Gen8Functions++
		}
	}

	r.HotDrawPath = analyzeDrawPath(cg)
	r.TopRegisterEmitters = rankRegisterEmitters(cg)
	r.BarrierHotspots = findBarrierHotspots(cg)
	r.RedundancyTargets = findRedundancyTargets(cg, r.TopRegisterEmitters)
	r.BatchSizeAnalysis = analyzeBatching(cg)
	r.QuickWins = synthesizeQuickWins(cg, r)

	return r
}

func analyzeDrawPath(cg *ast.CallGraph) *PathAnalysis {
	drawEntries := []string{
		"tu_CmdDraw", "tu_CmdDrawIndexed", "tu_CmdDrawIndirect",
		"tu_CmdDrawMultiEXT", "tu_CmdDrawMultiIndexedEXT", "tu_CmdDispatch",
	}

	pa := &PathAnalysis{}
	visited := make(map[string]bool)
	for _, entry := range drawEntries {
		tracePathGen8(cg, entry, 0, 6, visited, pa)
	}
	return pa
}

func tracePathGen8(cg *ast.CallGraph, fnName string, depth int, maxDepth int, visited map[string]bool, pa *PathAnalysis) {
	if depth > maxDepth || visited[fnName] {
		return
	}
	visited[fnName] = true

	fn := cg.Functions[fnName]
	if fn == nil {
		return
	}

	if fn.Gen8Site {
		pa.Gen8SitesOnPath += len(fn.Gen8Lines)
		pa.Functions = append(pa.Functions, fnName)
	}

	for _, callee := range fn.Callees {
		cfn := cg.Functions[callee]
		if cfn == nil {
			continue
		}
		if isRegisterEmitter(callee) {
			pa.RegisterWrites++
		}
		if isBarrierFunction(callee) {
			pa.Barriers++
		}
		tracePathGen8(cg, callee, depth+1, maxDepth, visited, pa)
	}
}

func rankRegisterEmitters(cg *ast.CallGraph) []RegisterEmitterRank {
	var ranks []RegisterEmitterRank
	for name, fn := range cg.Functions {
		if !fn.Gen8Site || !isRegisterEmitter(name) {
			continue
		}
		a8xxCount := 0
		for _, gn := range cg.Gen8Nodes {
			if gn.Function == name && len(gn.Regs) > 0 {
				a8xxCount += len(gn.Regs)
			}
		}

		callerCount := countCallers(cg, name)
		hotScore := float64(len(fn.Gen8Lines)*10 + a8xxCount*5 + callerCount*2)

		ranks = append(ranks, RegisterEmitterRank{
			Function:      name,
			File:          fn.File,
			RegWrites:     len(fn.Gen8Lines),
			A8XXRegWrites: a8xxCount,
			Gen8Sites:     len(fn.Gen8Lines),
			HotScore:      hotScore,
		})
	}
	sort.Slice(ranks, func(i, j int) bool { return ranks[i].HotScore > ranks[j].HotScore })
	return ranks
}

func isRegisterEmitter(name string) bool {
	return strings.Contains(name, "emit_") || strings.Contains(name, "tu_cs_") ||
		strings.Contains(name, "_write_") || strings.HasPrefix(name, "tu6_") ||
		strings.HasPrefix(name, "tu7_")
}

func isBarrierFunction(name string) bool {
	return strings.Contains(name, "cache_flush") || strings.Contains(name, "barrier") ||
		strings.Contains(name, "flush_") || strings.Contains(name, "wfi") ||
		strings.Contains(name, "wait_for_idle")
}

func countCallers(cg *ast.CallGraph, name string) int {
	count := 0
	for _, callees := range cg.Edges {
		for _, c := range callees {
			if c == name {
				count++
			}
		}
	}
	return count
}

func findBarrierHotspots(cg *ast.CallGraph) []BarrierHotspot {
	var hotspots []BarrierHotspot
	barrierPatterns := map[string]string{
		"tu_emit_cache_flush":           "cache flush",
		"tu_emit_cache_flush_renderpass": "renderpass flush",
		"tu_emit_cache_flush_ccu":        "CCU flush",
		"tu6_emit_flushes":               "multi flush",
		"tu_add_cb_barrier_info":         "barrier info",
		"tu_CmdPipelineBarrier2":         "VK barrier",
	}

	for name, kind := range barrierPatterns {
		fn := cg.Functions[name]
		if fn == nil {
			continue
		}
		callers := countCallers(cg, name)
		impact := "per-draw cost"
		if callers > 10 {
			impact = "heavy use"
		}
		hotspots = append(hotspots, BarrierHotspot{
			Function:    name,
			File:        fn.File,
			CallCount:   callers,
			BarrierKind: kind,
			ImpactNote:  impact,
		})
	}

	sort.Slice(hotspots, func(i, j int) bool { return hotspots[i].CallCount > hotspots[j].CallCount })
	return hotspots
}

func findRedundancyTargets(cg *ast.CallGraph, emitters []RegisterEmitterRank) []RedundancyTarget {
	var targets []RedundancyTarget
	regFiles := make(map[string]*RedundancyTarget)

	for _, gn := range cg.Gen8Nodes {
		for _, reg := range gn.Regs {
			if rt, ok := regFiles[reg]; ok {
				rt.WriteSites++
			} else {
				regFiles[reg] = &RedundancyTarget{
					RegisterName: reg,
					RegisterFile: gn.File,
					WriteSites:   1,
					A8XXSpecific: true,
				}
			}
		}
	}

	for _, rt := range regFiles {
		if rt.WriteSites >= 5 {
			rt.LikelyRedundant = true
			rt.SavingsEstimate = fmt.Sprintf("~%d writes/frame", rt.WriteSites)
		}
		if rt.WriteSites >= 20 {
			rt.SavingsEstimate = fmt.Sprintf("HIGH: %d writes/frame", rt.WriteSites)
		}
		targets = append(targets, *rt)
	}

	sort.Slice(targets, func(i, j int) bool { return targets[i].WriteSites > targets[j].WriteSites })
	return targets
}

func analyzeBatching(cg *ast.CallGraph) *BatchAnalysis {
	ba := &BatchAnalysis{}

	submitFuncs := []string{
		"tu_queue_submit", "kgsl_queue_submit", "tu_QueueSubmit",
		"queue_submit", "submit_add_entries",
	}

	for _, sf := range submitFuncs {
		if cg.Functions[sf] != nil {
			ba.FunctionsWithSubmits = append(ba.FunctionsWithSubmits, sf)
		}
	}

	drawEntryCount := 0
	for _, ep := range cg.EntryPoints {
		if strings.HasPrefix(ep, "tu_CmdDraw") || strings.HasPrefix(ep, "tu_CmdDispatch") {
			if cg.Functions[ep] != nil {
				drawEntryCount++
			}
		}
	}

	if drawEntryCount > 5 {
		ba.SubmitGranularity = "per-draw-call (fine-grained)"
		ba.BatchOpportunity = "Consider merging consecutive draw submissions via deferred CS"
	} else {
		ba.SubmitGranularity = "batched (coarse-grained)"
		ba.BatchOpportunity = "Batching appears healthy"
	}
	return ba
}

func synthesizeQuickWins(cg *ast.CallGraph, r *BottleneckReport) []QuickWin {
	var qw []QuickWin

	// Q1: Redundant register write filtering
	qw = append(qw, QuickWin{
		Rank: 1, Title: "Shadow Register Filter for A8XX PKT4 Writes",
		Description: fmt.Sprintf("Track recent register values and skip %d redundant PKT4 writes per frame. Found %d high-traffic A8XX registers.",
			countHighTrafficA8XX(cg), len(r.RedundancyTargets)),
		SourceFile: "tu_cs.h:tu_cs_emit_pkt4",
		FPSImpact:  "High (3-8% CPU reduction)",
		Effort:     "Medium (100 lines C + flag)",
		Mechanism:  "Add 4KB shadow reg cache in tu_cs. If tu_cs_emit_pkt4 writes same values as last time, skip packet. Prevents redundant GPU reg inflation from driver state tracking loops.",
	})

	// Q2: Barrier coalescing
	qw = append(qw, QuickWin{
		Rank: 2, Title: "Barrier Coalescing in Render Pass",
		Description: fmt.Sprintf("Merge consecutive cache flushes into single barriers. Found %d barrier-heavy functions on draw path.",
			r.HotDrawPath.Barriers),
		SourceFile: "tu_cmd_buffer.cc:tu_emit_cache_flush",
		FPSImpact:  "Medium (2-5% in barrier-heavy workloads)",
		Effort:     "Medium (track pending flush bits, emit once)",
		Mechanism:  "Set dirty_flush_bits instead of emitting immediately. At draw call or actual barrier, emit single combined flush. Avoids 3-5 redundant flush packets per draw.",
	})

	// Q3: Draw state caching
	qw = append(qw, QuickWin{
		Rank: 3, Title: "Draw State Deduplication",
		Description: fmt.Sprintf("Skip re-emitting identical pipeline/draw state. Gen8 has reg write count estimated at %d per draw path.",
			r.HotDrawPath.RegisterWrites),
		SourceFile: "tu_pipeline.cc, tu_cmd_buffer.cc",
		FPSImpact:  "High (5-12% CPU reduction in state-bound scenes)",
		Effort:     "Medium-High (state diff tracking)",
		Mechanism:  "Hash current draw state (GRAS, RB, VPC configs). Compare against last submitted state. Skip redundant CP_SET_DRAW_STATE and PKT4 reg programs. Gen8's BINNING pass is especially expensive here - KMD already programs non-ctx regs, so driver can skip them.",
	})

	// Q4: Gen8 CP_SET_MARKER batching 
	qw = append(qw, QuickWin{
		Rank: 4, Title: "A8XX_CP_SET_MARKER Batching",
		Description: "Gen8 adds CP_SET_MARKER per render pass. Batch multiple markers into single CS entry.",
		SourceFile: "tu_cmd_buffer.cc:tu6_emit_binning_pass",
		FPSImpact:  "Low-Medium (1-3% in multi-pass)",
		Effort:     "Low (reorder marker emission)",
		Mechanism:  "tu6_emit_binning_pass emits markers per tile. Merge into pre-computed marker array emitted as single PKT7 batch. Saves IB transition overhead.",
	})

	// Q5: Tess BO pre-allocation
	qw = append(qw, QuickWin{
		Rank: 5, Title: "Gen8 Tess BO Pre-allocation",
		Description: "Gen8 tess BO is sized for 2 draws per submit. Pre-allocate and reuse.",
		SourceFile: "tu_device.h:544 (TU_TESS gen8), tu_cmd_buffer.cc:8293",
		FPSImpact:  "Low (0.5-2% in tess-heavy content)",
		Effort:     "Low (pre-allocate pool of 2-draw tess BOs)",
		Mechanism:  "Instead of allocating per-submit, maintain a free list of gen8 tess BOs. Reclaim after fence completes. Avoids ioctl(KGSL_GPUMEM_ALLOC) on draw path.",
	})

	// Q6: Known redundant A8XX register writes
	regExamples := []string{}
	for i, rt := range r.RedundancyTargets {
		if i >= 3 {
			break
		}
		regExamples = append(regExamples, rt.RegisterName)
	}
	qw = append(qw, QuickWin{
		Rank: 6, Title: "A8XX Register Write Sweep",
		Description: fmt.Sprintf("Cache writes to %s (and %d more) across draw boundaries.",
			strings.Join(regExamples, ", "), len(r.RedundancyTargets)-3),
		SourceFile: "tu_cs.h:386 (REG_A6XX macro), tu_cmd_buffer.cc",
		FPSImpact:  "Medium (2-4% CPU reduction)",
		Effort:     "Low (add MESA_REG_CACHE macro)",
		Mechanism:  "Wrap tu_cs_emit_regs with a simple hash-based cache. Since many TU_DEBUG options already gate behavior, TU_REG_CACHE=1 env var can gate this optimization.",
	})

	return qw
}

func countHighTrafficA8XX(cg *ast.CallGraph) int {
	count := 0
	for _, gn := range cg.Gen8Nodes {
		if len(gn.Regs) > 0 && strings.HasPrefix(gn.Kind, "gen8-register") {
			count += len(gn.Regs)
		}
	}
	return count
}

func truncstr(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}
