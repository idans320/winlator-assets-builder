# The Mapper: Static Analysis → PM4 Data Flow Tracing

How we go from Mesa Turnip C++ source code to knowing which functions emit
PKT4 register writes and PKT7 command packets — and trace those paths back
to Vulkan entry points.

## Architecture

The mapper has 3 layers stacked on top of each other:

```
  Layer 1: Function Discovery    (regex → call graph)
  Layer 2: Gen8 Site Discovery   (regex → gen8 terminal nodes)
  Layer 3: Path Tracing          (graph walk → entry → gen8 chains)

  Runtime: PM4 Decoder           (dword stream → PKT4/PKT7 packets)
```

## Layer 1: Function Discovery

**Input:** Raw Turnip C++ source files (tu_cmd_buffer.cc, tu_pipeline.cc, etc.)

**How it works:**

The first pass uses `funcDefRe` (a regex in `internal/ast/parser.go`) to find every
function definition in the source. It handles the C++ template syntax that Turnip
uses heavily:

```regex
funcDefRe = ^(static|inline|constexpr|virtual|explicit|template<...>)*
            ((?:[\w:*]+\s+)+?)     # return type
            ([\w:~]+)              # function name (capture group 2)
            \(([^)]*)\)            # parameters
            (?:const)? (?:override)? (?:noexcept)? \{
```

Then inside each function body, `funcCallRe = \b([\w:]+)\s*\(` extracts every
function call. Keywords (`if`, `for`, `sizeof`), casts (`static_cast`), and
utility macros (`MIN2`, `assert`, `memset`) are filtered out.

**Output:** A `CallGraph` with:
- `Functions[name]FuncDef` — every function with its file, line, callee list
- `Edges[caller][]callee` — directed call graph
- `EntryPoints[]` — functions matching Vulkan API prefixes (`vkCmd*`, `vkCreate*`,
  `tu_Cmd*`, `tu_Create*`, etc.)

**Example:** Inside `tu_CmdDraw`'s body, the regex finds calls to:
```
tu6_lazy_init_vsc
tu6_tile_render_begin
tu_emit_cache_flush_renderpass
tu_cs_emit_pkt4        ← this is a PKT4 emitter
tu_cs_emit_pkt7        ← this is a PKT7 emitter
```

These callee names are stored on `tu_CmdDraw.Callees[]`. The edges `tu_CmdDraw →
tu_cs_emit_pkt4` and `tu_CmdDraw → tu_cs_emit_pkt7` are added to `CallGraph.Edges`.

### Two-Pass Merge for Cross-File Calls

Turnip functions are split across multiple .cc files. The analyzer runs in two passes:

1. **Pass 1:** Parse every file independently, building per-file call graphs
2. **Merge:** Combine all per-file graphs into one `CallGraph` via `Merge()` — unions
   functions, edges, gen8 nodes, and entry points
3. **Pass 2:** Parse each file again, but this time with `knownCallees` — a map of
   function names discovered in other files. If function A in file X calls function B
   that was defined in file Y, the call graph gets the cross-file edge.

This avoids the need for a full C++ preprocessor or header resolver.

## Layer 2: Gen8 Site Discovery

**Input:** Function bodies from Layer 1

**How it works:**

Three regex patterns are applied to each function body:

| Pattern | Name | What it finds |
|---------|------|---------------|
| `\b(CHIP\s*[<>!=]+\s*A8XX\|A8XX_\w+\|gen8\b\|a8xx)` | gen8PatternRe | Any Adreno 8xx reference |
| `if\s*\(.*(?:CHIP\s*[<>!=]+\s*A8XX\|A8XX).*\)` | gen8ConditionalRe | `if (CHIP >= A8XX)` branches |
| `(?:A8XX_\w+\|a8xx_\w+)\s*\|=\s*\|tu_cs_emit.*A8XX` | gen8RegWriteRe | A8XX register writes |

Each match becomes a `Gen8Node` with:
- **File/Line:** where in the source the match occurs
- **Kind:** `gen8-conditional`, `gen8-register-write`, or `gen8-reference`
- **Content:** 80-char snippet of surrounding source code
- **Regs:** A8XX register names extracted by `\bA8XX_\w+` from the full function body (not just the hit location)

**Example:** In `tu6_emit_binning_pass`, the scanner finds:

