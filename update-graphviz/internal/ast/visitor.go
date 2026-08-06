package ast

import (
	"fmt"
	"path/filepath"
	"strings"
)

type AnalysisResult struct {
	Graph        *DataFlowGraph    `json:"graph"`
	Files        []*FileAnalysis   `json:"files"`
	Gen8Paths    []*Gen8CodePath   `json:"gen8_paths"`
	Architecture []*ArchComponent  `json:"architecture"`
}

type FileAnalysis struct {
	Path       string     `json:"path"`
	Functions  []*FuncInfo `json:"functions"`
	Structs    []*StructInfo `json:"structs"`
	Gen8Blocks int        `json:"gen8_blocks"`
}

type FuncInfo struct {
	Name           string        `json:"name"`
	Signature      string        `json:"signature"`
	Line           int           `json:"line"`
	Gen8Paths      []*Gen8CodePath `json:"gen8_paths,omitempty"`
	Calls          []string      `json:"calls"`
	WritesA8XX     bool          `json:"writes_a8xx"`
}

type StructInfo struct {
	Name   string `json:"name"`
	Line   int    `json:"line"`
	Gen8Fields []string `json:"gen8_fields,omitempty"`
}

type Gen8CodePath struct {
	Condition   string   `json:"condition"`
	Line        int      `json:"line"`
	File        string   `json:"file"`
	Operations  []string `json:"operations"`
	Registers   []string `json:"registers,omitempty"`
}

type ArchComponent struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Files       []string `json:"files"`
	DependsOn   []string `json:"depends_on"`
}

func AnalyzeFile(filePath string, source []byte) (*FileAnalysis, error) {
	tokens := Tokenize(string(source))
	p := NewParser(tokens)

	fa := &FileAnalysis{
		Path: filePath,
	}

	for p.pos < len(p.tokens) {
		tok := p.peek()
		if tok.Type == TokEOF {
			break
		}

		switch tok.Type {
		case TokIdent:
			fn := p.ParseFunction()
			if fn != nil {
				fi := &FuncInfo{
					Name:      fn.Name,
					Signature: fn.Signature,
					Line:      fn.Line,
				}

				extractFromNode(fn, &fi.Calls, &fi.Gen8Paths, &fi.WritesA8XX)
				fa.Functions = append(fa.Functions, fi)
			}
			continue

		case TokKeyword:
			if tok.Value == "struct" || tok.Value == "class" {
				_ = p.next()
				sn := p.ParseFunction()
				if sn != nil {
					si := &StructInfo{
						Name: sn.Name,
						Line: sn.Line,
					}
					for _, child := range sn.Children {
						if child.Kind == "struct_field" && child.Gen8 {
							si.Gen8Fields = append(si.Gen8Fields, child.Name)
						}
					}
					fa.Structs = append(fa.Structs, si)
				}
			} else {
				p.next()
			}
			continue

		case TokComment:
			comment := strings.TrimPrefix(tok.Value, "//")
			comment = strings.TrimPrefix(comment, "/*")
			comment = strings.TrimSuffix(comment, "*/")
			comment = strings.TrimSpace(comment)
			if strings.Contains(comment, "gen8") || strings.Contains(comment, "A8XX") || strings.Contains(comment, "a8xx") {
				fa.Gen8Blocks++
			}
			p.next()
			continue

		case TokPreprocessor:
			p.next()
			continue

		default:
			p.next()
		}
	}

	return fa, nil
}

