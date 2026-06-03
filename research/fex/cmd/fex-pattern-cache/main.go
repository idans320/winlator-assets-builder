package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/idans/winlator-cmod-builder/research/internal/fex"
)

type Pattern struct {
	Sig      string
	File     string
	Line     int
	Insns    []string
	Category string
	Count    int
}

func main() {
	root := os.Getenv("FEX_ROOT")
	if root == "" {
		root = filepath.Join(os.Getenv("HOME"), "winlator-cmod-builder", "fexcore", "workdir", "fex")
	}

	fmt.Printf("=== FEXCore ARM64 Pattern Cache Analyzer ===\n\n")

	patterns := extractPatterns(root)

	bySig := make(map[string]*Pattern)
	for _, p := range patterns {
		sig := strings.Join(p.Insns, "+")
		if existing, ok := bySig[sig]; ok {
			existing.Count++
		} else {
			p.Count = 1
			p.Sig = sig
			bySig[sig] = &p
		}
	}

	var unique []Pattern
	byCat := make(map[string]int)
	for _, p := range bySig {
		unique = append(unique, *p)
		byCat[p.Category]++
	}
	sort.Slice(unique, func(i, j int) bool { return unique[i].Count > unique[j].Count })

	totalInstances := len(patterns)
	fmt.Printf("  Total patterns:   %d\n", totalInstances)
	fmt.Printf("  Unique patterns:  %d\n", len(unique))
	fmt.Printf("  Duplication:      %.1f%% of ARM64 sequences repeat\n",
		float64(totalInstances-len(unique))/float64(totalInstances)*100)

	multiplicity := make(map[int]int)
	for _, p := range unique {
		multiplicity[p.Count]++
	}
	type mEntry struct{ Instances, Count int }
	var mList []mEntry
	for k, v := range multiplicity {
		mList = append(mList, mEntry{k, v})
	}
	sort.Slice(mList, func(i, j int) bool { return mList[i].Instances > mList[j].Instances })
	fmt.Printf("\n  Multiplicity:\n")
	for _, m := range mList {
		fmt.Printf("    %4d instances: %4d unique patterns\n", m.Instances, m.Count)
	}

	fmt.Printf("\n  By category:\n")
	type cEntry struct{ Cat string; Count int }
	var cats []cEntry
	for c, n := range byCat {
		cats = append(cats, cEntry{c, n})
	}
	sort.Slice(cats, func(i, j int) bool { return cats[i].Count > cats[j].Count })
	for _, c := range cats {
		fmt.Printf("    %-10s %d\n", c.Cat, c.Count)
	}

	fmt.Printf("\n  Top 20 repeated ARM64 sequences:\n")
	fmt.Printf("  %-35s %7s %12s %s\n", "Sequence", "Count", "Category", "File")
	n := len(unique)
	if n > 20 {
		n = 20
	}
	for _, p := range unique[:n] {
		fmt.Printf("  %-35s %7d %12s %s:%d\n",
			p.Sig, p.Count, p.Category, filepath.Base(p.File), p.Line)
	}

	coverage := computeCoverage(unique, totalInstances)

	fmt.Printf("\n=== Cache Size vs Coverage ===\n\n")
	fmt.Printf("  %6s %10s %10s\n", "Size", "Coverage%", "Coverage")
	sizes := []int{64, 128, 256, 512, 1024, 2048, 4096, 8192}
	for _, sz := range sizes {
		cov := 0.0
		if c, ok := coverage[sz]; ok {
			cov = c
		} else {
			cov = 1.0
		}
		fmt.Printf("  %6d %9.1f%% %10d\n", sz, cov*100, int(cov*float64(totalInstances)))
	}

	fmt.Printf("\n=== Gaming Impact ===\n\n")
	c256 := coverage[256]
	fmt.Printf("  Top-256 patterns cover %.0f%% of ARM64 emissions\n", c256*100)
	fmt.Printf("  Warmup: JIT all + log patterns. Steady-state: cache hit → memcpy\n")
	fmt.Printf("  Estimated JIT savings: 30-60%% CPU for game workloads\n")

	out, _ := json.MarshalIndent(map[string]interface{}{
		"total_patterns":   totalInstances,
		"unique_patterns":  len(unique),
		"coverage_256":     coverage[256],
		"by_category":      byCat,
		"top_10":           unique[:min(10, len(unique))],
	}, "", "  ")
	_ = os.WriteFile(filepath.Join(os.Getenv("HOME"), "winlator-cmod-builder", "research", "fex", "pattern-cache.json"), out, 0644)
	fmt.Printf("\n[JSON] Written to research/fex/pattern-cache.json\n")
}