```cpp
// gen8PatternRe hits:
"CHIP >= A8XX", "A8XX", "gen8"

// gen8ConditionalRe matches:
"if (CHIP >= A8XX) { ... A8XX_CP_SET_MARKER_0_MODE(...) ... }"

// gen8RegWriteRe matches:
"A8XX_CP_SET_MARKER_0_MODE"
"A8XX_CP_SET_MARKER_0_USES_GMEM"
```

This creates a `Gen8Node`:
```json
{
  "id": "tu_cmd_buffer.cc:tu6_emit_binning_pass:g8:2643",
  "function": "tu6_emit_binning_pass",
  "kind": "gen8-register-write",
  "content": "if (CHIP >= A8XX) { tu_cs_emit(cs, A8XX_CP_SET_MARKER_0_MODE(...",
  "regs": ["A8XX_CP_SET_MARKER_0_MODE", "A8XX_CP_SET_MARKER_0_USES_GMEM"],
  "line": 2643
}
```

**Scale:** The analysis found **3,550** gen8 code sites across 10 .cc files and 8
header files, inside **475** gen8-involved functions.

## Layer 3: Path Tracing (Neuron Walk)

**Input:** CallGraph + Gen8Nodes from Layers 1 & 2

**How it works:**

`TraceBackFromGen8()` treats each Gen8Node as a **terminal neuron**. It walks
**up** the call graph to find the chain from Vulkan entry point → ... →
gen8 code site.

The algorithm:

```go
// For each Gen8Node (up to 150, to keep graph renderable):
for _, gn := range callGraph.Gen8Nodes {
    // Create graph node for the gen8 site itself
    // Create graph node for the enclosing function
    // Link: function → gen8 site

    // Walk up the call graph from this function
    traceUp(callGraph, tracedGraph, gn.Function, depth=0, maxDepth=3)
}

// traceUp recursively finds callers:
func traceUp(cg, tg, funcName, depth, maxDepth, visited) {
    if depth >= maxDepth || visited[funcName]:
        return
    for each caller of funcName:   // via findCallers()
        create node for caller
        link: caller → funcName
        if caller is an entry point:
            link: entry → caller
        recurse: traceUp(caller, depth+1)
}
```

`findCallers()` does the reverse edge lookup:

```go
func findCallers(cg *CallGraph, funcName string) []string {
    for caller, callees := range cg.Edges {
        for _, callee := range callees {
            if callee == funcName {
                // caller calls funcName
                callers = append(callers, caller)
            }
        }
    }
}
```

**Example trace** from `A8XX_CP_SET_MARKER` gen8 site back to entry:

```
Vulkan API
  └─ vkCmdDraw
       └─ tu_CmdDraw
            └─ tu_cmd_render_sysmem
                 └─ tu6_tile_render_begin
                      └─ tu6_emit_binning_pass
                           └─ A8XX_CP_SET_MARKER_0_MODE (gen8 terminal)
```

## How we know which functions emit PKT4 vs PKT7

The classification uses **name-based heuristics** on the callees found in Layer 1:

```go
// PKT4 emitters — functions whose names suggest register programming
func isRegisterEmitter(name string) bool {
    return strings.Contains(name, "emit_") ||     // tu6_emit_bin_size, tu7_emit_fs_params
           strings.Contains(name, "tu_cs_") ||     // tu_cs_emit_pkt4, tu_cs_emit_write_reg
           strings.Contains(name, "_write_") ||    // tu_cs_emit_write_reg
           strings.HasPrefix(name, "tu6_") ||      // tu6_emit_zs, tu6_emit_mrt
           strings.HasPrefix(name, "tu7_")         // tu7_emit_tile_render_begin_regs
}

// PKT7/barrier emitters — functions involved in cache flushes and barriers
func isBarrierFunction(name string) bool {
    return strings.Contains(name, "cache_flush") ||  // tu_emit_cache_flush
           strings.Contains(name, "barrier") ||      // tu_CmdPipelineBarrier2
           strings.Contains(name, "flush_") ||       // tu6_emit_flushes
           strings.Contains(name, "wfi")             // tu_cs_emit_wfi
}
```

The **hotness** of each function is scored from:
- Number of gen8 sites in the function body (`len(fn.Gen8Lines) * 10`)
- Number of A8XX register references (`a8xxCount * 5`)
- Number of callers (`countCallers(cg, name) * 2`)