func extractFromNode(node *ASTNode, calls *[]string, paths *[]*Gen8CodePath, writesA8XX *bool) {
	if node == nil {
		return
	}

	if node.Kind == "function_call" && node.Name != "" {
		*calls = append(*calls, node.Name)
		if strings.Contains(node.Name, "A8XX") || strings.Contains(node.Name, "a8xx") || strings.Contains(node.Name, "gen8") {
			*writesA8XX = true
		}
	}

	if node.Kind == "if_statement" && node.Gen8 {
		gp := &Gen8CodePath{
			Condition: node.Name,
			Line:      node.Line,
		}
		collectOperations(node, &gp.Operations, &gp.Registers)
		*paths = append(*paths, gp)
	}

	if node.Kind == "gen8_pattern" {
		gp := &Gen8CodePath{
			Condition: node.Name,
			Line:      node.Line,
			Operations: []string{node.Name},
		}
		*paths = append(*paths, gp)
	}

	for _, child := range node.Children {
		extractFromNode(child, calls, paths, writesA8XX)
	}
}

func collectOperations(node *ASTNode, ops *[]string, regs *[]string) {
	for _, child := range node.Children {
		if child.Kind == "function_call" {
			*ops = append(*ops, child.Name)
			if strings.Contains(child.Name, "A8XX") {
				*regs = append(*regs, child.Name)
			}
		}
		collectOperations(child, ops, regs)
	}
}

func BuildDataFlowGraph(analyses []*FileAnalysis) *DataFlowGraph {
	g := NewDataFlowGraph()

	g.AddNode("turnip_driver", "Turnip Vulkan Driver", "system", "src/freedreno/vulkan/", 0, false)
	g.AddNode("kgsl_kernel", "KGSL Kernel Interface", "system", "", 0, false)
	g.AddNode("adreno_gpu", "Adreno 8xx GPU", "hardware", "", 0, true)
	g.AddNode("vulkan_api", "Vulkan API", "interface", "", 0, false)

	g.AddEdge("vulkan_api", "turnip_driver", "Vulkan commands", false)
	g.AddEdge("turnip_driver", "kgsl_kernel", "ioctl calls", false)
	g.AddEdge("kgsl_kernel", "adreno_gpu", "submit command stream", true)

	fileToID := map[string]string{
		"tu_cmd_buffer.cc":   "cmd_buffer",
		"tu_cmd_buffer.h":    "cmd_buffer",
		"tu_cs.cc":           "cmd_stream",
		"tu_cs.h":            "cmd_stream",
		"tu_pipeline.cc":     "pipeline",
		"tu_pipeline.h":      "pipeline",
		"tu_clear_blit.cc":   "clear_blit",
		"tu_clear_blit.h":    "clear_blit",
		"tu_knl_kgsl.cc":     "kgsl_iface",
		"tu_knl.cc":          "kernel_iface",
		"tu_knl.h":           "kernel_iface",
		"tu_device.cc":       "device",
		"tu_device.h":        "device",
		"tu_shader.cc":       "shader",
		"tu_shader.h":        "shader",
		"tu_sampler.cc":      "sampler",
		"tu_sampler.h":       "sampler",
		"tu_lrz.cc":          "lrz",
		"tu_lrz.h":           "lrz",
		"tu_image.cc":        "image",
		"tu_image.h":         "image",
		"tu_query_pool.cc":   "query_pool",
		"tu_query_pool.h":    "query_pool",
		"tu_descriptor_set.cc": "descriptor_set",
		"tu_descriptor_set.h":  "descriptor_set",
		"tu_queue.cc":        "queue",
		"tu_queue.h":         "queue",
		"tu_suballoc.cc":     "suballoc",
		"tu_suballoc.h":      "suballoc",
		"tu_pass.cc":         "render_pass",
		"tu_pass.h":          "render_pass",
		"tu_tile_config.cc":  "tile_config",
		"tu_tile_config.h":   "tile_config",
	}

	for _, analysis := range analyses {
		baseFile := filepath.Base(analysis.Path)
		componentID := fileToID[baseFile]
		if componentID == "" {
			componentID = strings.TrimSuffix(baseFile, filepath.Ext(baseFile))
		}

		g.AddNode(componentID, baseFile, "component", analysis.Path, 0, false)
		g.AddEdge("turnip_driver", componentID, "contains", false)

		for _, fn := range analysis.Functions {
			funcID := componentID + "/" + fn.Name
			g.AddNode(funcID, fn.Name, "function", analysis.Path, fn.Line, fn.WritesA8XX)
			g.AddEdge(componentID, funcID, "defines", fn.WritesA8XX)

			for _, call := range fn.Calls {
				callTarget := resolveCall(call, fileToID, g)
				if callTarget != "" {
					isGen8 := fn.WritesA8XX || strings.Contains(call, "A8XX") || strings.Contains(call, "a8xx")
					g.AddEdge(funcID, callTarget, "calls "+call, isGen8)
				}
			}

			if fn.WritesA8XX {
				g.AddEdge(funcID, "adreno_gpu", "programs register", true)
			}

			for _, gp := range fn.Gen8Paths {
				gen8PathID := componentID + "/gen8_path_" + fmt.Sprintf("%d", gp.Line)
				g.AddNode(gen8PathID, "gen8: "+gp.Condition, "gen8_path", analysis.Path, gp.Line, true)
				g.AddEdge(funcID, gen8PathID, "branch", true)

				for _, op := range gp.Operations {
					opID := resolveCall(op, fileToID, g)
					if opID != "" {
						g.AddEdge(gen8PathID, opID, "invokes", true)
					}
				}

				for _, reg := range gp.Registers {
					regID := "register/" + cleanReg(reg)
					g.AddNode(regID, cleanReg(reg), "register", "", gp.Line, true)
					g.AddEdge(gen8PathID, regID, "writes", true)
				}
			}
		}

		for _, st := range analysis.Structs {
			structID := componentID + "/" + st.Name
			g.AddNode(structID, "struct "+st.Name, "struct", analysis.Path, st.Line, len(st.Gen8Fields) > 0)
			g.AddEdge(componentID, structID, "defines", len(st.Gen8Fields) > 0)

			for _, gf := range st.Gen8Fields {
				fieldID := structID + "." + gf
				g.AddNode(fieldID, gf, "gen8_field", analysis.Path, st.Line, true)
				g.AddEdge(structID, fieldID, "gen8 field", true)
			}
		}
	}

	addKnownEdges(g)
	addArchitectureNodes(g)

	return g
}

