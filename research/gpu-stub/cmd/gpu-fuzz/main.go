package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strings"

	"github.com/idans/winlator-cmod-builder/research/internal/ast"
	"github.com/idans/winlator-cmod-builder/research/internal/pm4"
	"github.com/idans/winlator-cmod-builder/research/internal/stub"
)

type FuzzConfig struct {
	NumRounds      int    `json:"num_rounds"`
	MaxMutations   int    `json:"max_mutations"`
	Seed           int64  `json:"seed"`
	TargetFunction string `json:"target_function"`
}

type FuzzCase struct {
	ID          int           `json:"id"`
	Name        string        `json:"name"`
	Mutations   int           `json:"mutations"`
	Dwords      []uint32      `json:"dwords"`
	SourceFiles []string      `json:"source_files"`
}

type FuzzResult struct {
	Case       FuzzCase              `json:"case"`
	Status     string                `json:"status"`
	EngineStats *stub.SubmitBatchStats `json:"engine_stats,omitempty"`
	Findings   []Finding             `json:"findings,omitempty"`
	CrashInfo  *CrashInfo            `json:"crash_info,omitempty"`
}

type Finding struct {
	Severity   string `json:"severity"`
	Category   string `json:"category"`
	Message    string `json:"message"`
	SourceFile string `json:"source_file"`
	SourceLine int    `json:"source_line"`
	FixSuggestion string `json:"fix_suggestion,omitempty"`
}

type CrashInfo struct {
	Signal    string `json:"signal"`
	Address   string `json:"address,omitempty"`
	Backtrace string `json:"backtrace,omitempty"`
}

type FuzzSession struct {
	Config           FuzzConfig             `json:"config"`
	TotalCases       int                    `json:"total_cases"`
	Crashes          int                    `json:"crashes"`
	Warnings         int                    `json:"warnings"`
	Anomalies        int                    `json:"anomalies"`
	Results          []FuzzResult           `json:"results"`
	ProblematicPaths []ProblematicPath      `json:"problematic_paths"`
}

type ProblematicPath struct {
	PathName     string   `json:"path_name"`
	Findings     []Finding `json:"findings"`
	AffectedFuncs []string `json:"affected_funcs"`
	Severity     string   `json:"severity"`
}

type Fuzzer struct {
	rng      *rand.Rand
	callGraph *ast.CallGraph
	engine   *stub.GPUEngine
	config   FuzzConfig
	session  *FuzzSession
}

func NewFuzzer(cg *ast.CallGraph, cfg FuzzConfig, engine *stub.GPUEngine) *Fuzzer {
	src := rand.NewSource(cfg.Seed)
	return &Fuzzer{
		rng:       rand.New(src),
		callGraph: cg,
		engine:    engine,
		config:    cfg,
		session:   &FuzzSession{Config: cfg},
	}
}

func (f *Fuzzer) Run() *FuzzSession {
	f.fuzzRegisterValueMutations()
	f.fuzzBarrierPlacement()
	f.fuzzDrawOrdering()
	f.fuzzDescriptorConfigs()
	f.fuzzBinningPassConfig()
	f.identifyProblematicPaths()
	return f.session
}

func (f *Fuzzer) fuzzRegisterValueMutations() {
	// Collect all A8XX register write sites from neuron analysis
	var regSites []*ast.Gen8Node
	for _, gn := range f.callGraph.Gen8Nodes {
		if len(gn.Regs) > 0 && strings.HasPrefix(gn.Kind, "gen8-register") {
			regSites = append(regSites, gn)
		}
	}

	for round := 0; round < f.config.NumRounds && round < 20; round++ {
		fc := FuzzCase{
			ID:        f.session.TotalCases,
			Name:      fmt.Sprintf("reg_mutation_%d", round),
			Mutations: f.rng.Intn(f.config.MaxMutations) + 1,
		}

		// Build a realistic baseline submission with state + draw + barrier
		base := f.buildBaselineSubmission()

		// Apply register value mutations
		for m := 0; m < fc.Mutations; m++ {
			site := regSites[f.rng.Intn(len(regSites))]
			fc.SourceFiles = append(fc.SourceFiles, fmt.Sprintf("%s:%d", site.File, site.Line))

			// Mutate a register write in the baseline
			reg := uint16(f.rng.Intn(0x4000))
			val := f.rng.Uint32() // random value

			// Insert or replace a PKT4 write
			hdr := f.buildPkt4(reg, 1)
			if f.rng.Intn(100) < 30 {
				base = append(base[:len(base)/2], append([]uint32{hdr, val}, base[len(base)/2:]...)...)
			} else {
				base = append(base, hdr, val)
			}
		}
		fc.Dwords = base

		result := f.evaluate(fc)
		f.session.TotalCases++
		if result.Status != "pass" {
			f.session.Crashes++
		}
		f.session.Results = append(f.session.Results, result)
	}
}

