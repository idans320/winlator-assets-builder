# TU8 Hacks: Turnip gen8 Performance Optimizations

Research documenting the complete set of performance hacks applied to Mesa Turnip
for Adreno 8xx GPUs (Snapdragon 8 Elite), and how they differ from vanilla Mesa.

## Quick Navigation

| Section | Description |
|---------|-------------|
| [Hack Comparison Matrix](#hack-comparison-matrix) | All hacks vs vanilla Mesa at a glance |
| [Vanilla vs Hacked Architecture](#vanilla-vs-hacked-architecture) | Go graph analysis of affected code paths |
| [Quantitative Impact](#quantitative-impact) | Measured PKT4/draw savings from trace analysis |
| [Hack Details](hacks/) | Deep-dive into each hack's mechanism |
| [Graph Analysis](analysis/) | Go analysis results: neuron paths, register hotspots, call graphs |

## Architecture Overview

```
  Vulkan API Calls
        │
        ▼
  tu_CmdDraw / tu_CmdDispatch
        │
        ▼
  ┌──────────────────────────────────────┐
  │  VANILLA MESA (A6XX code path)      │
  │                                      │
  │  For every draw:                     │
  │  1. tu_emit_cache_flush → 10+ PKT7  │
  │  2. tu_emit_pkt4 × N → N dwords     │
  │  3. HLSQ regs × 12, RB regs × 11    │
  │  4. Repeat all of above for next draw│
  └──────────────────────────────────────┘

  ┌──────────────────────────────────────┐
  │  HACKED: TU8 Optimization Stack     │
  │                                      │
  │  For every draw:                     │
  │  1. YOLO_SYNC → skip barriers       │ ◄── strip 10+ PKT7/draw
  │  2. tu_cs_set_register → shadow[]   │ ◄── deduplicate reg writes
  │  3. tu_cs_flush_dirty → compact PKT4│ ◄── emit only dirtied regs
  │  4. REG_BLAST → brute-force blocks  │ ◄── skip ffs scan overhead
  │  5. STATE_THROTTLE → skip N draws   │ ◄── batch writes across draws
  │  6. BARRIER_COALESCE → skip flush   │ ◄── no-op when flush_bits==0
  │                                      │
  │  At EndRenderPass:                   │
  │  7. tu_cs_flush_barrier_yolo → WFI  │ ◄── single barrier, not per-draw
  └──────────────────────────────────────┘
```

## Hack Comparison Matrix

| Feature | Vanilla Mesa | TU8 Hacked | Environment Var |
|---------|-------------|------------|-----------------|
| **Register emission** | `tu_cs_emit_pkt4` writes directly to command stream | `tu_cs_set_register` writes to shadow[], deferred batch flush | (always on for A8XX+) |
| **Redundant write detection** | None — every write emitted | Two-tier dirty-bit: writes suppressed if value unchanged | — |
| **Per-draw barriers** | `CP_WAIT_FOR_IDLE` + `CP_WAIT_FOR_ME` + `CP_EVENT_WRITE` on every `tu_emit_cache_flush` | Skipped, single mega-flush at EndRenderPass | `TU_YOLO_SYNC=1` |
| **State throttling** | Not available | Flush dirty registers only every N draws | `TU_SKIP_STATE=N` |
| **Register block dump** | Not available | Brute-force 64-reg PKT4 emission (skip ffs scan) | `TU_REG_BLAST=1` |
| **Barrier coalescing** | Barrier emission pipeline always runs | Early-return when `flush_bits == 0` | `TU_BARRIER_COALESCE=1` |
| **Push constant cache** | Every `vkCmdPushConstants` emitted | Cache last value per slot, skip if unchanged | (probes only) |
| **Data structure** | None | `struct tu_cs.dirty` — 5120-entry shadow + 3×64-bit block bitfields + slot bitfields | — |
| **Memory overhead** | 0 bytes | ~20KB per `tu_cs` (shadow) + ~256B (bitfields) | — |

## Vanilla vs Hacked: Code Flow

### Vanilla Mesa: Register Write Path

```
tu_CmdDraw()                                  // tu_cmd_buffer.cc
  → tu6_draw_common()                         // tu_cmd_buffer.cc
    → tu_emit_cache_flush_renderpass()        // tu_cmd_buffer.cc
      → tu_emit_cache_flush()                 // tu_cmd_buffer.cc
        → tu6_emit_flushes<A8XX>()            // tu_cmd_buffer.cc
          → CP_WAIT_FOR_IDLE (PKT7)
          → CP_WAIT_FOR_ME (PKT7)
          → CP_EVENT_WRITE (PKT7)
    → tu_cs_emit_pkt4(reg, 1)                 // tu_cs.h — inline, immediate emission
    → tu_cs_emit(value)                       // tu_cs.h — inline, immediate emission
    → [repeated for every register: HLSQ×12, RB×11, VPC×19, SP×32, GRAS×43]
```

### Hacked: Register Write Path

```
tu_CmdDraw()                                  // tu_cmd_buffer.cc
  → tu6_draw_common()                         // tu_cmd_buffer.cc
    → tu_emit_cache_flush_renderpass()        // tu_cmd_buffer.cc
      → if (YOLO_SYNC) → yolo_barrier_needed=true; return;  ← HACK
    → tu_cs_set_register(cs, reg, val)        // tu_cs.h — HACK added
      → if (!dirty.enabled) goto emit
      → if (shadow[idx] == val) return        // redundant write suppression
      → shadow[idx] = val
      → regs[block] |= (1 << bit)             // mark dirty
      → blocks[tier1] |= (1 << block)         // mark dirty block
    → [registers accumulate in shadow[], no PKT4 emitted yet]
    → tu_cs_flush_dirty(cs)                   // HACK — called before draw state submit
      → if (STATE_THROTTLE) → skip every N draws
      → if (REG_BLAST) → dump 32-reg blocks
      → else → ffs-scan dirty bits, merge consecutive into compact PKT4 batches

At EndRenderPass:
    → tu_cs_flush_barrier_yolo(cs)            // HACK — single WFI for all draws
      → CP_EVENT_WRITE + CP_WAIT_FOR_IDLE
```

## Source Files Modified

| File | Hack | Lines Modified |
|------|------|---------------|
| `src/freedreno/vulkan/tu_cs.h` | Dirty shadow + set_register + flush_dirty + flush_barrier_yolo | +120 lines (struct + 4 functions) |
| `src/freedreno/vulkan/tu_cs.cc` | tu_cs_init (dirty init), tu_cs_reset/finish (free), #include <stdlib.h> | +15 lines |
| `src/freedreno/vulkan/tu_cmd_buffer.cc` | YOLO in tu_emit_cache_flush + renderpass variant | +6 lines |

## Quantitative Impact

Measured via PM4 trace decoder on probe workloads:

| Workload | Vanilla Writes | Hacked Writes | Reduction |
|----------|---------------|---------------|-----------|
| Single triangle (probe_draw) | 453 writes / 501 pkts | 448 writes / 496 pkts | **-5 writes** (-1.1%) |
| Blit operation | 309 writes / 329 pkts | 307 writes / 327 pkts | **-2 writes** (-0.6%) |
| DXVK simulator (100 draws) | 48,675 writes | 45,375 writes | **-3,300 writes** (-6.8%) |

**Stimulation-estimated savings** (Go fuzz/stub pipeline):

| Optimization | Light (100d) | Heavy (1000d) | Blit (500b) | Barrier | State |
|-------------|-------------|--------------|------------|---------|-------|
| Redundant write filter | 11.4% | 11.4% | 33.5% | 11.0% | 11.0% |
| Barrier coalescing | 130 skip | 1600 skip | 230 skip | 1250 skip | 330 skip |
| Combined | **~15%** | **~15%** | **~33%** | **~17%** | **~14%** |

## Applying the Hacks

```bash
# One-shot: inject all hacks into cloned Mesa source
./apply-all-hacks.sh

# Then build Mesa Turnip (hacks auto-activate for A8XX+ chips)
devbox run -- bash mesa/build.sh

# Control at runtime via environment variables:
# TU_YOLO_SYNC=1        Strip per-draw barriers
# TU_SKIP_STATE=4       Flush dirty registers every 4th draw
# TU_REG_BLAST=1        Use brute-force 64-reg block dump
# TU_BARRIER_COALESCE=1  Skip flush emission when no flush bits
```

## Directory

```
tu8-hacks/
├── README.md                    # This file — overview + comparison
├── hacks/
│   ├── 00-overview.md           # Architecture + how hacks compose
│   ├── 01-dirty-shadow-registers.md  # Two-tier dirty-bit shadow system
│   ├── 02-yolo-sync.md          # Barrier stripping + deferred mega-flush
│   ├── 03-state-throttle.md     # Flush skipping every N draws
│   ├── 04-reg-blast.md          # Brute-force 64-reg block dump
│   ├── 05-barrier-coalesce.md   # Skip flush when no bits accumulated
│   └── 06-push-constant-cache.md # DXVK-style push constant deduplication
├── analysis/
│   ├── gen8-neuron-paths.md     # Go graph: which code paths carry A8XX register writes
│   ├── register-hotspots.md     # Top-written GPU registers mapped to hardware blocks
│   └── impact-metrics.md        # Trace diff results + fuzz stimulation benchmarks
└── patches/
    └── apply-all-hacks.sh       # Reference: the hack injection script (symlink to root)
```
