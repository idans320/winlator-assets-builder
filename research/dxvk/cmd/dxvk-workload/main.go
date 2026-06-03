package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/idans/winlator-cmod-builder/research/internal/dxvk"
	"github.com/idans/winlator-cmod-builder/research/internal/stub"
)

func main() {
	scene := flag.String("scene", "all", "Scene preset or 'all'")
	frames := flag.Int("frames", 60, "Frames to simulate")
	seed := flag.Int64("seed", 42, "Random seed")
	output := flag.String("output", "", "JSON output")
	fp16 := flag.Bool("fp16", false, "FP16 half-cost GPU model")
	compare := flag.Bool("compare", false, "Baseline vs optimized")
	flag.Parse()

	cfg := stub.ExperimentConfig{Fp16HalfCost: *fp16}

	if *compare {
		runComp(*scene, *frames, *seed, *output)
		return
	}

	results := run(*scene, *frames, *seed, cfg)
	dxvk.PrintWorkloadSummary(results)
	if *output != "" {
		o, _ := json.MarshalIndent(results, "", "  ")
		os.WriteFile(*output, o, 0644)
	}
}

func run(name string, frames int, seed int64, cfg stub.ExperimentConfig) []*dxvk.WorkloadResult {
	var scenes []dxvk.SceneConfig
	if name == "all" {
		scenes = dxvk.ScenePresets
	} else {
		for _, s := range dxvk.ScenePresets {
			if s.Name == name {
				scenes = []dxvk.SceneConfig{s}
				break
			}
		}
	}
	var results []*dxvk.WorkloadResult
	for i, s := range scenes {
		fmt.Fprintf(os.Stderr, "%s (%d draws, %d frames)...\n", s.Name, s.DrawCalls, frames)
		r := dxvk.RunWorkload(s, frames, seed+int64(i)*1000, cfg)
		results = append(results, r)
	}
	return results
}

func runComp(name string, frames int, seed int64, output string) {
	var scene dxvk.SceneConfig
	for _, s := range dxvk.ScenePresets {
		if s.Name == name {
			scene = s
			break
		}
	}
	if scene.Name == "" {
		scene = dxvk.ScenePresets[2]
	}
	base := dxvk.RunWorkload(scene, frames, seed, stub.ExperimentConfig{})
	opt := dxvk.RunWorkload(scene, frames, seed, stub.ExperimentConfig{
		Fp16HalfCost: true, RedundantWriteFilter: true,
	})
	dxvk.PrintComparison(base, opt)
	if output != "" {
		cr := struct {
			Scene     string               `json:"scene"`
			Baseline  *dxvk.WorkloadResult `json:"baseline"`
			Optimized *dxvk.WorkloadResult `json:"optimized"`
		}{scene.Name, base, opt}
		o, _ := json.MarshalIndent(cr, "", "  ")
		os.WriteFile(output, o, 0644)
	}
}