func resolveCall(call string, fileToID map[string]string, g *DataFlowGraph) string {
	parts := strings.Split(call, "_")
	if len(parts) >= 2 {
		prefix := parts[0] + "_" + parts[1]
		if id, ok := fileToID[prefix+".cc"]; ok {
			return id
		}
		if id, ok := fileToID[prefix+".h"]; ok {
			return id
		}
	}

	if strings.Contains(call, "tu_cs") || strings.Contains(call, "tu_cs_emit") {
		return "cmd_stream"
	}
	if strings.Contains(call, "tu_cmd") {
		return "cmd_buffer"
	}
	if strings.Contains(call, "tu_pipeline") {
		return "pipeline"
	}
	if strings.Contains(call, "tu_clear") || strings.Contains(call, "tu_blit") {
		return "clear_blit"
	}
	if strings.Contains(call, "kgsl") || strings.Contains(call, "KGSL") {
		return "kgsl_iface"
	}
	if strings.Contains(call, "tu_device") {
		return "device"
	}
	if strings.Contains(call, "tu_sampler") {
		return "sampler"
	}
	if strings.Contains(call, "tu_lrz") {
		return "lrz"
	}
	if strings.Contains(call, "tu_image") {
		return "image"
	}
	if strings.Contains(call, "tu_shader") || strings.Contains(call, "ir3") {
		return "shader"
	}
	if strings.Contains(call, "tu_queue") || strings.Contains(call, "submit") {
		return "queue"
	}
	if strings.Contains(call, "tu_pass") {
		return "render_pass"
	}
	if strings.Contains(call, "tu_query") {
		return "query_pool"
	}
	if strings.Contains(call, "tu_descriptor") {
		return "descriptor_set"
	}
	if strings.Contains(call, "tu_suballoc") {
		return "suballoc"
	}

	return ""
}

