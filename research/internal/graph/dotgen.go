package graph

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type DotGen struct {
	OutDir string
}

func NewDotGen(outDir string) *DotGen {
	os.MkdirAll(outDir, 0755)
	return &DotGen{OutDir: outDir}
}

func (d *DotGen) Generate(ig *IndexGraph) error {
	names := []string{"01_turnip_gen8_architecture", "02_gen8_neuron_paths", "03_call_graph_focused"}
	var paths []string
	for _, name := range names {
		p, err := d.writeDOT(name, ig)
		if err != nil {
			return err
		}
		paths = append(paths, p)
	}
	for _, p := range paths {
		fmt.Printf("  Wrote %s\n", filepath.Base(p))
	}
	return nil
}

func (d *DotGen) writeDOT(name string, ig *IndexGraph) (string, error) {
	var dot string
	switch name {
	case "01_turnip_gen8_architecture":
		dot = d.architectureDOT()
	case "02_gen8_neuron_paths":
		dot = d.neuronPathsDOT(ig)
	case "03_call_graph_focused":
		dot = d.focusedCallGraphDOT(ig)
	}
	path := filepath.Join(d.OutDir, name+".dot")
	if err := os.WriteFile(path, []byte(dot), 0644); err != nil {
		return "", err
	}
	return path, nil
}

func (d *DotGen) architectureDOT() string {
	return `digraph Architecture {
	rankdir=TB
	bgcolor="#0d1117"
	fontname="monospace"
	label="Turnip Adreno 8xx (gen8) Driver Architecture\nVulkan API to GPU Register Data Flow"
	fontsize=20 fontcolor="#e6edf3"
	labelloc="t"
	nodesep=0.4 ranksep=0.6
	compound=true

	node [fontname="monospace" fontsize=10 shape=box style="filled" penwidth=1.5]

	subgraph cluster_entry {
		label="Vulkan API Layer"
		fontcolor="#58a6ff" fontsize=14
		bgcolor="#161b22" color="#58a6ff"
		node [fillcolor="#1f2937" color="#58a6ff" fontcolor="#e6edf3"]
		vk_api [label=<<TABLE BORDER="0"><TR><TD><B>Vulkan API Commands</B></TD></TR><TR><TD>vkCmdDraw / vkCmdDispatch</TD></TR><TR><TD>vkCmdPipelineBarrier</TD></TR><TR><TD>vkCmdBeginRenderPass</TD></TR><TR><TD>vkCmdClearAttachments</TD></TR><TR><TD>vkCmdBlitImage / CopyImage</TD></TR><TR><TD>vkCreateGraphicsPipelines</TD></TR></TABLE>>]
	}

	subgraph cluster_turnip {
		label="Turnip Driver (src/freedreno/vulkan/)"
		fontcolor="#3fb950" fontsize=14
		bgcolor="#161b22" color="#3fb950"

		subgraph cluster_cmd {
			label="Command Buffer" fontcolor="#7ee787"
			bgcolor="#1a2e1a" color="#3fb950"
			node [fillcolor="#1a3a1a" color="#3fb950" fontcolor="#d2f8d2"]
			cmd_buffer [label="tu_cmd_buffer"]
			cmd_stream [label="tu_cs (PM4 emitter)"]
			suballoc [label="tu_suballoc (BO)"]
		}

		subgraph cluster_pipe {
			label="Pipeline" fontcolor="#7ee787"
			bgcolor="#1a2e1a" color="#3fb950"
			node [fillcolor="#1a3a1a" color="#3fb950" fontcolor="#d2f8d2"]
			pipeline [label="tu_pipeline"]
			shader [label="tu_shader (IR3)"]
			descriptor [label="tu_descriptor_set"]
			sampler [label="tu_sampler"]
		}

		subgraph cluster_gmem {
			label="GMEM/Tile" fontcolor="#7ee787"
			bgcolor="#1a2e1a" color="#3fb950"
			node [fillcolor="#1a3a1a" color="#3fb950" fontcolor="#d2f8d2"]
			clear_blit [label="tu_clear_blit"]
			render_pass [label="tu_pass"]
			tile_config [label="tu_tile_config"]
		}

		subgraph cluster_device {
			label="Device" fontcolor="#7ee787"
			bgcolor="#1a2e1a" color="#3fb950"
			node [fillcolor="#1a3a1a" color="#3fb950" fontcolor="#d2f8d2"]
			device [label="tu_device"]
			queue [label="tu_queue"]
			lrz [label="tu_lrz"]
		}
	}

	subgraph cluster_kernel {
		label="KGSL Kernel Interface"
		fontcolor="#d2a8ff" fontsize=14
		bgcolor="#161b22" color="#a371f7"
		node [fillcolor="#251a3a" color="#a371f7" fontcolor="#e2d4ff"]
		kgsl [label="tu_knl_kgsl"]
		kgsl_drv [label="Linux KGSL Driver"]
	}

	subgraph cluster_gen8 {
		label="Adreno 8xx Features"
		fontcolor="#ff7b72" fontsize=14
		bgcolor="#161b22" color="#ff7b72"
		style=dashed
		node [fillcolor="#3a1a1a" color="#ff7b72" fontcolor="#ffd2cc"]
		g_marker [label="A8XX_CP_SET_MARKER"]
		g_byteaddr [label="Byte-addressed Descriptors"]
		g_tess [label="Tess BO x2"]
		g_depth [label="Depth [0,1] Clamp"]
		g_combiner [label="FSR Combiner"]
		g_binpass [label="Binning Pass"]
		g_memobj [label="A8XX TEX_MEMOBJ"]
		g_samp [label="A8XX TEX_SAMP"]
	}

	subgraph cluster_gpu {
		label="Hardware"
		fontcolor="#ff7b72" fontsize=14
		bgcolor="#161b22" color="#ff7b72"
		node [fillcolor="#3a1a2a" color="#ff7b72" fontcolor="#ffd2cc"]
		adreno_gpu [label="Adreno 8xx GPU"]
	}

	vk_api -> cmd_buffer [color="#3fb950" penwidth=2]
	vk_api -> pipeline [color="#3fb950" penwidth=2]
	vk_api -> clear_blit [color="#3fb950" penwidth=2]
	vk_api -> device [color="#3fb950" penwidth=2]

	cmd_buffer -> cmd_stream [color="#3fb950" penwidth=2]
	cmd_buffer -> render_pass [color="#3fb950"]
	cmd_buffer -> tile_config [color="#3fb950"]
	cmd_buffer -> clear_blit [color="#3fb950"]
	cmd_stream -> suballoc [color="#3fb950"]
	cmd_stream -> kgsl [color="#a371f7" penwidth=2]

	pipeline -> shader [color="#3fb950" penwidth=2]
	pipeline -> descriptor [color="#3fb950"]
	pipeline -> sampler [color="#3fb950"]

	clear_blit -> descriptor [color="#3fb950"]
	clear_blit -> render_pass [color="#3fb950"]

	device -> kgsl [color="#a371f7" penwidth=2]
	device -> queue [color="#3fb950"]
	queue -> kgsl [color="#a371f7"]
	kgsl -> kgsl_drv [color="#a371f7" penwidth=3]
	kgsl_drv -> adreno_gpu [color="#ff7b72" penwidth=3]

	cmd_buffer -> g_marker [color="#ff7b72" style=dashed]
	cmd_buffer -> g_byteaddr [color="#ff7b72" style=dashed]
	cmd_buffer -> g_tess [color="#ff7b72" style=dashed]
	cmd_buffer -> g_binpass [color="#ff7b72" style=dashed]
	pipeline -> g_depth [color="#ff7b72" style=dashed]
	pipeline -> g_combiner [color="#ff7b72" style=dashed]
	clear_blit -> g_samp [color="#ff7b72" style=dashed]
	clear_blit -> g_memobj [color="#ff7b72" style=dashed]
	descriptor -> g_memobj [color="#ff7b72" style=dashed]
	sampler -> g_samp [color="#ff7b72" style=dashed]
	shader -> g_byteaddr [color="#ff7b72" style=dashed]

	g_marker -> adreno_gpu [color="#ff7b72" style=dashed]
	g_byteaddr -> adreno_gpu [color="#ff7b72" style=dashed]
	g_samp -> adreno_gpu [color="#ff7b72" style=dashed]
	g_memobj -> adreno_gpu [color="#ff7b72" style=dashed]
	g_combiner -> adreno_gpu [color="#ff7b72" style=dashed]
}
`
}

