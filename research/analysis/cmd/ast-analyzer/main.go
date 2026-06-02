package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/idans/winlator-cmod-builder/research/internal/ast"
	"github.com/idans/winlator-cmod-builder/research/internal/mesa"
)

func main() {
	mesaFlag := flag.String("mesa", "", "Path to Mesa source root (env: MESA_SRC)")
	outFlag := flag.String("out", "output/analysis.json", "Output JSON path")
	summaryOnly := flag.Bool("summary", false, "Print summary only, no full text")
	flag.Parse()

	mesaRoot := *mesaFlag
	if mesaRoot == "" {
		mesaRoot = os.Getenv("MESA_SRC")
	}
	if mesaRoot == "" {
		fmt.Fprintln(os.Stderr, "Error: --mesa or $MESA_SRC required")
		os.Exit(1)
	}

	files := mesa.DiscoverCCFiles(mesaRoot)
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "Error: no Turnip source files found")
		os.Exit(1)
	}

	fmt.Printf("=== Turnip gen8 Neuron Tracer ===\n")
	fmt.Printf("Mesa: %s\n", mesaRoot)
	fmt.Printf("Files to analyze: %d\n\n", len(files))

	globalCG := ast.NewCallGraph()
	var mu sync.Mutex
	var wg sync.WaitGroup

	pass1CG := ast.NewCallGraph()
	for _, f := range files {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			src, err := os.ReadFile(path)
			if err != nil {
				return
			}
			cg := ast.ParseFile(filepath.Base(path), src, nil)
			mu.Lock()
			pass1CG.Merge(cg)
			mu.Unlock()
		}(f)
	}
	wg.Wait()

	for _, f := range files {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			src, err := os.ReadFile(path)
			if err != nil {
				return
			}
			base := filepath.Base(path)
			known := make(map[string]*ast.FuncDef)
			for name, fn := range pass1CG.Functions {
				if fn.File != base {
					known[name] = fn
				}
			}
			cg := ast.ParseFile(base, src, known)
			mu.Lock()
			globalCG.Merge(cg)
			mu.Unlock()
		}(f)
	}
	wg.Wait()

	gen8Files := 0
	gen8Functions := 0
	totalGen8Nodes := len(globalCG.Gen8Nodes)
	for _, fn := range globalCG.Functions {
		if fn.Gen8Site {
			gen8Functions++
		}
	}
	for f := range globalCG.FileDefs {
		for _, fn := range globalCG.FileDefs[f] {
			if fn.Gen8Site {
				gen8Files++
				break
			}
		}
	}

	fmt.Printf("Functions parsed: %d\n", len(globalCG.Functions))
	fmt.Printf("Gen8 functions:  %d\n", gen8Functions)
	fmt.Printf("Gen8 files:      %d\n", gen8Files)
	fmt.Printf("Gen8 nodes:      %d (terminal gen8 code sites)\n", totalGen8Nodes)
	fmt.Printf("Entry points:    %d\n", len(globalCG.EntryPoints))
	fmt.Printf("Total edges:     %d\n\n", len(globalCG.Edges))

	fmt.Println("--- Gen8 Terminal Nodes ---")
	count := 0
	for _, gn := range globalCG.Gen8Nodes {
		if count >= 30 {
			fmt.Printf("  ... and %d more gen8 nodes (see JSON for full list)\n", totalGen8Nodes-30)
			break
		}
		count++
		fmt.Printf("  %-55s %-20s line=%-5d %s\n",
			truncateStr(gn.Content, 55),
			gn.Function,
			gn.Line,
			gn.Kind,
		)
	}

	fmt.Println("\n--- Gen8 Functions ---")
	count = 0
	for name, fn := range globalCG.Functions {
		if !fn.Gen8Site {
			continue
		}
		count++
		fmt.Printf("  %-35s %-30s %d gen8 sites, %d calls\n",
			fn.File, name, len(fn.Gen8Lines), len(fn.Callees))
	}

	fmt.Println("\n--- Entry Points ---")
	for _, ep := range globalCG.EntryPoints {
		fn := globalCG.Functions[ep]
		if fn == nil {
			continue
		}
		fmt.Printf("  %-35s %s\n", ep, fn.File)
	}

	if !*summaryOnly {
		traced := globalCG.TraceBackFromGen8()
		fmt.Printf("\n--- Traced Gen8 Pathways (%d nodes, %d edges) ---\n",
			len(traced.Nodes), len(traced.Edges))

		gen8PathEdges := 0
		for k := range traced.Edges {
			if strings.Contains(k, "gen8") {
				gen8PathEdges++
			}
		}
		fmt.Printf("  Gen8-involved edges: %d\n", gen8PathEdges)
	}

	os.MkdirAll(filepath.Dir(*outFlag), 0755)
	data, err := json.MarshalIndent(globalCG, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "JSON error: %v\n", err)
		os.Exit(1)
	}
	os.WriteFile(*outFlag, data, 0644)
	fmt.Printf("\nJSON written to %s (%d bytes)\n", *outFlag, len(data))
}

func truncateStr(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
