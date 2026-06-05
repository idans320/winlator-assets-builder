package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/idans/winlator-cmod-builder/research/internal/graph"
)

type ctagsEntry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Kind     string `json:"kind"`
	Signature string `json:"signature"`
	Pattern   string `json:"pattern"`
}

type rgMatch struct {
	Type string  `json:"type"`
	Data rgData  `json:"data"`
}
type rgData struct {
	Path       rgPath  `json:"path"`
	Lines      rgLines `json:"lines"`
	LineNumber int     `json:"line_number"`
}
type rgPath struct {
	Text string `json:"text"`
}
type rgLines struct {
	Text string `json:"text"`
}

func main() {
	mesaFlag := flag.String("mesa", os.Getenv("MESA_SRC"), "Mesa source root")
	ctagsFlag := flag.String("ctags", "output/ctags_index.json", "ctags JSON output")
	gen8Flag := flag.String("gen8", "output/gen8_sites.json", "ripgrep gen8 sites JSON")
	outFlag := flag.String("out", "output/index.json", "output index JSON")
	pipeline := flag.Bool("pipeline", false, "run ctags + rg first, then build")
	flag.Parse()

	if *pipeline {
		runPipeline(*mesaFlag, *ctagsFlag, *gen8Flag)
	}

	ig, err := buildIndex(*ctagsFlag, *gen8Flag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	os.MkdirAll(filepath.Dir(*outFlag), 0755)
	out, err := os.Create(*outFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
	defer out.Close()

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(ig); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Index built:\n")
	fmt.Printf("  Functions:    %d\n", len(ig.Functions))
	fmt.Printf("  Gen8 sites:   %d\n", len(ig.Gen8Nodes))
	fmt.Printf("  Entry points: %d\n", len(ig.EntryPoints))
	fmt.Printf("  Edges:        %d\n", edgeCount(ig.Edges))
	fmt.Printf("  Wrote %s\n", *outFlag)
}

func runPipeline(mesa, ctagsJSON, gen8JSON string) {
	for _, step := range []struct{ cmd, desc string }{
		{"bash scripts/index-functions.sh " + mesa + " " + ctagsJSON, "ctags"},
		{"bash scripts/search-gen8.sh " + mesa + " " + gen8JSON, "ripgrep gen8"},
	} {
		fmt.Printf("  [%s] running...\n", step.desc)
	}
}

func buildIndex(ctagsFile, gen8File string) (*graph.IndexGraph, error) {
	funcs, err := parseCtags(ctagsFile)
	if err != nil {
		return nil, fmt.Errorf("parse ctags: %w", err)
	}

	sites, err := parseGen8Sites(gen8File)
	if err != nil {
		return nil, fmt.Errorf("parse gen8 sites: %w", err)
	}

	ig := &graph.IndexGraph{
		Functions:   make(map[string]*graph.FuncDef),
		Gen8Nodes:   make([]graph.Gen8Node, 0),
		EntryPoints: make([]string, 0),
		Edges:       make(map[string][]string),
	}

	fileFuncs := make(map[string][]*graph.FuncDef)
	for i := range funcs {
		f := &funcs[i]
		fd := &graph.FuncDef{
			Name:      f.Name,
			File:      f.Path,
			Line:      f.Line,
			Kind:      f.Kind,
			Signature: f.Signature,
			IsEntry:   isVulkanEntry(f.Name),
		}
		fileFuncs[fd.File] = append(fileFuncs[fd.File], fd)

		ig.Functions[f.Name] = fd
		if fd.IsEntry {
			ig.EntryPoints = append(ig.EntryPoints, f.Name)
		}
	}

	for _, fileFuncList := range fileFuncs {
		sort.Slice(fileFuncList, func(i, j int) bool {
			return fileFuncList[i].Line < fileFuncList[j].Line
		})
	}

	for i := range sites {
		s := &sites[i]
		file := s.Data.Path.Text
		line := s.Data.LineNumber
		content := s.Data.Lines.Text
		enclosing := findEnclosingFunction(file, line, fileFuncs)

		gn := graph.Gen8Node{
			ID:       fmt.Sprintf("%s:%s:g8:%d", filepath.Base(file), enclosing, line),
			File:     file,
			Line:     line,
			Kind:     classifyGen8Kind(content),
			Content:  snippet(content, 80),
			Regs:     extractA8XXRegs(content),
			Function: enclosing,
		}
		ig.Gen8Nodes = append(ig.Gen8Nodes, gn)

		if fd, ok := ig.Functions[enclosing]; ok {
			fd.Gen8Site = true
			fd.Gen8Lines = append(fd.Gen8Lines, line)
			fd.A8XXRegs = append(fd.A8XXRegs, gn.Regs...)
		}
	}

	ig.Gen8Nodes = dedupGen8Nodes(ig.Gen8Nodes)
	return ig, nil
}

func parseCtags(path string) ([]ctagsEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	var entries []ctagsEntry
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var e ctagsEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		if e.Kind == "function" || e.Kind == "prototype" {
			entries = append(entries, e)
		}
	}
	return entries, nil
}

func parseGen8Sites(path string) ([]rgMatch, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	var matches []rgMatch
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var m rgMatch
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		if m.Type == "match" {
			matches = append(matches, m)
		}
	}
	return matches, nil
}

func findEnclosingFunction(file string, line int, fileFuncs map[string][]*graph.FuncDef) string {
	funcs, ok := fileFuncs[file]
	if !ok {
		return filepath.Base(file) + "_unknown"
	}
	for i := len(funcs) - 1; i >= 0; i-- {
		if funcs[i].Line <= line {
			return funcs[i].Name
		}
	}
	return filepath.Base(file) + "_unknown"
}

func classifyGen8Kind(content string) string {
	lower := strings.ToLower(content)
	if strings.Contains(lower, "a8xx_") || strings.Contains(lower, "a6xx_") {
		return "gen8-register-write"
	}
	if strings.Contains(lower, "if") && strings.Contains(lower, "chip") {
		return "gen8-conditional"
	}
	return "gen8-reference"
}

func extractA8XXRegs(content string) []string {
	var regs []string
	seen := make(map[string]bool)
	fields := strings.FieldsFunc(content, func(r rune) bool {
		return !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_')
	})
	for _, f := range fields {
		upper := strings.ToUpper(f)
		if (strings.HasPrefix(upper, "A8XX_") || strings.HasPrefix(upper, "A6XX_")) && len(upper) > 5 {
			if !seen[upper] {
				seen[upper] = true
				regs = append(regs, upper)
			}
		}
	}
	return regs
}

func snippet(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func dedupGen8Nodes(nodes []graph.Gen8Node) []graph.Gen8Node {
	seen := make(map[string]bool)
	var out []graph.Gen8Node
	for _, n := range nodes {
		key := n.ID
		if !seen[key] {
			seen[key] = true
			out = append(out, n)
		}
	}
	return out
}

func isVulkanEntry(name string) bool {
	entryPrefixes := []string{"vkCmd", "vkCreate", "vkDestroy", "vkAllocate", "vkFree", "vkGet", "vkEnd", "vkBegin", "vkQueue", "tu_Cmd", "tu_Create", "tu_Destroy"}
	for _, p := range entryPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func edgeCount(edges map[string][]string) int {
	n := 0
	for _, callees := range edges {
		n += len(callees)
	}
	return n
}
