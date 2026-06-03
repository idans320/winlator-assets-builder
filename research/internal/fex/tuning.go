package fex

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

type ARM64Template struct {
	ID          string
	Category    ARMOpCategory
	Insns       []string
	Hash        string
	AffectedOps []string
	Frequency   int
	File        string
	Line        int
}

type TemplateStats struct {
	Templates      []ARM64Template
	TotalTemplates int
	UniqueHashes   int
	SharedByOps    map[string]int
	Duplication    float64
}

var (
	reEmitTemplate = regexp.MustCompile(`^\s*\w+\.?(\w+)\(`)
)

func ExtractTemplates(fexRoot string) (*TemplateStats, error) {
	jitRoot := fexRoot + "/FEXCore/Source/Interface/Core/JIT"
	entries, err := os.ReadDir(jitRoot)
	if err != nil {
		return nil, err
	}

	known := knownEmissionCalls()
	templates := make(map[string]*ARM64Template)
	opSeq := make(map[string]map[string]int)

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".cpp") {
			continue
		}
		path := jitRoot + "/" + e.Name()
		data, _ := os.ReadFile(path)
		lines := strings.Split(string(data), "\n")

		var currentOp string
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)

			if m := reDefOp.FindStringSubmatch(trimmed); len(m) >= 2 {
				currentOp = m[1]
				continue
			}
			if strings.Contains(trimmed, "} //") || trimmed == "}" {
				if currentOp != "" && !strings.Contains(trimmed, "{") {
					continue
				}
			}

			m := reEmitTemplate.FindStringSubmatch(trimmed)
			if len(m) < 2 || !known[m[1]] {
				continue
			}
			call := m[1]
			hash := fmt.Sprintf("%s:%s", call, e.Name())

			t, ok := templates[hash]
			if !ok {
				t = &ARM64Template{
					ID:       hash,
					Category: ClassifyCall(call),
					Insns:    []string{call},
					Hash:     hash,
					File:     e.Name(),
					Line:     i + 1,
				}
				templates[hash] = t
			}
			t.Frequency++
			if currentOp != "" {
				t.AffectedOps = append(t.AffectedOps, currentOp)
				if opSeq[currentOp] == nil {
					opSeq[currentOp] = make(map[string]int)
				}
				opSeq[currentOp][hash]++
			}
		}
	}

	var tmpls []ARM64Template
	for _, t := range templates {
		tmpls = append(tmpls, *t)
	}

	uniqueHashes := make(map[string]bool)
	shared := make(map[string]int)
	for op, hashes := range opSeq {
		shared[op] = len(hashes)
		for h := range hashes {
			uniqueHashes[h] = true
		}
	}

	dup := 0.0
	if len(tmpls) > 0 {
		dup = float64(len(tmpls)-len(uniqueHashes)) / float64(len(tmpls)) * 100
	}

	return &TemplateStats{
		Templates:      tmpls,
		TotalTemplates: len(tmpls),
		UniqueHashes:   len(uniqueHashes),
		SharedByOps:    shared,
		Duplication:    dup,
	}, nil
}

type IRShape struct {
	OpName  string
	OpCategory string
	DestType string
	SrcTypes string
	OpSize   string
	HasDest  bool
}

type IRShapeFamily struct {
	Shape    IRShape
	OpCount  int
	Ops      []string
	EstARM64 int
}

func ClassifyIRShapes(ops []IROpDef) []IRShapeFamily {
	families := make(map[string]*IRShapeFamily)

	for _, op := range ops {
		shape := irShapeFromDef(op)
		key := fmt.Sprintf("%s|%s|%s|%t", shape.DestType, shape.SrcTypes, shape.OpSize, shape.HasDest)

		f, ok := families[key]
		if !ok {
			f = &IRShapeFamily{Shape: shape}
			families[key] = f
		}
		f.OpCount++
		f.Ops = append(f.Ops, op.Name)

		switch {
		case strings.Contains(op.Category, "Vector"):
			f.EstARM64 = 8
		case strings.Contains(op.Category, "Memory"):
			f.EstARM64 = 10
		case strings.Contains(op.Category, "F80"):
			f.EstARM64 = 15
		case strings.Contains(op.Category, "Atomic"):
			f.EstARM64 = 12
		case strings.Contains(op.Category, "Branch"):
			f.EstARM64 = 4
		default:
			f.EstARM64 = 5
		}
	}

	var result []IRShapeFamily
	for _, f := range families {
		result = append(result, *f)
	}
	return result
}

func irShapeFromDef(op IROpDef) IRShape {
	s := IRShape{
		OpName:     op.Name,
		OpCategory: op.Category,
		OpSize:     op.DestSize,
		HasDest:    op.HasDest,
	}

	parts := strings.Fields(op.RawDef)
	for i, p := range parts {
		if strings.Contains(p, "=") {
			for j := i + 1; j < len(parts); j++ {
				pt := parts[j]
				if strings.Contains(pt, ":") {
					colon := strings.Index(pt, ":")
					typ := pt[:colon]
					if typ == "GPR" {
						s.DestType = typ
						break
					}
				}
			}
		}
	}

	var srcs []string
	for _, p := range parts {
		if strings.Contains(p, ":") {
			colon := strings.Index(p, ":")
			typ := p[:colon]
			if typ == "GPR" || typ == "FPR" || typ == "SSA" || typ == "u64" ||
				typ == "u32" || typ == "u8" || typ == "i64" || typ == "i32" {
				srcs = append(srcs, typ)
			}
		}
	}
	s.SrcTypes = strings.Join(srcs, ",")

	return s
}

type TSOSite struct {
	File     string
	Line     int
	OpName   string
	Strippable bool
	Reason   string
}