func (f *Fuzzer) fuzzBarrierPlacement() {
	for round := 0; round < f.config.NumRounds && round < 15; round++ {
		fc := FuzzCase{
			ID:        f.session.TotalCases,
			Name:      fmt.Sprintf("barrier_fuzz_%d", round),
			Mutations: f.rng.Intn(f.config.MaxMutations) + 1,
		}

		base := f.buildBaselineSubmission()
		barrierOps := []uint32{
			pm4.CP_WAIT_FOR_IDLE,
			pm4.CP_WAIT_FOR_ME,
			pm4.CP_WAIT_MEM_WRITES,
			pm4.CP_BARRIER,
			pm4.CP_INVALIDATE_STATE,
		}

		for m := 0; m < fc.Mutations; m++ {
			op := barrierOps[f.rng.Intn(len(barrierOps))]
			hdr := f.buildPkt7(op, 0)
			insertPos := f.rng.Intn(len(base))
			base = append(base[:insertPos], append([]uint32{hdr}, base[insertPos:]...)...)
		}
		fc.Dwords = base

		result := f.evaluate(fc)
		f.session.TotalCases++
		f.session.Results = append(f.session.Results, result)

		// Check for excessive barriers
		if result.EngineStats != nil && result.EngineStats.Barriers > result.EngineStats.Draws*3 {
			result.Findings = append(result.Findings, Finding{
				Severity:   "warning",
				Category:   "barrier_abuse",
				Message:    fmt.Sprintf("%d barriers for %d draws — potential CPU waste", result.EngineStats.Barriers, result.EngineStats.Draws),
				SourceFile: "tu_cmd_buffer.cc",
				FixSuggestion: "Coalesce adjacent cache flushes into single barrier",
			})
			f.session.Warnings++
		}
	}
}

func (f *Fuzzer) fuzzDrawOrdering() {
	for round := 0; round < f.config.NumRounds && round < 10; round++ {
		fc := FuzzCase{
			ID:        f.session.TotalCases,
			Name:      fmt.Sprintf("draw_order_%d", round),
			Mutations: f.rng.Intn(f.config.MaxMutations) + 1,
		}

		base := f.buildBaselineSubmission()
		drawOps := []uint32{
			pm4.CP_DRAW_INDX,
			pm4.CP_DRAW_INDX_BIN,
			pm4.CP_DRAW_INDIRECT,
			pm4.CP_DRAW_AUTO,
			pm4.CP_EXEC_CS,
		}

		for m := 0; m < fc.Mutations; m++ {
			op := drawOps[f.rng.Intn(len(drawOps))]
			payloadSize := 6
			if op == pm4.CP_EXEC_CS {
				payloadSize = 4
			}
			hdr := f.buildPkt7(op, payloadSize)
			drawPkt := []uint32{hdr}
			for i := 0; i < payloadSize; i++ {
				drawPkt = append(drawPkt, f.rng.Uint32())
			}
			insertPos := f.rng.Intn(len(base))
			base = append(base[:insertPos], append(drawPkt, base[insertPos:]...)...)
		}
		fc.Dwords = base

		result := f.evaluate(fc)
		f.session.TotalCases++
		f.session.Results = append(f.session.Results, result)

		// Draws without barriers → potential GPU hang
		if result.EngineStats != nil && result.EngineStats.Draws > 5 && result.EngineStats.Barriers == 0 {
			result.Findings = append(result.Findings, Finding{
				Severity:   "info",
				Category:   "missing_barriers",
				Message:    fmt.Sprintf("%d draws with 0 barriers — may cause GPU stall on real HW", result.EngineStats.Draws),
				SourceFile: "tu_cmd_buffer.cc",
			})
		}
	}
}

