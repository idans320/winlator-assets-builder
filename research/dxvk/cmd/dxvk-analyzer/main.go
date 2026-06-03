package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/idans/winlator-cmod-builder/research/internal/dxvk"
)

func main() {
	rootDir := flag.String("dxvk-root", "", "Path to DXVK source root (src/ directory parent)")
	outputFile := flag.String("output", "output/dxvk_analysis.json", "JSON output path")
	flag.Parse()

	if *rootDir == "" {
		for _, c := range []string{"dxvk/workdir/dxvk"} {
			if info, err := os.Stat(c); err == nil && info.IsDir() {
				*rootDir = c
				break
			}
		}
	}
	if *rootDir == "" {
		fmt.Fprintln(os.Stderr, "ERROR: --dxvk-root is required")
		os.Exit(1)
	}

	srcDir := *rootDir + "/src"
	files := dxvk.DiscoverCCFiles(srcDir)
	fmt.Fprintf(os.Stderr, "Discovered %d DXVK source files\n", len(files))

	result := analyzeDXVK(files, srcDir)

	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		panic(err)
	}

	os.MkdirAll(filepath.Dir(*outputFile), 0755)
	os.WriteFile(*outputFile, out, 0644)

	fmt.Fprintf(os.Stderr, "Wrote %d bytes to %s\n", len(out), *outputFile)
	printSummary(result)
}

// ================================================================
// Data types
// ================================================================

type DXVKAnalysis struct {
	SourceRoot      string               `json:"source_root"`
	TotalFiles      int                  `json:"total_files"`
	TotalFunctions  int                  `json:"total_functions"`
	TotalLines      int                  `json:"total_lines"`
	VulkanEjects    []EjectionSite       `json:"vulkan_ejects"`
	ContextHotPaths []HotPath            `json:"context_hot_paths"`
	OptTargets      []OptimizationTarget `json:"optimization_targets"`
	FileStats       []FileStat           `json:"file_stats"`
	CategoryCounts  map[string]int       `json:"category_counts"`
}

type EjectionSite struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Function string `json:"function"`
	VKCall   string `json:"vk_call"`
	Category string `json:"category"`
}

type HotPath struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Function string `json:"function"`
	Call     string `json:"call"`
	Category string `json:"category"`
}

type OptimizationTarget struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	Files int    `json:"files"`
	Lines []struct {
		File     string `json:"file"`
		Line     int    `json:"line"`
		Function string `json:"function"`
	} `json:"top_lines"`
}

type FileStat struct {
	Path        string `json:"path"`
	TotalLines  int    `json:"total_lines"`
	Functions   int    `json:"functions"`
	VkEjects    int    `json:"vk_ejects"`
	HotPaths    int    `json:"hot_paths"`
	OptPatterns int    `json:"opt_patterns"`
}

type funcLoc struct {
	name string
	file string
	line int
}

// ================================================================
// Core analysis
// ================================================================