func (d *DotGen) neuronPathsDOT(ig *IndexGraph) string {
	var sb strings.Builder
	sb.WriteString(`digraph NeuronPaths {
	rankdir=LR
	bgcolor="#0d1117"
	fontname="monospace"
	label="Gen8 Neuron Paths\nEntry Points to Gen8 Terminal Code Sites"
	fontsize=18 fontcolor="#ff7b72"
	labelloc="t"
	nodesep=0.3 ranksep=0.5

	node [fontname="monospace" fontsize=8 shape=box style="filled" penwidth=1]

`)

	maxNodes := 150
	count := 0

	for _, gn := range ig.Gen8Nodes {
		if count >= maxNodes {
			break
		}
		fn := ig.Functions[gn.Function]
		if fn == nil {
			continue
		}

		gnID := sanitize("gn_" + gn.ID)
		label := truncate(gn.Content, 45)
		fill := "#3a1a1a"
		if fn.IsEntry {
			fill = "#1f2937"
		}
		sb.WriteString(fmt.Sprintf(`	%s [label="%s" fillcolor="%s" color="%s" fontcolor="%s" fontsize=7]`+"\n",
			gnID, label, fill, "#ff7b72", "#ffd2cc"))

		funcID := sanitize("fn_" + gn.Function)
		sb.WriteString(fmt.Sprintf(`	%s [label="%s()" fillcolor="#1a2e1a" color="#3fb950" fontcolor="#d2f8d2" fontsize=7]`+"\n",
			funcID, truncate(gn.Function, 35)))

		sb.WriteString(fmt.Sprintf(`	%s -> %s [color="#ff7b72" style=bold]`+"\n", funcID, gnID))

		for _, caller := range findCallers(ig, gn.Function) {
			callerID := sanitize("fn_" + caller)
			cfn := ig.Functions[caller]
			callerFill := "#1a2e1a"
			callerBorder := "#3fb950"
			callerColor := "#d2f8d2"
			if cfn != nil && cfn.IsEntry {
				callerFill = "#1f2937"
				callerBorder = "#58a6ff"
				callerColor = "#e6edf3"
			}
			sb.WriteString(fmt.Sprintf(`	%s [label="%s()" fillcolor="%s" color="%s" fontcolor="%s" fontsize=7]`+"\n",
				callerID, truncate(caller, 35), callerFill, callerBorder, callerColor))
			sb.WriteString(fmt.Sprintf(`	%s -> %s [color="#3fb950" arrowsize=0.7]`+"\n", callerID, funcID))
		}
		count++
	}

	sb.WriteString("}\n")
	return sb.String()
}