func (f *Fuzzer) fuzzDescriptorConfigs() {
	for round := 0; round < f.config.NumRounds && round < 10; round++ {
		fc := FuzzCase{
			ID:        f.session.TotalCases,
			Name:      fmt.Sprintf("desc_mutate_%d", round),
			Mutations: f.rng.Intn(f.config.MaxMutations) + 1,
		}

		base := f.buildBaselineSubmission()

		// Mutate A8XX TEX_SAMP descriptor values
		sampRegs := []uint16{0, 1, 2, 3}
		for m := 0; m < fc.Mutations; m++ {
			reg := sampRegs[f.rng.Intn(len(sampRegs))]
			// Edge-case values
			val := f.rng.Uint32()
			switch f.rng.Intn(8) {
			case 0:
				val = 0 // clear
			case 1:
				val = 0xFFFFFFFF // max
			case 2:
				val = 0x80000000 // sign bit
			case 3:
				val = 0x0000FFFF // partial
			}
			hdr := f.buildPkt4(reg, 1)
			base = append(base, hdr, val)
		}
		fc.Dwords = base

		result := f.evaluate(fc)
		f.session.TotalCases++
		f.session.Results = append(f.session.Results, result)
	}
}

func (f *Fuzzer) fuzzBinningPassConfig() {
	for round := 0; round < f.config.NumRounds && round < 8; round++ {
		fc := FuzzCase{
			ID:        f.session.TotalCases,
			Name:      fmt.Sprintf("binning_fuzz_%d", round),
			Mutations: f.rng.Intn(f.config.MaxMutations) + 1,
		}

		base := f.buildBaselineSubmission()

		// Fuzz CP_SET_MARKER (gen8 specific)
		for m := 0; m < fc.Mutations; m++ {
			hdr := f.buildPkt7(pm4.CP_SET_MARKER, 2)
			pkt := []uint32{hdr, f.rng.Uint32(), f.rng.Uint32()}
			insertPos := f.rng.Intn(len(base))
			base = append(base[:insertPos], append(pkt, base[insertPos:]...)...)
		}
		fc.Dwords = base

		result := f.evaluate(fc)
		f.session.TotalCases++
		f.session.Results = append(f.session.Results, result)
	}
}

func (f *Fuzzer) buildBaselineSubmission() []uint32 {
	var dwords []uint32

	// State setup PKT4
	for i := 0; i < 10; i++ {
		reg := uint16(0x2000 + i)
		hdr := f.buildPkt4(reg, 1)
		dwords = append(dwords, hdr, 0x55555555)
	}

	// A8XX sampler descriptors
	for _, reg := range []uint16{0, 1, 2, 3} {
		hdr := f.buildPkt4(reg, 1)
		dwords = append(dwords, hdr, 0x00000000)
	}

	// CP_SET_MARKER (gen8)
	dwords = append(dwords, f.buildPkt7(pm4.CP_SET_MARKER, 1), 0)

	// WFI barrier
	dwords = append(dwords, f.buildPkt7(pm4.CP_WAIT_FOR_IDLE, 0))

	// Draw
	dwords = append(dwords, f.buildPkt7(pm4.CP_DRAW_INDX, 6))
	for i := 0; i < 6; i++ {
		dwords = append(dwords, uint32(i*100))
	}

	// Cache flush
	dwords = append(dwords, f.buildPkt7(pm4.CP_EVENT_WRITE, 4), 0x1f, 0, 0, 0)

	return dwords
}

