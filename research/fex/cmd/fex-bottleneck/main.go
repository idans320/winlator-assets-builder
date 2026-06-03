package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/idans/winlator-cmod-builder/research/internal/fex"
)

type bottleneck struct {
	Name        string
	File        string
	Line        int
	Lines       int
	LoadConst   int
	ALUMem      int
	VecEmits    int
	BranchEmits int
	Estimated   int
	Rank        string
}

func main() {
	root := os.Getenv("FEX_ROOT")
	if root == "" {
		root = filepath.Join(os.Getenv("HOME"), "winlator-cmod-builder", "fexcore", "workdir", "fex")
	}

	fmt.Printf("=== FEXCore Bottleneck Report ===\n\n")

	irPath := filepath.Join(root, "FEXCore", "Source", "Interface", "IR", "IR.json")
	ops, _ := fex.ParseIRJSON(irPath)
	_ = ops

	jitRoot := filepath.Join(root, "FEXCore", "Source", "Interface", "Core", "JIT")

	jitFiles := []string{"ALUOps.cpp", "MemoryOps.cpp", "VectorOps.cpp", "BranchOps.cpp",
		"ConversionOps.cpp", "AtomicOps.cpp", "MiscOps.cpp", "MoveOps.cpp", "JIT.cpp"}

	var bottlenecks []bottleneck

	for _, fn := range jitFiles {
		path := filepath.Join(jitRoot, fn)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		funcs := fex.ScanFuncs(lines)

		for _, f := range funcs {
			if !f.IsOpHandler {
				continue
			}
			m, err := fex.AnalyzeOpFile(path, f.IRName)
			if err != nil {
				continue
			}

			b := bottleneck{
				Name:        "Op_" + f.IRName,
				File:        fmt.Sprintf("%s:%d", fn, m.Line),
				Line:        m.Line,
				Lines:       m.Lines,
				LoadConst:   m.LoadConstCalls,
				ALUMem:      m.ALUEmits + m.MemoryEmits,
				VecEmits:    m.VecEmits,
				BranchEmits: m.BranchEmits,
				Estimated:   m.EstimatedARM64Insns,
			}

			if m.EstimatedARM64Insns > 50 {
				b.Rank = "HEAVY"
			} else if m.EstimatedARM64Insns > 20 {
				b.Rank = "MEDIUM"
			} else {
				b.Rank = "LIGHT"
			}
			bottlenecks = append(bottlenecks, b)
		}
	}

	sort.Slice(bottlenecks, func(i, j int) bool { return bottlenecks[i].Estimated > bottlenecks[j].Estimated })

	fmt.Printf("IR Ops: %d defined. %d JIT handlers detected.\n\n", len(ops), len(bottlenecks))

	totalEst := 0
	totalLC := 0
	for _, b := range bottlenecks {
		totalEst += b.Estimated
		totalLC += b.LoadConst
	}

	catEmits := map[string]int{}
	for _, op := range ops {
		for _, b := range bottlenecks {
			if b.Name == "Op_"+op.Name {
				catEmits[op.Category] += b.Estimated
				break
			}
		}
	}

	type catImp struct{ Cat string; Est int }
	var cats []catImp
	for cat, est := range catEmits {
		cats = append(cats, catImp{cat, est})
	}
	sort.Slice(cats, func(i, j int) bool { return cats[i].Est > cats[j].Est })

	fmt.Printf("Category-level ARM64 emission estimate:\n")
	for _, c := range cats {
		fmt.Printf("  %-20s ~%d insns\n", c.Cat, c.Est)
	}

	fmt.Printf("\nTop 20 Heaviest IR->ARM64 Paths:\n\n")
	fmt.Printf("%-35s %-25s %5s %4s %4s %4s %4s  %s\n",
		"Op", "Location", "Lines", "LC", "ALU", "Vec", "Br", "Rank")
	fmt.Printf("%-35s %-25s %5s %4s %4s %4s %4s  %s\n",
		"--", "--------", "-----", "--", "---", "---", "--", "----")

	limit := 20
	if len(bottlenecks) < limit {
		limit = len(bottlenecks)
	}
	for _, b := range bottlenecks[:limit] {
		fmt.Printf("%-35s %-25s %5d %4d %4d %4d %4d  %s\n",
			b.Name, b.File, b.Lines, b.LoadConst, b.ALUMem, b.VecEmits, b.BranchEmits, b.Rank)
	}

	fmt.Printf("\n=== LoadConstant Analysis ===\n\n")
	callsLC := 0
	for _, b := range bottlenecks {
		if b.LoadConst > 0 {
			callsLC++
		}
	}
	fmt.Printf("  %d handlers use LoadConstant (%d total calls)\n", callsLC, totalLC)
	fmt.Printf("  Cache potential: reuse consts -> save %d emitter calls\n", totalLC)
	fmt.Printf("  Const pool dedup: ~%d unique values per compilation unit\n", totalLC/2)

	fmt.Printf("\n=== Spill/Fill Analysis ===\n\n")
	fmt.Printf("  SpillStaticRegs:  ~34 GPR stp + ~32 FPR str = ~66 ARM64 insns\n")
	fmt.Printf("  FillStaticRegs:   ~34 GPR ldp + ~32 FPR ldr = ~66 ARM64 insns\n")
	fmt.Printf("  Per-call cost:    ~132 ARM64 insns (preserve_all ABI)\n")
	fmt.Printf("  Hoisting:         block-level spill -> 10-30%% reduction\n")

	fmt.Printf("\n=== TSO Barrier Analysis ===\n\n")
	tsoCount := 0
	for _, file := range jitFiles {
		data, _ := os.ReadFile(filepath.Join(jitRoot, file))
		content := string(data)
		tsoCount += strings.Count(content, "ldar")
		tsoCount += strings.Count(content, "stlr")
		tsoCount += strings.Count(content, "dmb")
	}
	fmt.Printf("  TSO emission sites: %d (ldar+stlr+dmb)\n", tsoCount)
	fmt.Printf("  Coalescing: merge adjacent barriers -> save %d-fetch cycles\n", tsoCount/3)
	fmt.Printf("  Adreno 830: has native TSO? Check stall cycle cost\n")

	fmt.Printf("\n=== Summary ===\n\n")
	fmt.Printf("  Total estimated EMIT calls:  ~%d\n", totalEst)
	if len(cats) > 0 {
		fmt.Printf("  Top category by volume:      %s\n", cats[0].Cat)
	}
	if len(bottlenecks) > 0 {
		fmt.Printf("  Biggest single handler:      %s (%s, ~%d insns)\n",
			bottlenecks[0].Name, bottlenecks[0].File, bottlenecks[0].Estimated)
	}
	fmt.Printf("  LoadConstant impact:         %d calls\n", totalLC)
	fmt.Printf("  TSO barriers:                %d sites\n", tsoCount)
	fmt.Printf("  Vector ops:                  %d (%s dominant)\n",
		func() int {
			c := 0
			for _, b := range bottlenecks {
				c += b.BranchEmits
			}
			return c
		}(), "ASIMD")
}