func analyzeDXVK(files []string, srcDir string) *DXVKAnalysis {
	result := &DXVKAnalysis{
		SourceRoot:     srcDir,
		TotalFiles:     len(files),
		CategoryCounts: make(map[string]int),
	}

	var allFuncs []funcLoc
	fileStatsMap := make(map[string]*FileStat)

	funcNameRe := regexp.MustCompile(`(?m)^(?:\w+(?:\s*[*&]+)?\s+)+(\w+)::(\w+)\s*\(([^)]*)\)\s*(?:const\s*)?\{`)
	funcDefRe := regexp.MustCompile(`(?m)^(?:\w+(?:\s*[*&]+|\s+))+\s*(\w+)\s*\(([^)]*)\)\s*(?:const\s*)?\{`)

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		source := string(data)
		lines := strings.Split(source, "\n")

		relPath := strings.TrimPrefix(f, srcDir+"/")
		fs := &FileStat{Path: relPath, TotalLines: len(lines)}
		fileStatsMap[f] = fs

		matches := funcNameRe.FindAllStringSubmatch(source, -1)
		if matches == nil {
			matches = funcDefRe.FindAllStringSubmatch(source, -1)
		}
		for _, m := range matches {
			var fname string
			lineNum := lineOf(source, m[0])
			if len(m) == 4 {
				fname = m[1] + "::" + m[2]
			} else {
				fname = m[1]
			}
			allFuncs = append(allFuncs, funcLoc{name: fname, file: relPath, line: lineNum})
			fs.Functions++
		}

		// Vulkan ejection points
		for lidx, line := range lines {
			if dxvk.IsVulkanEjectionPoint(line) {
				vkCall := strings.TrimSpace(line)
				if len(vkCall) > 80 {
					vkCall = vkCall[:80]
				}
				cat := categorizeVKCall(vkCall)
				ej := EjectionSite{
					File:     relPath,
					Line:     lidx + 1,
					Function: findFuncAt(allFuncs, relPath, lidx+1),
					VKCall:   vkCall,
					Category: cat,
				}
				result.VulkanEjects = append(result.VulkanEjects, ej)
				result.CategoryCounts[cat]++
				fs.VkEjects++
			}
		}

		// DXVK context hot paths
		for lidx, line := range lines {
			if dxvk.IsContextHotPath(line) {
				callStr := strings.TrimSpace(line)
				cat := categorizeMethod(callStr)
				hp := HotPath{
					File:     relPath,
					Line:     lidx + 1,
					Function: findFuncAt(allFuncs, relPath, lidx+1),
					Call:     callStr,
					Category: cat,
				}
				result.ContextHotPaths = append(result.ContextHotPaths, hp)
				result.CategoryCounts["hotpath:"+cat]++
				fs.HotPaths++
			}
		}

		// Optimization targets
		for _, line := range lines {
			targets := dxvk.FindOptimizationTargets(line)
			fs.OptPatterns += len(targets)
		}

		result.TotalLines += fs.TotalLines
		result.TotalFunctions += fs.Functions
	}

	// Build file stats list
	for f, fs := range fileStatsMap {
		fs.Path = strings.TrimPrefix(f, srcDir+"/")
		result.FileStats = append(result.FileStats, *fs)
	}
	sort.Slice(result.FileStats, func(i, j int) bool {
		return result.FileStats[i].HotPaths > result.FileStats[j].HotPaths
	})

	// Aggregate optimization targets
	optCounts := make(map[string]*OptimizationTarget)
	for _, fs := range fileStatsMap {
		data, _ := os.ReadFile(srcDir + "/" + fs.Path)
		lines := strings.Split(string(data), "\n")
		for lidx, line := range lines {
			names := dxvk.FindOptimizationTargets(line)
			for _, name := range names {
				if _, ok := optCounts[name]; !ok {
					optCounts[name] = &OptimizationTarget{Name: name}
				}
				t := optCounts[name]
				t.Count++
				if len(t.Lines) < 10 {
					t.Lines = append(t.Lines, struct {
						File     string `json:"file"`
						Line     int    `json:"line"`
						Function string `json:"function"`
					}{File: fs.Path, Line: lidx + 1, Function: ""})
				}
			}
		}
	}
	for _, t := range optCounts {
		fileSet := make(map[string]bool)
		for _, l := range t.Lines {
			fileSet[l.File] = true
		}
		t.Files = len(fileSet)
		result.OptTargets = append(result.OptTargets, *t)
	}
	sort.Slice(result.OptTargets, func(i, j int) bool {
		return result.OptTargets[i].Count > result.OptTargets[j].Count
	})

	return result
}

// ================================================================
// Helpers
// ================================================================

func lineOf(source string, substr string) int {
	idx := strings.Index(source, substr)
	if idx < 0 {
		return 0
	}
	return strings.Count(source[:idx], "\n") + 1
}