func (f *Fuzzer) evaluate(fc FuzzCase) FuzzResult {
	result := FuzzResult{
		Case:   fc,
		Status: "pass",
	}

	entry := &pm4.SubmissionEntry{
		Name:   fc.Name,
		Dwords: fc.Dwords,
	}
	packets, decodeStats := pm4.Decode(fc.Dwords)
	entry.Packets = packets

	result.EngineStats = &stub.SubmitBatchStats{
		SubmitID:      uint64(fc.ID),
		Entries:       1,
		TotalDwords:   len(fc.Dwords),
		PKT4Packets:   decodeStats.PKT4Count,
		PKT7Packets:   decodeStats.PKT7Count,
		RegWrites:     decodeStats.RegWrites,
		A8XXRegWrites: decodeStats.A8XXRegWrites,
		Barriers:      decodeStats.BarrierPackets,
		Draws:         decodeStats.DrawPackets,
	}

	// Run through engine
	submitResult := f.engine.ProcessSubmit([]*pm4.SubmissionEntry{entry})
	_ = submitResult

	// --- Detection Rules ---

	// CRITICAL: Unknown opcodes → potential crash on real GPU
	for opcode := range decodeStats.UnknownOpcodes {
		result.Findings = append(result.Findings, Finding{
			Severity:   "critical",
			Category:   "unknown_opcode",
			Message:    fmt.Sprintf("Unknown PM4 opcode 0x%02x — would cause GPU hang on real HW", opcode),
			SourceFile: fc.Name,
		})
		result.Status = "crash"
	}

	// High register write density without draws
	if decodeStats.RegWrites > 100 && decodeStats.DrawPackets == 0 {
		result.Findings = append(result.Findings, Finding{
			Severity:   "warning",
			Category:   "wasted_reg_writes",
			Message:    fmt.Sprintf("%d register writes with 0 draws — wasted CPU time", decodeStats.RegWrites),
			SourceFile: "tu_cs.h",
			FixSuggestion: "Batch register writes or skip if no draw follows",
		})
		f.session.Warnings++
	}

	// Redundant check from engine stats
	engStats := f.engine.Stats()
	if engStats.RedundantWritesDropped > 50 {
		result.Findings = append(result.Findings, Finding{
			Severity:   "info",
			Category:   "redundant_writes",
			Message:    fmt.Sprintf("%d redundant register writes detected", engStats.RedundantWritesDropped),
			SourceFile: "tu_cs.h",
			FixSuggestion: "Add shadow register cache in tu_cs_emit_pkt4",
		})
		f.session.Anomalies++
	}

	// Barrier after every draw → excessive
	if decodeStats.DrawPackets > 0 && decodeStats.BarrierPackets >= decodeStats.DrawPackets {
		result.Findings = append(result.Findings, Finding{
			Severity:   "warning",
			Category:   "barrier_per_draw",
			Message:    fmt.Sprintf("Barrier emitted for every draw (%d draws, %d barriers) — coalesce", decodeStats.DrawPackets, decodeStats.BarrierPackets),
			SourceFile: "tu_cmd_buffer.cc:tu_emit_cache_flush",
			FixSuggestion: "Defer flushes, merge before submit",
		})
		f.session.Warnings++
	}

	// PKT7 count > PKT4 — mostly command overhead, not state
	if decodeStats.PKT7Count > decodeStats.PKT4Count*2 {
		result.Findings = append(result.Findings, Finding{
			Severity:   "info",
			Category:   "command_overhead",
			Message:    fmt.Sprintf("PKT7:%d dominates PKT4:%d — high command overhead vs state programming", decodeStats.PKT7Count, decodeStats.PKT4Count),
			SourceFile: "tu_cmd_buffer.cc",
		})
	}

	// Track source files from neuron data
	for _, siteFile := range fc.SourceFiles {
		parts := strings.SplitN(siteFile, ":", 2)
		if len(parts) == 2 {
			file, lineStr := parts[0], parts[1]
			line := 0
			fmt.Sscanf(lineStr, "%d", &line)
			if fn := f.callGraph.Functions[file]; fn != nil && fn.Gen8Site {
				result.Findings = append(result.Findings, Finding{
					Severity:   "info",
					Category:   "gen8_path_exercised",
					Message:    fmt.Sprintf("Fuzzed gen8 site in %s", file),
					SourceFile: fn.File,
					SourceLine: fn.Line,
				})
			}
		}
	}

	return result
}

func (f *Fuzzer) identifyProblematicPaths() {
	// Group findings by category
	categories := make(map[string][]Finding)
	for _, result := range f.session.Results {
		for _, finding := range result.Findings {
			categories[finding.Category] = append(categories[finding.Category], finding)
		}
	}

	for cat, findings := range categories {
		severity := "info"
		for _, fi := range findings {
			if fi.Severity == "critical" {
				severity = "critical"
				break
			}
			if fi.Severity == "warning" && severity == "info" {
				severity = "warning"
			}
		}

		path := ProblematicPath{
			PathName: cat,
			Findings: findings[:min(3, len(findings))],
			Severity: severity,
		}

		// Link to affected functions from call graph
		for name, fn := range f.callGraph.Functions {
			for _, fi := range findings {
				if strings.Contains(fn.File, fi.SourceFile) || fn.Gen8Site {
					path.AffectedFuncs = append(path.AffectedFuncs, name)
					break
				}
			}
		}
		f.session.ProblematicPaths = append(f.session.ProblematicPaths, path)
	}
}