The top register emitters (from actual analysis run):

| Function | Reg Writes | A8XX Writes | Hot Score |
|----------|-----------|-------------|-----------|
| tu6_emit_blit_consts_load | 18 | 144 | 1128 |
| tu7_set_thread_br_patchpoint | 21 | 0 | 726 |
| tu6_emit_blit_scissor | 15 | 0 | 690 |
| tu6_lazy_init_vsc | 24 | 48 | 626 |
| tu_emit_vis_stream_patchpoint | 24 | 48 | 626 |

## Runtime: PM4 Decoder

The static analysis tells us **where** PM4 is emitted. The runtime decoder tells us
**what** flows through. `internal/pm4/decoder.go` parses raw dword streams:

```go
func Decode(dwords []uint32) ([]PM4Packet, *DecodeStats) {
    for each 32-bit header in the dword stream:
        if header & 0xF0000000 == 0x40000000:  // Type4 → PKT4 register write
            reg = (header >> 8) & 0x7FFFF
            cnt = header & 0x7F
            // next cnt dwords are register values

        if header & 0xF0000000 == 0x70000000:  // Type7 → PKT7 command
            opcode = (header >> 16) & 0x7F
            cnt = header & 0x3FFF
            // next cnt dwords are command payload
}
```

Each register write is classified:
- `IsA8XXRegister[reg]` — from the generated register map (1351 gen8-specific registers)
- `RegNameByOffset[reg]` — human-readable name (3666 entries)
- `BarrierOpcodes[opcode]` — is this a barrier packet?
- `DrawOpcodes[opcode]` — is this a draw packet?

The decoder includes odd-parity checking (`pm4_calc_odd_parity_bit`) to validate
packets — matching exactly what the Adreno CP hardware validates.

## Data Flow Diagram

```
  Mesa C++ Source                    Go Analysis Pipeline
  ────────────────                  ────────────────────
  tu_cmd_buffer.cc  ──funcDefRe──→  Layer 1: CallGraph
  tu_pipeline.cc                    {Functions, Edges, EntryPoints}
  tu_clear_blit.cc         │
  tu_shader.cc             │
  ...                      │
                           ├─gen8PatternRe──→  Layer 2: Gen8Nodes
                           │                    {3550 code sites,
                           │                     475 functions,
                           │                     A8XX register refs}
                           │
                           ├─TraceBack──→  Layer 3: TracedPaths
                           │               {entry → gen8 chains,
                           │                PKT4/PKT7 classification}
                           │
  ┌────────────────────────┘
  │
  ▼
  Graphviz DOT Generator → SVG diagrams
    • 01_turnip_gen8_architecture  —  layers + gen8 feature overlay
    • 02_gen8_neuron_paths         —  entry → terminal trace chains
    • 03_call_graph_focused        —  gen8-involved function graph

  ────────────────────
  Runtime PM4 Stream
  ────────────────────
  Raw dwords from       │
  driver / stim test    │
                        ├─Decode()──→  PM4Packet[]
                        │              {PKT4: reg+vals,
                        │               PKT7: opcode+payload,
                        │               IB: indirect buffer}
                        │
                        ├─Stats──→  DecodeStats
                        │           {PKT4 count, PKT7 count,
                        │            barrier count, draw count,
                        │            A8XX reg write count}
                        │
                        └─Engine──→ SubmitResult
                                    {redundant writes skipped,
                                     barriers bypassed,
                                     fences signaled}
```

## Limitations

1. **Regex parsing cannot fully resolve C++** — The old regex-based parser
   (`funcDefRe`, `findClosingBrace`) produced ~3,550 fuzzy "gen8 sites" with
   false positives and missed sites. Inline functions in headers and template
   instantiations are not reliably traced. The new pipeline uses native tools
   (ctags, ripgrep, tree-sitter) to build accurate call graphs from real parse
   trees, not regex heuristics.

2. **No actual PM4 data from static analysis** — The static analysis tells us
   which functions MAY emit PM4, but the actual register values and opcode
   sequences come only from real hardware traces. The new pipeline replaces
   synthetic workload generation with `TU_DEBUG=trace` captures from physical
   Adreno 8xx devices.