func cleanReg(reg string) string {
	reg = strings.TrimPrefix(reg, "A8XX_")
	reg = strings.TrimPrefix(reg, "A6XX_")
	reg = strings.TrimPrefix(reg, "a8xx_")
	return reg
}

func addKnownEdges(g *DataFlowGraph) {
	knownFlows := []struct {
		from, to, label string
		gen8            bool
	}{
		{"cmd_buffer", "cmd_stream", "tu_cs_emit() writes PM4 packets", false},
		{"cmd_buffer", "pipeline", "pipeline state setup", false},
		{"cmd_buffer", "clear_blit", "resolve GMEM blits", false},
		{"cmd_buffer", "kgsl_iface", "tu_submit", false},
		{"pipeline", "shader", "ir3 compile", false},
		{"pipeline", "descriptor_set", "layout binding", false},
		{"shader", "adreno_gpu", "shader binary upload", true},
		{"clear_blit", "image", "resolve image layout", false},
		{"clear_blit", "descriptor_set", "blit descriptor", false},
		{"device", "kgsl_iface", "KGSL device init", false},
		{"device", "queue", "queue creation", false},
		{"device", "sampler", "sampler state", false},
		{"device", "lrz", "LRZ init", false},
		{"queue", "kgsl_iface", "submitqueue_new", false},
		{"kgsl_iface", "kgsl_kernel", "IOCTL_KGSL_SUBMIT_COMMANDS", false},
		{"descriptor_set", "image", "texture descriptor", false},
		{"descriptor_set", "sampler", "sampler descriptor", false},
		{"render_pass", "tile_config", "tile rendering config", false},
		{"render_pass", "clear_blit", "load/store ops", false},
		{"cmd_stream", "suballoc", "BO suballocation", false},
	}

	for _, flow := range knownFlows {
		g.AddEdge(flow.from, flow.to, flow.label, flow.gen8)
	}
}

func addArchitectureNodes(g *DataFlowGraph) {
	gen8TessID := "pipeline/tess_bo"
	g.AddNode(gen8TessID, "Tess BO (gen8×2 sized)", "gen8_buffer", "tu_device.h:545", 0, true)
	g.AddEdge("pipeline", gen8TessID, "allocates for two draws", true)

	gen8ByteAddr := "cmd_buffer/byte_addressing"
	g.AddNode(gen8ByteAddr, "Byte Addressing (gen8)", "gen8_feature", "tu_cmd_buffer.cc:4771", 0, true)
	g.AddEdge("cmd_buffer", gen8ByteAddr, "gen8 buffer descriptor addressing", true)

	gen8DepthClamp := "pipeline/depth_01_clamp"
	g.AddNode(gen8DepthClamp, "Depth [0,1] Clamp (gen8)", "gen8_feature", "tu_pipeline.cc:3723", 0, true)
	g.AddEdge("pipeline", gen8DepthClamp, "enables depth clamp", true)

	gen8Perf := "device/perfcntr_uapi"
	g.AddNode(gen8Perf, "Perfcntr UAPI (gen8+)", "gen8_feature", "tu_device.cc:1824", 0, true)
	g.AddEdge("device", gen8Perf, "uses kernel perfcntr UAPI", true)
	g.AddEdge(gen8Perf, "kgsl_kernel", "perfcntr management", true)

	gen8Marker := "cmd_buffer/cp_set_marker"
	g.AddNode(gen8Marker, "CP_SET_MARKER (gen8)", "gen8_feature", "tu_cmd_buffer.cc:300", 0, true)
	g.AddEdge(gen8Marker, "adreno_gpu", "SET_MARKER cmd", true)
	g.AddEdge("cmd_buffer", gen8Marker, "emits A8XX_CP_SET_MARKER", true)

	gen8BinPass := "cmd_buffer/binning_pass"
	g.AddNode(gen8BinPass, "Binning Pass (gen8)", "gen8_feature", "tu_cmd_buffer.cc:2264", 0, true)
	g.AddEdge("cmd_buffer", gen8BinPass, "KMD-programmed non-ctx regs on gen8+", true)

	gen8TexMemobj := "descriptor_set/a8xx_tex_memobj"
	g.AddNode(gen8TexMemobj, "A8XX TEX_MEMOBJ Descriptors", "gen8_feature", "tu_descriptor_set.h", 0, true)
	g.AddEdge("descriptor_set", gen8TexMemobj, "gen8 descriptor packing (A8XX_TEX_MEMOBJ_*)", true)

	gen8Combiner := "pipeline/fsr_combiner_clamp"
	g.AddNode(gen8Combiner, "FSR_COMBINER_CLAMP (gen8)", "gen8_feature", "tu_pipeline.cc:3864", 0, true)
	g.AddEdge("pipeline", gen8Combiner, "CLAMP_16_SAMP vs 4x4", true)
}