func (f *Fuzzer) buildPkt4(reg uint16, cnt int) uint32 {
	count := uint32(cnt)
	hdr := pm4.Type4Mask | count | (f.oddParity(count) << 7) |
		((uint32(reg) & 0x3ffff) << 8) |
		(f.oddParity(uint32(reg)) << 27)
	return hdr
}

func (f *Fuzzer) buildPkt7(opcode uint32, cnt int) uint32 {
	count := uint32(cnt)
	hdr := pm4.Type7Mask | count |
		(f.oddParity(count) << 15) |
		((opcode & 0x7f) << 16) |
		(f.oddParity(opcode) << 23)
	return hdr
}

func (f *Fuzzer) oddParity(val uint32) uint32 {
	val ^= val >> 16
	val ^= val >> 8
	val ^= val >> 4
	val &= 0xf
	return (uint32(0x9669) >> val) & 1
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func main() {
	analysisFile := flag.String("input", "output/analysis.json", "Neuron analysis JSON")
	seed := flag.Int64("seed", 123456, "RNG seed")
	rounds := flag.Int("rounds", 10, "Fuzz rounds per category")
	mutations := flag.Int("mutations", 50, "Max mutations per case")
	outFile := flag.String("out", "output/fuzz_report.json", "Output report JSON")
	flag.Parse()

	data, err := os.ReadFile(*analysisFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v — run ast-analyzer first\n", err)
		os.Exit(1)
	}

	var cg ast.CallGraph
	if err := json.Unmarshal(data, &cg); err != nil {
		fmt.Fprintf(os.Stderr, "JSON error: %v\n", err)
		os.Exit(1)
	}

	config := FuzzConfig{
		NumRounds:    *rounds,
		MaxMutations: *mutations,
		Seed:         *seed,
	}

	engine := stub.NewGPUEngine(stub.NewMemoryTracker(), stub.ExperimentConfig{
		RedundantWriteFilter: true,
		BatchAnalysis:        true,
	})

	fuzzer := NewFuzzer(&cg, config, engine)
	session := fuzzer.Run()

	sort.Slice(session.ProblematicPaths, func(i, j int) bool {
		order := map[string]int{"critical": 0, "warning": 1, "info": 2}
		return order[session.ProblematicPaths[i].Severity] < order[session.ProblematicPaths[j].Severity]
	})

	fmt.Println("╔══════════════════════════════════════════════════════════╗")
	fmt.Println("║            TURNIP GEN8 FUZZ REPORT                       ║")
	fmt.Println("╠══════════════════════════════════════════════════════════╣")
	fmt.Printf("║  Seed: %-10d  Rounds per category: %-3d              ║\n", *seed, *rounds)
	fmt.Printf("║  Total cases: %3d   Crashes: %2d   Warnings: %2d   Anomalies: %2d ║\n", session.TotalCases, session.Crashes, session.Warnings, session.Anomalies)
	fmt.Println("╠══════════════════════════════════════════════════════════╣")

	fmt.Println("║                                                          ║")
	fmt.Println("║  🔴 CRITICAL / ⚠ WARNING PATHS                           ║")
	fmt.Println("║                                                          ║")
	for _, pp := range session.ProblematicPaths {
		icon := "⚠"
		if pp.Severity == "critical" {
			icon = "🔴"
		}
		fmt.Printf("║  %s %-52s ║\n", icon, pp.PathName)
		for _, fi := range pp.Findings {
			fmt.Printf("║     %-52s ║\n", truncate(fi.Message, 52))
		}
		if len(pp.AffectedFuncs) > 0 {
			fmt.Printf("║     Affected: %-44s ║\n", truncate(strings.Join(pp.AffectedFuncs[:min(3, len(pp.AffectedFuncs))], ", "), 44))
		}
		fmt.Println("║                                                          ║")
	}
	fmt.Println("╚══════════════════════════════════════════════════════════╝")

	report, _ := json.MarshalIndent(session, "", "  ")
	os.WriteFile(*outFile, report, 0644)
	fmt.Printf("\nFull report: %s\n", *outFile)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