func (d *DotGen) focusedCallGraphDOT(ig *IndexGraph) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`digraph FocusedCallGraph {
	rankdir=TB
	bgcolor="#0d1117"
	fontname="monospace"
	label="Gen8-Involved Call Graph (%d functions, %d gen8 nodes)"
	fontsize=18 fontcolor="#e6edf3"
	labelloc="t"
	nodesep=0.3 ranksep=0.5

	node [fontname="monospace" fontsize=8 shape=box style="filled" penwidth=1]

`, len(ig.Functions), len(ig.Gen8Nodes)))

	gen8Funcs := make(map[string]bool)
	for _, gn := range ig.Gen8Nodes {
		gen8Funcs[gn.Function] = true
	}
	for fnName := range gen8Funcs {
		for _, caller := range findCallers(ig, fnName) {
			gen8Funcs[caller] = true
		}
	}

	maxEdges := 1000
	edgeCount := 0

	for fnName := range gen8Funcs {
		fn := ig.Functions[fnName]
		fillColor := "#1a2e1a"
		borderColor := "#3fb950"
		fontColor := "#d2f8d2"
		if fn == nil {
			continue
		}
		if fn.Gen8Site {
			fillColor = "#3a1a1a"
			borderColor = "#ff7b72"
			fontColor = "#ffd2cc"
		}
		if fn.IsEntry {
			borderColor = "#58a6ff"
			if !fn.Gen8Site {
				fillColor = "#1f2937"
				fontColor = "#e6edf3"
			}
		}

		fnID := sanitize("f_" + fnName)
		sb.WriteString(fmt.Sprintf(`	%s [label="%s" fillcolor="%s" color="%s" fontcolor="%s" fontsize=8]`+"\n",
			fnID, truncate(fnName, 30), fillColor, borderColor, fontColor))

		for _, callee := range fn.Callees {
			if edgeCount >= maxEdges {
				break
			}
			if !gen8Funcs[callee] {
				continue
			}
			color := "#30363d"
			style := ""
			if fn.Gen8Site || (ig.Functions[callee] != nil && ig.Functions[callee].Gen8Site) {
				color = "#ff7b72"
				style = ` style=bold`
			}
			sb.WriteString(fmt.Sprintf(`	%s -> %s [color="%s"%s arrowsize=0.6]`+"\n",
				fnID, sanitize("f_"+callee), color, style))
			edgeCount++
		}
	}

	if edgeCount >= maxEdges {
		sb.WriteString(fmt.Sprintf(`	note_max [label="... %d+ more edges" shape=note fillcolor="#3a1a1a" color="#ff7b72" fontcolor="#ffd2cc" fontsize=7]`+"\n", edgeCount-maxEdges))
	}

	sb.WriteString("}\n")
	return sb.String()
}

func findCallers(ig *IndexGraph, funcName string) []string {
	seen := make(map[string]bool)
	var callers []string
	for caller, callees := range ig.Edges {
		for _, callee := range callees {
			if callee == funcName {
				if !seen[caller] {
					seen[caller] = true
					callers = append(callers, caller)
				}
				break
			}
		}
	}
	return callers
}

func sanitize(s string) string {
	s = strings.ReplaceAll(s, ":", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, ".", "_")
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, "<", "_")
	s = strings.ReplaceAll(s, ">", "_")
	s = strings.ReplaceAll(s, "(", "_")
	s = strings.ReplaceAll(s, ")", "_")
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "|", "_")
	s = strings.ReplaceAll(s, "#", "_")
	s = strings.ReplaceAll(s, "*", "_")
	for len(s) > 1 && s[0] == '_' {
		s = s[1:]
	}
	return s
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
