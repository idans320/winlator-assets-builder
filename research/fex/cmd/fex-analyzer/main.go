package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/idans/winlator-cmod-builder/research/internal/fex"
)

func main() {
	root := os.Getenv("FEX_ROOT")
	if root == "" {
		root = filepath.Join(os.Getenv("HOME"), "winlator-cmod-builder", "fexcore", "workdir", "fex")
	}

	fmt.Printf("=== FEXCore JIT Analyzer ===\nFEX root: %s\n\n", root)

	irPath := filepath.Join(root, "FEXCore", "Source", "Interface", "IR", "IR.json")
	ops, err := fex.ParseIRJSON(irPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[IR] %d ops defined\n\n", len(ops))

	catStats := fex.IROpCategories(ops)
	sort.Slice(catStats, func(i, j int) bool { return catStats[i].Count > catStats[j].Count })
	for _, cs := range catStats {
		fmt.Printf("  %-20s %3d ops (dest=%d, sidefx=%d, no-jit=%d)\n",
			cs.Category, cs.Count, cs.WithDest, cs.WithSide, cs.NoDispatch)
	}

	fmt.Printf("\n=== JIT Emission Site Analysis ===\n\n")

	jitRoot := filepath.Join(root, "FEXCore", "Source", "Interface", "Core", "JIT")
	ceRoot := filepath.Join(root, "CodeEmitter", "CodeEmitter")

	allEmit := make(map[string]int)
	fileStats := analyzeEmitDir(jitRoot, allEmit)
	analyzeEmitDir(ceRoot, allEmit)

	sort.Slice(fileStats, func(i, j int) bool { return fileStats[i].opHandlers > fileStats[j].opHandlers })

	fmt.Printf("%-18s %6s %6s %6s\n", "File", "Lines", "Funcs", "Ops")
	fmt.Printf("%-18s %6s %6s %6s\n", "----", "-----", "-----", "---")
	for _, fs := range fileStats {
		fmt.Printf("%-18s %6d %6d %6d\n", fs.name, fs.lines, fs.funcs, fs.opHandlers)
	}

	type emitEntry struct {
		Name  string
		Count int
		Cat   string
	}
	var topEmits []emitEntry
	for name, count := range allEmit {
		cat := fex.CatALU
		switch {
		case strings.HasPrefix(name, "ld") || strings.HasPrefix(name, "st"):
			cat = fex.CatLoadStore
		case strings.HasPrefix(name, "b") || strings.HasPrefix(name, "bl"):
			cat = fex.CatBranch
		case strings.HasPrefix(name, "f"):
			cat = fex.CatScalar
		}
		topEmits = append(topEmits, emitEntry{name, count, cat.String()})
	}
	sort.Slice(topEmits, func(i, j int) bool { return topEmits[i].Count > topEmits[j].Count })

	fmt.Printf("\nTop 20 ARM64 emission calls:\n")
	fmt.Printf("%-14s %6s %10s\n", "Instruction", "Count", "Category")
	for i := range getLimit(len(topEmits), 20) {
		e := topEmits[i]
		fmt.Printf("%-14s %6d %10s\n", e.Name, e.Count, e.Cat)
	}

	fmt.Printf("\n=== Known Hot Paths ===\n\n")
	for _, hp := range fex.KnownHotPaths {
		fmt.Printf("  %2d. [%s] %-30s %s\n", hp.Rank, hp.Cat, hp.Name, hp.Description)
	}

	fmt.Printf("\n=== Caching Opportunities ===\n\n")
	opps := []struct {
		Rank int
		Name string
		Desc string
	}{
		{1, "LoadConstant cache", "reuse movz/movk/literal-pool across ops"},
		{2, "SpillStaticRegs hoist", "save/restore once per block, not per call"},
		{3, "TSO barrier coalesce", "merge adjacent dmb/stlr into single fence"},
		{4, "Address calc reuse", "cache base+offset across adjacent mem ops"},
		{5, "Vector broadcast cache", "reuse dup() when same constant is used"},
		{6, "Flag calculation memo", "memoize PSR flag derivations"},
		{7, "Constant pool dedup", "merge identical 64-bit literals"},
		{8, "Prolog/epilog merge", "hoist spill/fill out of inner loops"},
	}
	for _, o := range opps {
		fmt.Printf("  %d. %-30s %s\n", o.Rank, o.Name, o.Desc)
	}

	totalEmits := 0
	for _, v := range allEmit {
		totalEmits += v
	}
	out := map[string]interface{}{
		"ir_ops":       len(ops),
		"jit_files":    len(fileStats),
		"total_emits":  totalEmits,
		"unique_insn":  len(topEmits),
		"top_insn_top5": topEmits[:getLimit(len(topEmits), 5)],
	}
	_ = os.WriteFile(filepath.Join(os.Getenv("HOME"), "winlator-cmod-builder", "research", "fex", "analysis.json"), mustJSON(out), 0644)
	fmt.Printf("\n[JSON] Written to research/fex/analysis.json\n")
}

type fstat struct {
	name       string
	lines      int
	funcs      int
	opHandlers int
}

func analyzeEmitDir(dir string, allEmit map[string]int) []fstat {
	entries, _ := os.ReadDir(dir)
	var stats []fstat
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		fn := e.Name()
		if !strings.HasSuffix(fn, ".cpp") && !strings.HasSuffix(fn, ".inl") {
			continue
		}
		path := filepath.Join(dir, fn)
		data, _ := os.ReadFile(path)
		lines := strings.Split(string(data), "\n")
		funcs := fex.ScanFuncs(lines)

		opCount := 0
		for _, f := range funcs {
			if f.IsOpHandler {
				opCount++
			}
		}

		for _, c := range fex.CountEmissions(lines) {
			allEmit[c.Name] += c.Count
		}

		stats = append(stats, fstat{fn, len(lines), len(funcs), opCount})
	}
	return stats
}

func getLimit(avail, want int) int {
	if avail < want {
		return avail
	}
	return want
}

func mustJSON(v interface{}) []byte {
	b, _ := json.MarshalIndent(v, "", "  ")
	return b
}