func DetectTSOSites(fexRoot string) ([]TSOSite, int) {
	jitRoot := fexRoot + "/FEXCore/Source/Interface/Core/JIT"
	var sites []TSOSite
	strippable := 0

	reTSO := regexp.MustCompile(`ldar|stlr|dmb`)
	reStackBase := regexp.MustCompile(`(?i)rsp|rbp|base\s*==\s*RSP|base\s*==\s*RBP|Stack.*base|stack.*addr`)

	scanFile := func(path string) {
		data, _ := os.ReadFile(path)
		lines := strings.Split(string(data), "\n")
		var currentOp string

		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if m := reDefOp.FindStringSubmatch(trimmed); len(m) >= 2 {
				currentOp = m[1]
			}

			if reTSO.MatchString(trimmed) {
				site := TSOSite{
					File:   path,
					Line:   i + 1,
					OpName: currentOp,
				}

				if reStackBase.MatchString(strings.ToLower(trimmed)) {
					site.Strippable = true
					site.Reason = "stack-based access (RSP/RBP)"
					strippable++
				} else if strings.Contains(trimmed, "TSO") || strings.Contains(trimmed, "Volatile") {
					site.Reason = "general memory — keep barrier"
				} else {
					site.Reason = "unclassified — keep barrier"
				}

				sites = append(sites, site)
			}
		}
	}

	entries, _ := os.ReadDir(jitRoot)
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".cpp") {
			scanFile(jitRoot + "/" + e.Name())
		}
	}

	return sites, strippable
}

type SelfLoop struct {
	File     string
	Line     int
	OpName   string
	Hoistable bool
}

func DetectSelfLoops(fexRoot string) []SelfLoop {
	jitRoot := fexRoot + "/FEXCore/Source/Interface/Core/JIT"
	var loops []SelfLoop

	reBackBranch := regexp.MustCompile(`(?i)backward|back.*edge|self.*loop|loop.*back|back-to-start|JMP.*back`)
	reGuard := regexp.MustCompile(`(?i)side.*exit|helper.*call|syscall|yield|thunk|exitfunc`)

	scanFile := func(path string) {
		data, _ := os.ReadFile(path)
		lines := strings.Split(string(data), "\n")
		var currentOp string

		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if m := reDefOp.FindStringSubmatch(trimmed); len(m) >= 2 {
				currentOp = m[1]
			}

			if reBackBranch.MatchString(strings.ToLower(trimmed)) {
				loop := SelfLoop{
					File:     path,
					Line:     i + 1,
					OpName:   currentOp,
					Hoistable: true,
				}
				_ = reGuard
				loops = append(loops, loop)
			}
		}
	}

	entries, _ := os.ReadDir(jitRoot)
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".cpp") {
			scanFile(jitRoot + "/" + e.Name())
		}
	}

	return loops
}

type CacheConfig struct {
	Name              string
	Description       string
	WarmupFrames      int
	SteadyFrames      int
	BlockReusePct     float64
	ConstReusePct     float64
	SpillReusePct     float64
	TSOCoalescePct    float64
	TemplateCacheSz   int
	PrecompileEnabled bool

	ConstPoolEnabled     bool
	ConstPoolInsnsSaved  int
	TSOStripEnabled      bool
	TSOStripStackPct     float64
	SpillHoistEnabled    bool
	SpillHoistLoopPct    float64
}

var (
	GameConfig = CacheConfig{
		Name:              "game",
		Description:       "UE4/Unity game: assets loaded upfront, hot loops per frame",
		WarmupFrames:      120,
		SteadyFrames:      5000,
		BlockReusePct:     0.94,
		ConstReusePct:     0.85,
		SpillReusePct:     0.72,
		TSOCoalescePct:    0.40,
		TemplateCacheSz:   2048,
		PrecompileEnabled: true,
		ConstPoolEnabled:  true,
		ConstPoolInsnsSaved: 2,
		TSOStripEnabled:   true,
		TSOStripStackPct:  0.65,
		SpillHoistEnabled: true,
		SpillHoistLoopPct: 0.30,
	}
	ProConfig = CacheConfig{
		Name:              "professional",
		Description:       "Networking/DB/office: dynamic data, cold paths frequent",
		WarmupFrames:      20,
		SteadyFrames:      5000,
		BlockReusePct:     0.45,
		ConstReusePct:     0.25,
		SpillReusePct:     0.15,
		TSOCoalescePct:    0.10,
		TemplateCacheSz:   512,
		PrecompileEnabled: false,
		ConstPoolEnabled:  false,
		TSOStripEnabled:   false,
		SpillHoistEnabled: false,
	}
	LightGameConfig = CacheConfig{
		Name:              "light-game",
		Description:       "Isometric/pixel game: very small code footprint",
		WarmupFrames:      30,
		SteadyFrames:      10000,
		BlockReusePct:     0.98,
		ConstReusePct:     0.92,
		SpillReusePct:     0.85,
		TSOCoalescePct:    0.50,
		TemplateCacheSz:   4096,
		PrecompileEnabled: true,
		ConstPoolEnabled:  true,
		ConstPoolInsnsSaved: 2,
		TSOStripEnabled:   true,
		TSOStripStackPct:  0.78,
		SpillHoistEnabled: true,
		SpillHoistLoopPct: 0.40,
	}
)

func (c CacheConfig) IsGame() bool {
	return c.PrecompileEnabled && c.BlockReusePct > 0.70
}

func (c CacheConfig) WarmupJITFraction() float64 {
	return float64(c.WarmupFrames) / float64(c.WarmupFrames+c.SteadyFrames)
}
