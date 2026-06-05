package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/idans/winlator-cmod-builder/research/internal/graph"
)

func main() {
	inputFlag := flag.String("input", "output/index.json", "index JSON (from index-builder)")
	outdirFlag := flag.String("outdir", "output/", "output directory for DOT and SVG files")
	flag.Parse()

	ig, err := loadIndex(*inputFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	dg := graph.NewDotGen(*outdirFlag)
	if err := dg.Generate(ig); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\nDone. SVG diagrams can be rendered with:")
	fmt.Println("  for f in output/*.dot; do sfdp -Tsvg -Goverlap=false \"$f\" -o output/$(basename \"$f\" .dot).svg; done")
}

func loadIndex(path string) (*graph.IndexGraph, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ig graph.IndexGraph
	if err := json.Unmarshal(data, &ig); err != nil {
		return nil, err
	}
	return &ig, nil
}