func FormatAnalysis(result *AnalysisResult) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("=== Turnip gen8 AST Analysis ===\n"))
	sb.WriteString(fmt.Sprintf("Files analyzed: %d\n", len(result.Files)))
	sb.WriteString(fmt.Sprintf("Gen8 code paths: %d\n\n", len(result.Gen8Paths)))

	for _, file := range result.Files {
		sb.WriteString(fmt.Sprintf("--- %s ---\n", file.Path))
		sb.WriteString(fmt.Sprintf("  Functions: %d, Structs: %d, Gen8 blocks: %d\n",
			len(file.Functions), len(file.Structs), file.Gen8Blocks))

		for _, fn := range file.Functions {
			if !fn.WritesA8XX && len(fn.Gen8Paths) == 0 {
				continue
			}
			sb.WriteString(fmt.Sprintf("  [gen8] %s (line %d)\n", fn.Name, fn.Line))
			for _, gp := range fn.Gen8Paths {
				sb.WriteString(fmt.Sprintf("    |-- %s at line %d\n", gp.Condition, gp.Line))
				for _, op := range gp.Operations {
					sb.WriteString(fmt.Sprintf("    |   `-- %s\n", op))
				}
			}
		}

		for _, st := range file.Structs {
			if len(st.Gen8Fields) == 0 {
				continue
			}
			sb.WriteString(fmt.Sprintf("  [gen8 struct] %s (line %d)\n", st.Name, st.Line))
			for _, gf := range st.Gen8Fields {
				sb.WriteString(fmt.Sprintf("    |-- gen8 field: %s\n", gf))
			}
		}

		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("=== Data Flow Graph ===\n"))
	sb.WriteString(fmt.Sprintf("Nodes: %d, Edges: %d\n", len(result.Graph.Nodes), len(result.Graph.Edges)))

	gen8Nodes := result.Graph.GetGen8Nodes()
	sb.WriteString(fmt.Sprintf("Gen8-specific nodes: %d\n", len(gen8Nodes)))

	return sb.String()
}

func FormatShortAnalysis(result *AnalysisResult) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Files: %d | Functions (gen8): %d | Graph nodes: %d | Gen8 nodes: %d",
		len(result.Files),
		countGen8Functions(result),
		len(result.Graph.Nodes),
		len(result.Graph.GetGen8Nodes()),
	))
	return sb.String()
}

func countGen8Functions(result *AnalysisResult) int {
	count := 0
	for _, f := range result.Files {
		for _, fn := range f.Functions {
			if fn.WritesA8XX || len(fn.Gen8Paths) > 0 {
				count++
			}
		}
	}
	return count
}

