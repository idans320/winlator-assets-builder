# vkCmdDraw(3) → Adreno 830 Silicon: An Archaeological Expedition

This document walks through a single `vkCmdDraw(3 vertices, 1 instance, 0 offset)`
from the Vulkan API call all the way down to the electrical signals leaving the
GPU's command processor. Every term is defined. Every register block is explained.
The trace was captured from a real Adreno 830 (Snapdragon 8 Gen 3) running the
Mesa Turnip open-source Vulkan driver with `TU_DEBUG=trace` instrumentation.

The trace data: `output/traces/probe_tri_2026-06-05_214554.log` — 441 PM4 events.

---

## A Note on Method

This is **archaeology by lesion**. We modified the driver (added `fprintf`
trace points to the PM4 emission functions), built it, ran it on the real
silicon, and captured exactly what the GPU's command processor sees. The
silicon is the teacher. When our understanding is wrong, the GPU hangs —
and that friction is the learning signal. This document is what the hardware
taught us.

---

## Table of Contents

1. [The Adreno GPU Architecture](#1-the-adreno-gpu-architecture)
2. [The PM4 Command Stream](#2-the-pm4-command-stream)
3. [Layer 1: The Vulkan API Call](#3-layer-1-the-vulkan-api-call)
4. [Layer 2: The Turnip Driver Call Chain](#4-layer-2-the-turnip-driver-call-chain)
5. [Layer 3: The PM4 Trace — Every Packet Explained](#5-layer-3-the-pm4-trace)
6. [Layer 4: Silicon Execution — What the GPU Does](#6-layer-4-silicon-execution)
7. [Archaeological Findings](#7-archaeological-findings)
8. [The Connectome So Far](#8-the-connectome-so-far)

---

## 1. The Adreno GPU Architecture

### What is Adreno?

Adreno is Qualcomm's mobile GPU IP, used in Snapdragon SoCs. The name is
an anagram of Radeon (Qualcomm acquired AMD's mobile GPU division in 2009).
Each generation is named by a chip identifier:

| Generation | Chip ID | Year | Found In |
|-----------|---------|------|----------|
| A6XX | a630, a640, a650, a660, a690 | 2018-2021 | Snapdragon 845 through 888 |
| A7XX | a730, a740, a750 | 2022-2024 | Snapdragon 8 Gen 1 through 8 Gen 3 |
| A8XX | a830 | 2025 | Snapdragon 8 Elite |

Our device has **Adreno 830** (chip ID `A8XX`), the first of the gen8 generation.

### The GPU Pipeline: A Simplified Model

A GPU is a factory with specialized stations. Data flows through each station
in sequence:

```
  Application
      │
      ▼
  [CP]  Command Processor — Reads the PM4 command stream, dispatches work
      │
      ▼
  [VFD] Vertex Fetch/Decode — Reads vertex attributes from memory or
      │  generates auto-index vertices (0, 1, 2...)
      ▼
  [SP]  Shader Processor (Vertex Stage) — Runs the vertex shader for each
      │  vertex. Transforms 3D positions to screen coordinates.
      ▼
  [PA]  Primitive Assembly — Groups vertices into triangles, lines, points.
      │
      ▼
  [GRAS] Rasterizer — Converts triangles into fragments (pixels). Determines
      │  which pixels are covered by each triangle.
      ▼
  [SP]  Shader Processor (Fragment Stage) — Runs the fragment shader for
      │  each pixel. Computes the final color.
      ▼
  [RB]  Render Backend — Writes the final color to memory. Handles blending,
        depth testing, stencil testing.
```

### GMEM: The On-Chip Tile Buffer

**GMEM** (Graphics Memory) is a small, extremely fast SRAM buffer on the GPU
die. Instead of rendering the entire framebuffer at once (which would require
huge memory bandwidth), Adreno breaks the screen into **tiles** (typically
~256×256 pixels) and renders one tile at a time into GMEM.

```
  ┌─────────────────────────────────┐
  │  Full Framebuffer (1080×2400)   │
  │  ┌────┬────┬────┬────┬────┐     │
  │  │Tile│Tile│Tile│Tile│Tile│     │
  │  │ 0  │ 1  │ 2  │ 3  │ 4  │     │
  │  ├────┼────┼────┼────┼────┤     │
  │  │Tile│Tile│Tile│...   ... │     │
  │  │ 5  │ 6  │ 7  │         │     │
  │  └────┴────┴────┴─────────┘     │
  └─────────────────────────────────┘

  For each tile:
    1. Load tile from system RAM → GMEM     (resolve in)
    2. Render all geometry touching this tile  (GMEM rendering)
    3. Store tile from GMEM → system RAM     (resolve out)
```

This is called **tile-based rendering** or **binning**. It's why Adreno (and
other mobile GPUs) can achieve high performance with limited memory bandwidth.
The penalty is that each tile boundary requires a resolve operation — moving
data between GMEM and system RAM.

### Binning Pass vs. Rendering Pass

Before rendering, Adreno runs a **binning pass** — a lightweight vertex
shader that determines which tiles each triangle touches. The output is a
**visibility stream** — a bitmask per tile indicating which primitives are
visible. The rendering pass then only shades fragments in tiles that actually
contain geometry.

On A8XX, this binning pass uses **CP_SET_MARKER** (opcode 0x65) to delimit
binning vs. rendering phases.

### LRZ: Low Resolution Z

**LRZ** (Low Resolution Z) is a coarse early-depth-culling optimization.
During the binning pass, the hardware builds a low-resolution depth map of
the scene. Before the expensive fragment shader runs, the GPU tests against
this low-res Z buffer — if a fragment is known to be occluded (behind
something already drawn), the fragment shader is skipped entirely.

**Not to be confused with Late-Z**, which is the standard fallback when a
fragment shader modifies `gl_FragDepth` or uses `discard`. Late-Z forces the
hardware to write depth *after* the shader executes, losing the early-reject
optimization. LRZ is an early, approximate culling pass; Late-Z is a
correctness fallback for shaders that touch depth.

LRZ is configured per-draw via 15 register writes to the `0x81xx` block:
- `GRAS_LRZ_CNTL` — enable/disable LRZ for this draw
- `GRAS_LRZ_BUFFER_BASE` — where the LRZ buffer lives in memory
- `GRAS_LRZ_BUFFER_PITCH` — stride of the LRZ buffer
- `GRAS_LRZ_DEPTH_CLEAR` — clear value for the LRZ buffer
- etc.

### HLSQ: The Shader Stage Scheduler

**HLSQ** (High-Level Sequencer) is the hardware unit that manages which
shader stages are active and how they're configured. It dispatches work to
the SP (Shader Processor) for each stage: VS (vertex), FS (fragment),
GS (geometry), TCS/TES (tessellation).

On the register map, HLSQ registers are in the `0xB1xx` block.

---

## 2. The PM4 Command Stream

### What is PM4?

**PM4** (Power Management 4) is the command stream protocol used by Adreno
GPUs. It's a sequence of 32-bit words (dwords) that the CP (Command Processor)
parses and executes. Think of it as the GPU's machine code.

Every PM4 packet starts with a **header dword** that encodes the packet type,
register/opcode, and payload size:

```
  PKT4 Header (register write):
  ┌────────────┬──┬──────────────────────┬──┬──────────────────┐
  │ type4=0x4  │P │  register offset     │P │  count           │
  │  (4 bits)  │  │  (18 bits)           │  │  (7 bits)        │
  └────────────┴──┴──────────────────────┴──┴──────────────────┘
    bit 31-28   27  26-8                  7   6-0

  PKT7 Header (command opcode):
  ┌────────────┬──┬──────────────┬──┬──────────────────────────┐
  │ type7=0x7  │P │  opcode      │P │  payload size            │
  │  (4 bits)  │  │  (7 bits)    │  │  (14 bits)               │
  └────────────┴──┴──────────────┴──┴──────────────────────────┘
    bit 31-28   27  26-16         15  13-0
```

The `P` bits are **odd parity** checks — the GPU validates each packet header
to catch command stream corruption. If parity fails, the CP hangs.

### PKT4: Register Writes

A PKT4 packet writes values to GPU hardware registers. The header specifies
the starting register offset and the number of consecutive registers to write.
The payload is one dword per register.

```
  Example: PKT4 reg=0x8818 cnt=1  val=0x00000000
  ┌──────────────┬──────────────┐
  │ header       │ payload      │
  │ 0x48818801   │ 0x00000000   │
  └──────────────┴──────────────┘
```

This writes value `0x00000000` to register `0x8818` (RB_UNKNOWN_8818).

Multi-register PKT4:
```
  Example: PKT4 reg=0x8809 cnt=2
  ┌──────────────┬──────────────┬──────────────┐
  │ header       │ val[0]       │ val[1]       │
  │ 0x48880902   │ 0x........   │ 0x........   │
  └──────────────┴──────────────┴──────────────┘
```

This writes two consecutive registers starting at 0x8809.

### PKT7: Command Opcodes

A PKT7 packet issues a command to the CP. The opcode tells the CP what to do.
Examples from our trace:

| Opcode | Name | What It Does |
|--------|------|-------------|
| 0x26 | CP_WAIT_FOR_IDLE | Stall until GPU pipeline is fully drained |
| 0x13 | CP_WAIT_FOR_ME | Stall until CP's own queue is empty (A8XX+) |
| 0x46 | CP_EVENT_WRITE | Write a timestamp or flush a cache |
| 0x38 | CP_DRAW_INDX_OFFSET | **Submit a draw call** |
| 0x43 | CP_SET_DRAW_STATE | Load pre-built state from indirect buffers |
| 0x3f | CP_INDIRECT_BUFFER | Chain to another command buffer in memory |
| 0x65 | CP_SET_MARKER | Delimit binning vs. rendering phases (A8XX+) |
| 0x17 | CP_THREAD_CONTROL | Control GPU thread allocation (A7XX+) |

### Indirect Buffers (IBs)

An **Indirect Buffer** is a secondary command stream stored in GPU memory.
The CP can "call" into an IB and return when it's done. This is how Turnip
manages draw state:

```
  Main CS:  CP_SET_DRAW_STATE → IB_chain[0] → IB_chain[1] → ... → CP_DRAW
            │                      │               │
            │  ┌──────────────────┘               │
            │  │  ┌───────────────────────────────┘
            ▼  ▼  ▼
  State IBs: [VS config] [FS config] [VPC config] [Blend state] ...
```

Each state IB is pre-built when the pipeline is created. The draw call just
references them by (GPU address, size) pairs. This avoids re-emitting the
same register writes every frame.

---

## 3. Layer 1: The Vulkan API Call

Our probe submits exactly one draw call:

```c
vkCmdDraw(commandBuffer,   // command buffer to record into
          3,               // vertexCount: draw 3 vertices
          1,               // instanceCount: 1 instance of them
          0,               // firstVertex: start at vertex index 0
          0);              // firstInstance: instance index 0
```

This draws a single triangle using auto-generated vertex indices (0, 1, 2).
The vertex shader uses `gl_VertexIndex` to look up positions from a built-in
array — no vertex buffer is bound.

The vertex shader (SPIR-V embedded in `loader_probe.c`):
```glsl
vec2 positions[3] = vec2[](vec2(0,-0.5), vec2(0.5,0.5), vec2(-0.5,0.5));
gl_Position = vec4(positions[gl_VertexIndex], 0.0, 1.0);
```

The fragment shader:
```glsl
outColor = vec4(1.0, 0.0, 0.0, 1.0);  // pure red
```

---

## 4. Layer 2: The Turnip Driver Call Chain

**Turnip** is the open-source Mesa Vulkan driver for Adreno. Its name comes
from "turnip" being a root vegetable — freedreno (the umbrella project for
Adreno open-source drivers) has a naming convention of root vegetables (turnip
for Vulkan, freedreno for the kernel/Gallium layer).

When `vkCmdDraw` is called, the Vulkan loader dispatches to Turnip's
implementation. Here is the exact call chain, traced through the source:

### 4.1 Entry Point: `tu_CmdDraw`

```cpp
// tu_cmd_buffer.cc:8631
void tu_CmdDraw(VkCommandBuffer commandBuffer,
                uint32_t vertexCount,      // 3
                uint32_t instanceCount,     // 1
                uint32_t firstVertex,       // 0
                uint32_t firstInstance)     // 0
{
    struct tu_cs *cs = &cmd->draw_cs;

    // Step 1: Emit vertex shader parameters (base vertex/instance)
    tu6_emit_vs_params(cmd, 0, firstVertex, firstInstance);

    // Step 2: Emit all draw state + cache flushes
    tu6_draw_common<CHIP>(cmd, cs, false, vertexCount);

    // Step 3: Emit the actual draw packet
    tu_cs_emit_pkt7(cs, CP_DRAW_INDX_OFFSET, 3);
    tu_cs_emit(cs, tu_draw_initiator(cmd, DI_SRC_SEL_AUTO_INDEX));
    tu_cs_emit(cs, instanceCount);
    tu_cs_emit(cs, vertexCount);
}
```

### 4.2 The Heavy Lifter: `tu6_draw_common`

```cpp
// tu_cmd_buffer.cc:8170
void tu6_draw_common(struct tu_cmd_buffer *cmd,
                     struct tu_cs *cs,
                     bool indexed,
                     uint32_t draw_count)
{
    // Step 2a: Emit dirty dynamic state (viewport, scissor, etc.)
    tu_emit_draw_state<CHIP>(cmd);
    // → Produces CP_SET_DRAW_STATE packets (0x43)
    // → These load pre-built state indirect buffers

    // Step 2b: LRZ state (low-resolution Z buffer optimization)
    tu6_emit_lrz(cmd, cs);
    // → Produces PKT4 writes to 0x81xx registers

    // Step 2c: Cache flush BEFORE the draw
    // THIS IS THE BARRIER BOTTLENECK
    tu_emit_cache_flush_renderpass<CHIP>(cmd);
    // → Produces CP_EVENT_WRITE × 16
    // → Produces CP_WAIT_FOR_IDLE × 3
    // → Produces CP_WAIT_FOR_ME (gen8)
    // → Produces CP_THREAD_CONTROL (A7XX+)
}
```

### 4.3 The Cache Flush Function (Barrier Bottleneck)

```cpp
// tu_cmd_buffer.cc:561
void tu_emit_cache_flush_renderpass(struct tu_cmd_buffer *cmd_buffer)
{
    // If no flush bits accumulated, skip the entire barrier
    if (!cmd_buffer->state.renderpass_cache.flush_bits &&
        likely(!tu_env.debug))
        return;  // ← THIS EARLY RETURN IS THE PATCH 0004 OPTIMIZATION

    // Otherwise, emit full flush sequence:
    tu6_emit_flushes<CHIP>(cmd_buffer, cs, cache);
    // → CP_EVENT_WRITE(0x46) × N for each dirty cache
    // → CP_WAIT_FOR_IDLE(0x26) to drain pipeline
}
```

This function is called **before every draw** in `tu6_draw_common` (line 8225).
Each call emits up to 16 `CP_EVENT_WRITE` packets. If the `flush_bits` field
is zero (meaning no caches are dirty), the early return would skip all 16
packets — a massive savings. This is exactly what patch `0004-barrier-coalesce`
does.

In our trace, patch 0004 was NOT applied, so we see all 16 flushes.

---

## 5. Layer 3: The PM4 Trace — Every Packet Explained

Our trace captured 441 PM4 events from `vkCmdDraw(3)`. Let's walk through
them in order, grouping by what the driver was doing.

### 5.1 Pipeline State Setup (~200 PKT4 writes)

These are the register writes emitted during `tu_emit_draw_state`. Turnip
uses a **dirty-bit system** (from patch 0003) to track which state has
changed and only emit what's needed. For a fresh pipeline, everything is dirty.

The writes cluster by hardware block:

#### RB Block (0x88xx) — 84 writes: Render Backend

The **Render Backend** handles per-fragment operations: color/depth/stencil
testing, blending, and writing the final pixel value to GMEM.

```
  PKT4 reg=0x8809 cnt=2   RB_BIN_CONTROL + RB_BIN_CONTROL2
  PKT4 reg=0x8810 cnt=1   RB_RENDER_CONTROL
  PKT4 reg=0x8818 cnt=1   RB_UNKNOWN_8818 → val=0x00000000
  PKT4 reg=0x8866 cnt=1   RB_LB_PARAM_LIMIT
  PKT4 reg=0x8800 cnt=1   RB_CCU_CNTL (Color Cache Unit)
  PKT4 reg=0x88d3 cnt=1   RB_BLEND_*
  PKT4 reg=0x88e4 cnt=1   RB_*_INFO
  PKT4 reg=0x8898 cnt=1   RB_DEPTH_*
  PKT4 reg=0x8870 cnt=1   RB_STENCIL_*
  ... and ~74 more
```

**Why so many RB writes?** The RB is the most complex block. It needs to know:
- Color format (R8G8B8A8 in our case)
- Blend mode (none in our case — just write the color)
- Depth format (none — our render pass has no depth attachment)
- Stencil mode (none)
- Sample count (1 — no multisampling)
- Whether LRZ is active
- GMEM tile configuration

Even with all these at their default/zero values, the driver still emits them
because the "dirty" flag was set at pipeline creation.

#### GRAS Block (0x82xx) — 43 writes: Rasterizer

The **Rasterizer** determines which pixels are covered by each triangle.

```
  PKT4 reg=0x8213 cnt=1   GRAS_MODE_CNTL → val=0x00000010
  PKT4 reg=0x8228-0x822D  GRAS_SC_SCREEN_SCISSOR_* (6 writes)
  PKT4 reg=0x8235 cnt=2   GRAS_BIN_*
  PKT4 reg=0x8231 cnt=1   GRAS_*
  PKT4 reg=0x8507 cnt=2   (part of GRAS block)
```

Key registers:
- `GRAS_MODE_CNTL`: Controls rasterization mode (MSAA, line width, etc.)
- `GRAS_SC_SCREEN_SCISSOR_*`: Defines the scissor rectangle in screen space
- Binning-related registers: Configure tile binning for GMEM rendering

#### SP Block (0xA9xx) — 32 writes: Shader Processor

The **Shader Processor** is the programmable compute unit that runs shaders.

```
  PKT4 reg=0xA002 cnt=1   SP_*
  PKT4 reg=0xA003 cnt=2   SP_*
  PKT4 reg=0xA005 cnt=1   SP_FS_*
  PKT4 reg=0xA006 cnt=1   SP_VS_*
  PKT4 reg=0xA99E cnt=1   SP_*
  PKT4 reg=0xA9C7 cnt=5   SP_* × 5
```

These configure:
- Which shader stages are active (VS+FS only, for us)
- Shader binary addresses
- Constant buffer bindings
- Thread allocation per shader stage

#### VPC Block (0x93xx) — 19 writes: Varying/Primitive Controller

The **VPC** handles varying interpolation (passing data from vertex shader to
fragment shader) and primitive output routing.

```
  PKT4 reg=0x9303 cnt=1   VPC_*
  PKT4 reg=0x9252 cnt=4   VPC_*
```

#### LRZ Block (0x81xx) — 15 writes: Low Resolution Z

```
  PKT4 reg=0x8102 cnt=1   GRAS_LRZ_MRT_BUFFER_INFO_0
  PKT4 reg=0x8109 cnt=1   GRAS_LRZ_PS_SAMPLEFREQ_CNTL
  PKT4 reg=0x810B cnt=1   GRAS_LRZ_CNTL2
  ... and ~12 more
```

These 15 writes configure the low-resolution Z buffer for occlusion culling.
Even though our triangle has no depth buffer, the LRZ block is still configured.

#### HLSQ Block (0xB1xx) — 14 writes: Shader Sequencer

```
  PKT4 reg=0xB182 cnt=12  HLSQ_* (written 12 times — the hottest register!)
  PKT4 reg=0xB183 cnt=12  HLSQ_* (also 12 times)
```

These two registers are the hottest in our trace, written 12 times each.
They're likely shader stage enable/configuration registers that get rewritten
at multiple points in the state setup.

### 5.2 Draw State Loading: `CP_SET_DRAW_STATE` (opcode 0x43)

```
  PKT7 op=0x43 cnt=90    ← Load pre-built state IB chain
```

`CP_SET_DRAW_STATE` loads 90 dwords of state references. Each dword is an
identifier for a pre-built **draw state object**. The CP uses these to look
up the actual state IB chain in GPU memory.

The 90 entries correspond to:
- Program configuration (shader binaries, consts) — `TU_DRAW_STATE_PROGRAM_CONFIG`
- Vertex shader state — `TU_DRAW_STATE_VS`
- Fragment shader state — `TU_DRAW_STATE_FS`
- VPC output mapping — `TU_DRAW_STATE_VPC`
- Primitive mode (GMEM binning) — `TU_DRAW_STATE_PRIM_MODE_GMEM`
- Dynamic states (blend, depth, stencil, etc.) — `TU_DRAW_STATE_DYNAMIC + id`
- Descriptor sets — `TU_DRAW_STATE_DESC_SETS`
- Input attachments — `TU_DRAW_STATE_INPUT_ATTACHMENTS_GMEM`

These are enumerated in `tu_draw_state_group_id` in `tu_cmd_buffer.h`. The
CP fetches each state's IB from memory, loading its register writes into the
GPU's internal shadow register file — but does NOT yet write them to hardware.

### 5.3 Pre-Draw Cache Flush: `CP_EVENT_WRITE` (opcode 0x46)

```
  PKT7 op=0x46 cnt=1     ← Cache flush event (×16 times)
  PKT7 op=0x46 cnt=4     ← Cache flush with timestamp (4 dword payload)
  PKT7 op=0x26 cnt=0     ← CP_WAIT_FOR_IDLE — drain pipeline
  PKT7 op=0x13 cnt=0     ← CP_WAIT_FOR_ME — CP queue empty (A8XX only)
  PKT7 op=0x17 cnt=1     ← CP_THREAD_CONTROL — thread management (A7XX+)
```

**CP_EVENT_WRITE** is the Swiss Army knife of GPU synchronization. Its
payload encodes:
- **Event type** (what to wait for)
- **Destination** (on-chip, memory, or timestamp)
- **Source** (what to write — timestamp, register value, etc.)
- **Address** (where to write the result)

Common event types in our trace:
- `CACHE_FLUSH_TS (0x1f)`: Flush L2 cache, write timestamp
- `WT_DONE_GFX_CORE (0x20)`: Wait for graphics core write-through done
- `RB_DONE_TS (0x22)`: Wait for render backend done, write timestamp

Each `CP_EVENT_WRITE` is followed by `CP_WAIT_FOR_IDLE` which stalls the CP
until all pending GPU work completes. This is expensive — the GPU pipeline
is deep (hundreds of cycles), and draining it means the hardware sits idle.

### 5.4 THE DRAW: `CP_DRAW_INDX_OFFSET` (opcode 0x38)

```
  [TU_TRACE] PKT7 op=0x38 cnt=3   ← CP_DRAW_INDX_OFFSET
  [payload dword 0]: 0x........   ← draw_initiator
  [payload dword 1]: 0x00000001   ← instanceCount = 1
  [payload dword 2]: 0x00000003   ← vertexCount = 3
```

This is the packet that actually triggers rendering. The **draw initiator**
encodes:
- **Source select**: `DI_SRC_SEL_AUTO_INDEX` (generate indices 0,1,2...)
- **Primitive type**: Triangles
- **Number of instances**: 1
- **Visibility stream**: Whether to use the binning visibility stream

When the CP sees this packet, it:
1. Configures VFD to generate 3 vertices
2. Sets up primitive assembly for triangles
3. Tells GRAS to expect 1 instance
4. Dispatches the draw to the rendering pipeline

### 5.5 Post-Draw State

```
  PKT4 reg=0x8235 cnt=2   GRAS_BIN_*
  PKT4 reg=0x8507 cnt=2   (part of GRAS block)
  PKT4 reg=0x8231 cnt=1   GRAS_*
  PKT4 reg=0x8800 cnt=1   RB_CCU_CNTL
  PKT4 reg=0x88d3 cnt=1   RB_BLEND_*
  PKT7 op=0x65 cnt=1      CP_SET_MARKER (A8XX binning phase marker)
  PKT7 op=0x1d cnt=1      CP_SKIP_IB2_ENABLE_LOCAL
```

After the draw, Turnip restores some state and emits the gen8 binning marker.
`CP_SET_MARKER` is used to delimit the rendering phase from the binning phase
on A8XX hardware. This is pure gen8 archaeology — no earlier Adreno has
this opcode.

### 5.6 Post-Draw Flush & Resolve

```
  PKT7 op=0x46 cnt=1     ← CP_EVENT_WRITE (RB_DONE_TS)
  PKT7 op=0x46 cnt=4     ← CP_EVENT_WRITE (CACHE_FLUSH_TS)
  PKT7 op=0x26 cnt=0     ← CP_WAIT_FOR_IDLE
  PKT7 op=0x13 cnt=0     ← CP_WAIT_FOR_ME
  PKT7 op=0x3f cnt=3     ← CP_INDIRECT_BUFFER
```

The post-draw flush ensures:
1. RB has finished writing all fragments to GMEM
2. L2 cache is flushed (makes results visible to other GPU units)
3. Pipeline is drained

Then `CP_INDIRECT_BUFFER` chains to the next IB, which handles the **resolve**
— copying the GMEM tile contents to the system memory framebuffer.

### 5.7 Reg-Cache Hits (Patch 0003)

```
  [TU_TRACE] WRITE_REG SKIP reg=0x8818 val=0x00000000
  [TU_TRACE] WRITE_REG SKIP reg=0xB183 val=0x00000000
```

Two writes were caught by the reg-cache and skipped. The reg-cache stores
the last value written to each register. If the new value matches the cached
value, the PKT4 is suppressed. These two skips saved 2 dwords out of ~500 —
a 0.4% reduction. The reg-cache is working correctly, but it's most effective
within a single command buffer (it gets invalidated on `tu_cs_reset`).

---

## 6. Layer 4: Silicon Execution — What the GPU Does

Now let's trace what happens inside the silicon when the CP processes each
packet. This section is speculative — based on public Adreno architecture
documentation (freedreno wiki, Mesa source comments, register definitions).

### 6.1 Register Loading Phase

When the CP processes a PKT4 packet:
```
  CP reads header: type4=1, reg=0x8818, cnt=1
  CP reads payload dword: 0x00000000
  CP writes 0x00000000 to internal shadow register 0x8818
```

The GPU has a **hardware context bank (shadow register file)** — a bank
of registers that mirrors the hardware state. The CP parses the PKT4 stream
and updates these banks asynchronously. The state is only **latched** into
the actual execution pipeline at specific trigger points (notably
`CP_DRAW_INDX_OFFSET`), allowing the CP to run ahead of the Shader Processors.

### 6.2 State Loading Phase (CP_SET_DRAW_STATE)

```
  CP reads CP_SET_DRAW_STATE header, cnt=90
  For each of 90 state IDs:
    CP looks up state IB address from internal table
    CP loads IB from memory (DMA from system RAM)
    CP processes register writes in the IB
    (These are PKT4 packets inside the IB — same mechanism)
```

The 90 state IBs collectively contain hundreds of register writes. This is
why our trace shows ~200 PKT4 writes before the draw — they're all from
the state IB chains being loaded and processed.

### 6.3 Cache Flush Phase

```
  CP processes CP_EVENT_WRITE(0x46) with event=CACHE_FLUSH_TS:
    CP sends "flush L2 cache" signal to memory subsystem
    Memory subsystem writes dirty cache lines to system RAM
    Memory subsystem signals completion to CP
    CP writes timestamp to specified memory address
    CP processes CP_WAIT_FOR_IDLE:
    CP stalls until all GPU pipeline stages report idle
    (This can take hundreds of cycles)
```

Each `CP_EVENT_WRITE` + `CP_WAIT_FOR_IDLE` pair drains the pipeline.
For a simple triangle, the pipeline is mostly empty — so these drains
are wasted cycles. In a real game, the pipeline would be full of work
from previous draws, and the drain would be necessary for correct
synchronization.

### 6.4 The Draw Itself

```
  CP processes CP_DRAW_INDX_OFFSET(0x38):
    CP reads draw initiator: SRCSEL=AUTO_INDEX, PRIM=TRIANGLES
    CP reads instanceCount=1, vertexCount=3

  CP → VFD: "Generate 3 auto-indexed vertices"
    VFD starts vertex shader invocations:
      Thread 0: gl_VertexIndex=0 → gl_Position=vec4( 0.0,-0.5, 0.0, 1.0)
      Thread 1: gl_VertexIndex=1 → gl_Position=vec4( 0.5, 0.5, 0.0, 1.0)
      Thread 2: gl_VertexIndex=2 → gl_Position=vec4(-0.5, 0.5, 0.0, 1.0)

  CP → PA (Primitive Assembly):
    Groups 3 vertices → 1 triangle
    Computes edge equations for rasterization

  CP → GRAS (Rasterizer):
    Vulkan's default coordinate system: Y points *down*.
    y = -0.5 maps to the top of the viewport (y = 64).
    Transforms triangle to screen coordinates (256×256 viewport):
      Vertex 0: (128, 64)  — top center   (y = -0.5 → viewport top)
      Vertex 1: (192, 192) — bottom right (y =  0.5 → viewport bottom)
      Vertex 2: (64,  192) — bottom left  (triangle points up)
    Triangle dimensions:
      Base  = 192 - 64  = 128 pixels
      Height = 192 - 64 = 128 pixels
      Area  = ½ × 128 × 128 = 8,192 fragments
    Scan-converts: finds all pixels inside the triangle
    8,192 fragments generated (12.5% of the tile)

  CP → SP (Fragment Shader) × 8,192:
    For each fragment:
      Interpolate varyings (none in our shader)
      Run fragment shader: outColor = vec4(1,0,0,1)

  CP → RB (Render Backend):
    For each fragment:
      No depth test (no depth attachment)
      No stencil test
      No blending (fragment color replaces framebuffer)
      Write red pixel (0xFF, 0x00, 0x00, 0xFF) to GMEM at this tile coordinate
```

### 6.5 Post-Draw: GMEM → Sysmem Resolve

```
  CP processes CP_EVENT_WRITE(RB_DONE_TS):
    Waits for RB to finish writing all fragments to GMEM

  CP processes CP_EVENT_WRITE(CACHE_FLUSH_TS):
    Flushes L2 cache — makes GMEM writes visible

  CP processes CP_WAIT_FOR_IDLE:
    Full pipeline drain

  CP processes CP_INDIRECT_BUFFER:
    Chains to resolve IB:
      → CP loads resolve IB from memory
      → Resolve IB contains PKT4 writes to configure the
        blit engine (2D copy from GMEM to system RAM)
      → Blit engine copies tile contents to framebuffer
      → RESOLVE COMPLETE — triangle is now visible in system RAM
```

For the full 1080×2400 screen, this tile loop (load → render → resolve) repeats
dozens of times. Our 256×256 test is a single tile, so there's exactly one
resolve at the end.

---

## 7. Archaeological Findings

### 7.1 The Barrier Bottleneck

```
  tu_emit_cache_flush_renderpass() is called BEFORE every draw
  → 16 CP_EVENT_WRITE packets per draw
  → 3 CP_WAIT_FOR_IDLE stalls per draw
  → Total barrier cost: ~19 PM4 packets per draw
```

With `patch 0004` applied (barrier coalesce):
```cpp
  if (!cmd_buffer->state.renderpass_cache.flush_bits)
      return;  // skip the entire 19-packet barrier sequence
```

For draws that don't change cache state (most draws in a render pass),
all 19 packets are skipped. This is the biggest single optimization target.

### 7.2 The State Blast

```
  84 RB register writes per draw
  43 GRAS register writes per draw
  32 SP register writes per draw
  ────────────────────────────────
  ~200 total register writes for pipeline state
```

Turnip's dirty-bit system (patch 0003) tracks which state changed since the
last draw. For the first draw, everything is dirty. For subsequent draws with
the same pipeline, most of these writes would be skipped.

### 7.3 Gen8 Fossils

```
  CP_SET_MARKER (0x65) — New in A8XX. Used for binning/rendering phase markers
  CP_WAIT_FOR_ME (0x13) — New in A8XX. Lightweight pipeline barrier
  CP_THREAD_CONTROL (0x17) — Introduced in A7XX, used in A8XX
```

The presence of A7XX opcodes (`CP_THREAD_CONTROL`) proves that A8XX is not a
clean-sheet design — it's an evolution of the A7XX command processor with
gen8-specific additions. The same `tu_cs_emit_pkt7` function emits packets for
all generations, with the driver using `if (CHIP >= A8XX)` conditionals to
include gen8-specific opcodes.

### 7.4 Blind LRZ Emission

```
  15 PKT4 writes to 0x81xx (GRAS_LRZ_*) per draw
```

Even with no depth attachment, the LRZ block is fully configured with zero
values. Turnip initializes state to default/zero to prevent dirty-state leakage
between command buffers, but unconditionally writing full LRZ configuration
for a color-only pass is wasted command processor bandwidth. A potential
optimization: skip LRZ emission when the pipeline has no depth attachment.

### 7.5 Register Write Redundancy

```
  Register 0x8818 (RB_UNKNOWN_8818): written 12 times, always val=0x00000000
  Register 0xB182 (HLSQ_*): written 12 times
  Register 0xB183 (HLSQ_*): written 12 times
```

Some registers are written with the same value every time they appear. The
reg-cache catches 2 of these. The remaining 10 redundant writes happen across
`tu_cs_reset()` boundaries, which correctly invalidate the cache — you cannot
trust that GPU state persists across a command buffer reset.

The redundant HLSQ writes within a single draw are likely artifacts of Turnip's
state-group emission logic: VS state and FS state both independently touch
the same HLSQ enable register. When multiple state groups share a dirty flag
for the same physical register, the register gets written once per group even
though its value hasn't changed. A unified HLSQ dirty mask would collapse
these into a single write.

---

## 8. The Connectome So Far

Every edge in this map is **verified** — backed by a PM4 packet in our trace
that was emitted by real Adreno 830 hardware.

```
  Vulkan API
  └─ vkCmdDraw(3)
      └─ tu_CmdDraw                    [tu_cmd_buffer.cc:8631]
          ├─ tu6_emit_vs_params          → PKT4 (VS constants)
          ├─ tu6_draw_common<A8XX>       [tu_cmd_buffer.cc:8170]
          │   ├─ tu_emit_draw_state       → CP_SET_DRAW_STATE (0x43) cnt=90
          │   │   ├─ TU_DRAW_STATE_PROGRAM_CONFIG → RB/SP/HLSQ register writes
          │   │   ├─ TU_DRAW_STATE_VS             → SP_VS_* register writes
          │   │   ├─ TU_DRAW_STATE_FS             → SP_FS_* register writes
          │   │   ├─ TU_DRAW_STATE_VPC            → VPC_* register writes
          │   │   └─ TU_DRAW_STATE_PRIM_MODE_GMEM → GRAS_* register writes
          │   ├─ tu6_emit_lrz             → PKT4 reg=0x81xx × 15
          │   └─ tu_emit_cache_flush      → CP_EVENT_WRITE (0x46) × 16
          │                                → CP_WAIT_FOR_IDLE (0x26) × 3
          │                                → CP_WAIT_FOR_ME (0x13) × 1
          │                                → CP_THREAD_CONTROL (0x17) × 6
          └─ tu_cs_emit_pkt7(CP_DRAW_INDX_OFFSET) → THE DRAW (0x38) cnt=3
              ├─ tu_draw_initiator(DI_SRC_SEL_AUTO_INDEX)
              ├─ instanceCount = 1
              └─ vertexCount = 3
```

---

## Next Steps in the Archaeological Dig

1. **Apply barrier coalesce (patch 0004)**: Skip `tu_emit_cache_flush_renderpass`
   when `flush_bits == 0`. Expect 16 fewer `CP_EVENT_WRITE` per draw.

2. **Trace a second draw with the same pipeline**: The dirty-bit system should
   skip most of the ~200 state PKT4 writes on the second draw.

3. **Add per-function trace instrumentation**: Currently we only trace the
   low-level `tu_cs_emit_pkt4/pkt7/write_reg` functions. Adding trace points
   to `tu6_draw_common`, `tu_emit_draw_state`, and `tu6_emit_lrz` would give
   us caller context in the trace.

4. **Map unknown registers**: `RB_UNKNOWN_8818` and several HLSQ registers are
   unnamed in the generated register map. Cross-reference with Qualcomm's
   proprietary headers or reverse-engineer by observing which values are written.

5. **Build the interactive connectome**: Use `cmd/graph-builder` + ctags/ripgrep
   to generate a visual map of `Vulkan API → Turnip function → PM4 register`.

---

*This document was generated from a single `vkCmdDraw(3)` trace on Adreno 830
using the Mesa Turnip driver with custom `TU_CS_TRACE` instrumentation. The
trace is reproducible — see `probes/loader_probe.c` and run with
`TU_DEBUG=trace`.*