func findFuncAt(funcs []funcLoc, file string, line int) string {
	var best string
	var bestLine int
	for _, f := range funcs {
		if f.file == file && f.line <= line && f.line > bestLine {
			best = f.name
			bestLine = f.line
		}
	}
	return best
}

func categorizeVKCall(line string) string {
	switch {
	case strings.Contains(line, "cmdDraw"):
		return "draw"
	case strings.Contains(line, "cmdDispatch"):
		return "compute"
	case strings.Contains(line, "cmdBindPipeline"):
		return "pipeline-bind"
	case strings.Contains(line, "cmdBindDescriptor"):
		return "descriptor-bind"
	case strings.Contains(line, "cmdPipelineBarrier"):
		return "barrier"
	case strings.Contains(line, "cmdClear"):
		return "clear"
	case strings.Contains(line, "cmdCopy"):
		return "copy"
	case strings.Contains(line, "cmdBindVertex"):
		return "vertex-bind"
	case strings.Contains(line, "cmdBindIndex"):
		return "index-bind"
	case strings.Contains(line, "cmdPushConstants"):
		return "pushconstant"
	case strings.Contains(line, "cmdBeginRenderPass") || strings.Contains(line, "cmdEndRenderPass"):
		return "renderpass"
	case strings.Contains(line, "vkQueueSubmit") || strings.Contains(line, "submitCommandList"):
		return "submit"
	case strings.Contains(line, "vkAllocate") || strings.Contains(line, "allocCommandList"):
		return "allocation"
	default:
		return "other"
	}
}

func categorizeMethod(line string) string {
	for method, cat := range dxvk.ContextMethodCategories() {
		if strings.Contains(line, method) {
			return cat
		}
	}
	return "other"
}

func printSummary(result *DXVKAnalysis) {
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "╔════════════════════════════════════════════════════╗")
	fmt.Fprintln(os.Stderr, "║        DXVK STATIC ANALYSIS SUMMARY               ║")
	fmt.Fprintln(os.Stderr, "╠════════════════════════════════════════════════════╣")
	fmt.Fprintf(os.Stderr, "║ Files: %4d   Functions: %4d   Lines: %6d ║\n",
		result.TotalFiles, result.TotalFunctions, result.TotalLines)
	fmt.Fprintf(os.Stderr, "║ Vulkan eject sites: %4d                          ║\n",
		len(result.VulkanEjects))
	fmt.Fprintf(os.Stderr, "║ Context hot paths:  %4d                          ║\n",
		len(result.ContextHotPaths))
	fmt.Fprintln(os.Stderr, "╠════════════════════════════════════════════════════╣")
	fmt.Fprintln(os.Stderr, "║ Vulkan call categories:                            ║")
	cats := []string{"draw", "compute", "pipeline-bind", "descriptor-bind", "barrier", "clear", "copy", "renderpass", "submit", "pushconstant"}
	for _, c := range cats {
		fmt.Fprintf(os.Stderr, "║   %-18s: %4d                             ║\n", c, result.CategoryCounts[c])
	}
	fmt.Fprintln(os.Stderr, "╠════════════════════════════════════════════════════╣")
	fmt.Fprintln(os.Stderr, "║ Top optimization targets:                          ║")
	for i, t := range result.OptTargets {
		if i >= 8 {
			break
		}
		fmt.Fprintf(os.Stderr, "║   %-18s: %4d hits in %d files    ║\n", t.Name, t.Count, t.Files)
	}
	fmt.Fprintln(os.Stderr, "╠════════════════════════════════════════════════════╣")
	fmt.Fprintln(os.Stderr, "║ Top files by hot-path density:                     ║")
	for i, fs := range result.FileStats {
		if i >= 6 {
			break
		}
		fmt.Fprintf(os.Stderr, "║   %-30s hot=%3d vk=%3d ║\n", fs.Path, fs.HotPaths, fs.VkEjects)
	}
	fmt.Fprintln(os.Stderr, "╚════════════════════════════════════════════════════╝")
}