func CreateArchitecture() []*ArchComponent {
	return []*ArchComponent{
		{
			Name:        "Command Buffer Layer",
			Description: "Translates Vulkan draw/dispatch commands into PM4 command stream packets for Adreno GPU",
			Files:       []string{"tu_cmd_buffer.cc", "tu_cmd_buffer.h", "tu_cs.cc", "tu_cs.h", "tu_cs_breadcrumbs.cc"},
			DependsOn:   []string{"Command Stream Layer", "Pipeline Layer", "Kernel Interface"},
		},
		{
			Name:        "Command Stream Layer",
			Description: "Low-level PM4 packet emission, buffer object management, suballocation",
			Files:       []string{"tu_cs.cc", "tu_cs.h", "tu_suballoc.cc", "tu_suballoc.h"},
			DependsOn:   []string{"Kernel Interface"},
		},
		{
			Name:        "Pipeline Layer",
			Description: "Vulkan pipeline creation, shader state, depth/stencil/blend config, gen8 FSR combiner clamp",
			Files:       []string{"tu_pipeline.cc", "tu_pipeline.h", "tu_shader.cc", "tu_shader.h"},
			DependsOn:   []string{"Shader Compiler (IR3)", "Descriptor Set Layer"},
		},
		{
			Name:        "Descriptor Set Layer",
			Description: "Vulkan descriptor set layout, texture/sampler/buffer descriptor packing, gen8 TEX_MEMOBJ format",
			Files:       []string{"tu_descriptor_set.cc", "tu_descriptor_set.h", "tu_sampler.cc", "tu_sampler.h"},
			DependsOn:   []string{"Image Layer", "Command Buffer Layer"},
		},
		{
			Name:        "Clear/Blit Layer",
			Description: "Resolve operations, GMEM load/store, sysmem blit, format conversion, gen8 GMEM offset",
			Files:       []string{"tu_clear_blit.cc", "tu_clear_blit.h"},
			DependsOn:   []string{"Command Buffer Layer", "Descriptor Set Layer", "Image Layer"},
		},
		{
			Name:        "Image Layer",
			Description: "Vulkan image creation, layout transitions, UBWC/tiling, format properties",
			Files:       []string{"tu_image.cc", "tu_image.h"},
			DependsOn:   []string{"Kernel Interface"},
		},
		{
			Name:        "LRZ (Late-Z Resolve) Layer",
			Description: "Late-Z resolve for bandwidth optimization, gen8 LRZ behavior differences",
			Files:       []string{"tu_lrz.cc", "tu_lrz.h"},
			DependsOn:   []string{"Command Buffer Layer", "Pipeline Layer"},
		},
		{
			Name:        "Kernel Interface (KGSL)",
			Description: "Qualcomm KGSL ioctl interface: buffer alloc, submitqueue, timestamp wait, gen8 perfcntr UAPI",
			Files:       []string{"tu_knl.cc", "tu_knl.h", "tu_knl_kgsl.cc", "tu_knl_drm.cc", "tu_knl_drm_msm.cc"},
			DependsOn:   []string{},
		},
		{
			Name:        "Device Management",
			Description: "Physical/logical device creation, queue families, memory types, gen8 device dispatch table",
			Files:       []string{"tu_device.cc", "tu_device.h", "tu_queue.cc", "tu_queue.h"},
			DependsOn:   []string{"Kernel Interface (KGSL)"},
		},
		{
			Name:        "Tile Rendering",
			Description: "GMEM tile configuration, binning pass setup, render pass begin/end",
			Files:       []string{"tu_pass.cc", "tu_pass.h", "tu_tile_config.cc", "tu_tile_config.h"},
			DependsOn:   []string{"Command Buffer Layer", "Clear/Blit Layer"},
		},
		{
			Name:        "gen8: Adreno 8xx Extensions",
			Description: "All gen8-specific code paths: A8XX registers, CP_SET_MARKER, byte-addressed descriptors, tess BO sizing, perfcntr UAPI, depth clamp, FSR combiner",
			Files:       []string{},
			DependsOn:   []string{
				"Command Buffer Layer",
				"Pipeline Layer",
				"Descriptor Set Layer",
				"Clear/Blit Layer",
				"Kernel Interface (KGSL)",
				"Device Management",
				"LRZ (Late-Z Resolve) Layer",
			},
		},
	}
}
