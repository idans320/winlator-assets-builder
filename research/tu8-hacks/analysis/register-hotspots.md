# Register Hotspots: Most-Written GPU Registers

Analysis from PM4 trace decoding of Turnip gen8 workloads. Data is from
`TU_DEBUG=trace` captures on real Adreno 830 hardware.

## Trace Methodology

1. Run Vulkan probe on Snapdragon 8 Elite (Adreno 830) with `TU_DEBUG=trace`
2. Capture PM4 command stream log
3. Parse log with `analysis-py/trace_parser.py` → JSON
4. Decode register offsets to hardware blocks via `pm4/regmap.go` (3666-register lookup table)

## Single Triangle Draw (probe_draw.c)

**Context:** One `vkCmdDraw(3)`, simple triangle, no texture, no blending.
**Trace:** 501 PM4 packets, 453 register writes, 188 unique registers.

### Top 10 Most-Written Registers

| Rank | Register | Offset | Hardware Block | Writes | % of Total |
|------|----------|--------|----------------|--------|------------|
| 1 | `HLSQ_UPDATE_CNTL` | 0xB182 | HLSQ (High-Level Sequencer) | 12 | 2.6% |
| 2 | `RB_UNKNOWN_8818` | 0x8818 | RB (Render Backend) | 11 | 2.4% |
| 3 | `HLSQ_UPDATE_CNTL2` | 0xB183 | HLSQ | 11 | 2.4% |
| 4 | `VPC_UNKNOWN_8213` | 0x8213 | VPC (Vertex Parameter Cache) | 6 | 1.3% |
| 5 | `PC_UNKNOWN_A9CB` | 0xA9CB | PC (Primitive Controller) | 6 | 1.3% |
| 6 | `RB_8x28-8x2D` | 0x8228-0x822D | RB (Render Backend) | 6 each | 7.9% |
| 7 | `RB_UNKNOWN_8819` | 0x8819 | RB | 6 | 1.3% |
| 8 | `SP_UNKNOWN_9800` range | 0x9800+ | SP (Shader Processor) | 4-6 | 6.6% |
| 9 | `GRAS_UNKNOWN_8000` range | 0x8000+ | GRAS (Rasterizer) | 3-4 | 5.3% |
| 10 | `VFD_UNKNOWN_A000` range | 0xA000+ | VFD (Vertex Fetch/Decode) | 2-3 | 2.6% |

### Writes by Hardware Block

```
 Register writes by hardware block (single triangle draw):
 ┌─────────────────────────────────────────────────────────┐
 │ Block    │ Writes │ Unique Regs │ Description            │
 ├─────────────────────────────────────────────────────────┤
 │ RB       │   84   │     47      │ Blend, depth, stencil, │
 │          │        │             │ clear color, resolve   │
 │ GRAS     │   43   │     22      │ Viewport, scissor, LRZ,│
 │          │        │             │ rasterizer config      │
 │ SP       │   32   │     15      │ Shader constants,      │
 │          │        │             │ uniforms, SSBO         │
 │ VPC      │   19   │     12      │ Vertex attributes,     │
 │          │        │             │ varying linkage        │
 │ LRZ      │   15   │      8      │ Low-Resolution Z pass  │
 │ HLSQ     │   14   │      7      │ Shader scheduling,     │
 │          │        │             │ wave config            │
 │ PC       │   12   │      6      │ Primitive controller,  │
 │          │        │             │ draw state             │
 │ VFD      │    8   │      5      │ Vertex fetch config    │
 │ Other    │  226   │     66      │ Misc (CP, unknown, etc)│
 └─────────────────────────────────────────────────────────┘
```

## DXVK Simulator (dxvk_probe.c)

**Context:** 100 draws with pipeline switching (every 20 draws), UBO descriptor updates
(every 5 draws), push constant skips.
**Trace:** 61,745 PM4 packets, 48,675 register writes, 205 unique registers.

### Top 10 Most-Written Registers

| Rank | Register Range | Offset Range | Description | Writes | % |
|------|---------------|-------------|-------------|--------|---|
| 1-8 | SP/UBO push constants | 0xAB30-0xAB37 | Shader Processor UBO/push constant slots | 5,000 each | 82% |
| 9 | RB_UNKNOWN_8818 | 0x8818 | Render Backend config | 208 | 0.4% |
| 10 | HLSQ_UPDATE_CNTL | 0xB182 | HLSQ control | 208 | 0.4% |

**Key insight:** 82% of all register writes are push constant / UBO updates. The push
constant cache (Hack 06) would eliminate ~40% of these.

## Redundancy Analysis

From the `regcache_vs_baseline` diff analysis:

### Before Patch (Vanilla)
```
Total writes: 309
Top emitter: tu_cs_emit_pkt4  → 222 writes
             tu_cs_emit_write_reg → 87 writes
Most written: 0x8818 (RB) 12x, 0xB182 (HLSQ) 12x, 0xB183 (HLSQ) 12x
```

### After Patch (Regcache/Dirty-Shadow)
```
Total writes: 307
Top emitter: tu_cs_emit_pkt4  → 220 writes  (-2)
             tu_cs_emit_write_reg → 87 writes (unchanged)
Most written: 0xB182 (HLSQ) 12x, 0x8818 (RB) 11x (-1), 0xB183 (HLSQ) 11x (-1)
```

**Savings:** 2 redundant writes eliminated per blit operation:
- 0x8818 (RB): wrote same value twice → 1 suppressed
- 0xB183 (HLSQ): wrote same value twice → 1 suppressed

## Why These Registers?

### RB_UNKNOWN_8818 (Render Backend)
Written on every CMDBUF submit. Controls render backend global configuration.
Because it's written from multiple code paths (blit, clear, draw), the same
value is often re-emitted.

### HLSQ_UPDATE_CNTL (0xB182) / HLSQ_UPDATE_CNTL2 (0xB183)
High-Level Sequencer update control registers. Tell the GPU which shader stages
are active. Written before every draw, but typically doesn't change between
consecutive draws with the same pipeline.

### SP UBO/Push Constants (0xAB30-0xAB37)
Shader Processor uniform buffer / push constant base addresses. DXVK updates
these on every draw call — 5,000 writes each for 100 draws. The push constant
cache targets exactly these registers.

## Hardware Block Reference

| Hardware Block | Register Prefix | Typical Offset Range | Purpose |
|---------------|----------------|---------------------|---------|
| CP | `CP_` | 0x0000-0x07FF | Command Processor |
| RB | `RB_` | 0x0800-0x0FFF, 0x8800-0x8FFF | Render Backend (blend, depth, stencil, color output) |
| GRAS | `GRAS_` | 0x2000-0x20FF | Rasterizer |
| HLSQ | `HLSQ_` | 0xB000-0xB1FF | High-Level Sequencer (shader scheduling) |
| SP | `SP_` | 0x9000-0x98FF, 0xA800-0xABFF | Shader Processor |
| VPC | `VPC_` | 0x4000-0x41FF | Vertex Parameter Cache |
| VFD | `VFD_` | 0xA000-0xA0FF | Vertex Fetch/Decode |
| PC | `PC_` | 0x2100-0x21FF | Primitive Controller |
| LRZ | `GRAS_LRZ_` | 0x2000 (subrange) | Low-Resolution Z |
| TEX | `TEX_` | 0xE000-0xE300 | Texture sampler configuration |
| UCHE | `UCHE_` | 0x0E00-0x0EFF | Unified Cache |