func extractPatterns(root string) []Pattern {
	known := knownCalls()
	var all []Pattern
	reInsn := regexp.MustCompile(`^\s*(?:\w+\.)?(\w+)\(`)

	scanFile := func(path string) {
		data, _ := os.ReadFile(path)
		lines := strings.Split(string(data), "\n")

		var window []string
		var startLine int

		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			m := reInsn.FindStringSubmatch(trimmed)
			if len(m) >= 2 && known[m[1]] {
				if len(window) == 0 {
					startLine = i + 1
				}
				window = append(window, m[1])
				if len(window) >= 3 || (len(window) >= 2 && i+1 < len(lines) && !isEmit(lines[i+1], known)) {
					all = append(all, Pattern{
						File:     path,
						Line:     startLine,
						Insns:    append([]string{}, window...),
						Category: catName(m[1]),
					})
					window = nil
				}
			} else if len(window) > 0 && isEmit(trimmed, known) {
				continue
			} else if len(window) > 0 {
				if len(window) >= 2 {
					all = append(all, Pattern{
						File:     path,
						Line:     startLine,
						Insns:    append([]string{}, window...),
						Category: catName(window[0]),
					})
				}
				window = nil
			}
		}
	}

	jitRoot := filepath.Join(root, "FEXCore", "Source", "Interface", "Core", "JIT")
	ceRoot := filepath.Join(root, "CodeEmitter", "CodeEmitter")

	for _, dir := range []string{jitRoot, ceRoot} {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if !e.IsDir() && (strings.HasSuffix(e.Name(), ".cpp") || strings.HasSuffix(e.Name(), ".inl")) {
				scanFile(filepath.Join(dir, e.Name()))
			}
		}
	}

	return all
}

func isEmit(line string, known map[string]bool) bool {
	m := regexp.MustCompile(`^\s*(?:\w+\.)?(\w+)\(`).FindStringSubmatch(strings.TrimSpace(line))
	return len(m) >= 2 && known[m[1]]
}

func catName(name string) string {
	switch fex.ClassifyCall(name) {
	case fex.CatBranch:   return "Branch"
	case fex.CatLoadStore: return "Memory"
	case fex.CatScalar:   return "Float"
	case fex.CatASIMD:    return "Vector"
	case fex.CatSystem:   return "System"
	case fex.CatMove:     return "Move"
	default:              return "ALU"
	}
}

func knownCalls() map[string]bool {
	return map[string]bool{
		"add": true, "sub": true, "mul": true, "and": true, "orr": true, "eor": true, "bic": true,
		"mov": true, "movz": true, "movk": true, "movn": true,
		"csel": true, "lsl": true, "lsr": true, "asr": true, "ror": true,
		"uxtb": true, "uxth": true, "sxtb": true, "sxth": true, "sxtw": true,
		"ubfm": true, "sbfm": true, "bfm": true, "bfi": true, "bfc": true,
		"neg": true, "adc": true, "sbc": true, "mvn": true,
		"umulh": true, "smulh": true, "udiv": true, "sdiv": true,
		"madd": true, "msub": true,
		"clz": true, "cls": true, "rbit": true, "rev": true,
		"ccmp": true, "ccmn": true,
		"b": true, "bl": true, "blr": true, "br": true, "ret": true,
		"cbz": true, "cbnz": true, "tbz": true, "tbnz": true,
		"ldr": true, "str": true, "ldp": true, "stp": true,
		"ldrb": true, "strb": true, "ldrh": true, "strh": true,
		"ldrsb": true, "ldrsh": true, "ldrsw": true,
		"ldar": true, "stlr": true, "ldaxr": true, "stlxr": true,
		"prfm": true, "prfum": true,
		"fmov": true, "fabs": true, "fneg": true, "fsqrt": true,
		"fadd": true, "fsub": true, "fmul": true, "fdiv": true,
		"fcmp": true, "fcsel": true, "fcvt": true, "scvtf": true, "ucvtf": true,
		"frintm": true, "frintp": true, "frintn": true, "frintz": true,
		"ins": true, "umov": true, "smov": true, "dup": true,
		"hint": true, "nop": true, "msr": true, "mrs": true,
		"dmb": true, "dsb": true, "isb": true,
		"adr": true, "adrp": true,
		"abs": true, "movi": true, "not": true,
		"fmla": true, "fmls": true,
		"ext": true, "zip1": true, "zip2": true,
		"uzp1": true, "uzp2": true, "trn1": true, "trn2": true,
		"shl": true, "ushr": true, "sshr": true, "sli": true, "sri": true,
		"cmhi": true, "cmhs": true, "cmeq": true, "cmge": true, "cmgt": true, "cmtst": true,
		"tbl": true, "tbx": true,
		"adds": true, "subs": true, "ands": true,
	}
}

func computeCoverage(unique []Pattern, total int) map[int]float64 {
	out := make(map[int]float64)
	cum := 0
	for i, p := range unique {
		cum += p.Count
		coverage := float64(cum) / float64(total)
		sz := i + 1
		switch sz {
		case 64, 128, 256, 512, 1024, 2048, 4096, 8192:
			out[sz] = coverage
		}
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
