package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/idans/winlator-cmod-builder/research/internal/ast"
	"github.com/idans/winlator-cmod-builder/research/internal/graph"
)

func main() {
	inputFlag := flag.String("input", "output/analysis.json", "Input analysis JSON")
	outDirFlag := flag.String("outdir", "output/", "Output directory")
	flag.Parse()

	data, err := os.ReadFile(*inputFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\nRun ast-analyzer first.\n", err)
		os.Exit(1)
	}

	var cg ast.CallGraph
	if err := json.Unmarshal(data, &cg); err != nil {
		fmt.Fprintf(os.Stderr, "JSON error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("=== Graphviz DOT Generator ===\n")
	fmt.Printf("Functions: %d\n", len(cg.Functions))
	fmt.Printf("Gen8 nodes: %d\n", len(cg.Gen8Nodes))
	fmt.Printf("Entry points: %d\n", len(cg.EntryPoints))
	fmt.Printf("Edges: %d\n\n", len(cg.Edges))

	gen := graph.NewDotGen(*outDirFlag)
	gen.Generate(&cg)

	fmt.Printf("\nDone! DOT files in %s/\n", *outDirFlag)
	fmt.Printf("\nRender with Graphviz:\n")
	fmt.Printf("  dot -Tsvg %s/01_turnip_gen8_architecture.dot -o arch.svg\n", *outDirFlag)
	fmt.Printf("  dot -Tsvg %s/02_gen8_neuron_paths.dot -o neurons.svg\n", *outDirFlag)
	fmt.Printf("  dot -Tsvg %s/03_call_graph_full.dot -o full.svg\n", *outDirFlag)
}
