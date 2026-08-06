# TU8 Hack Architecture: How They Compose

## The Problem: Vanilla Mesa PKT4 Overhead

Vanilla Mesa emits every GPU register write as an immediate `tu_cs_emit_pkt4` + `tu_cs_emit`
pair into the command stream ring buffer. On a typical `vkCmdDraw`, the Turnip driver emits
~150 register writes and ~10 barrier packets. For DXVK workloads doing 100 draws/frame, this
is ~15,000 register writes and ~1,000 barrier packets per frame.

The Adreno command processor parses every PKT4 — even redundant ones that set the same register
to the same value as the previous draw. Driver-side CPU overhead of generating these redundant
packets is 11-33% of total draw-call CPU time (measured via stimulation benchmarks).

## Solution: Multi-Layer GPU Register Deduplication

The TU8 hacks form a **layered optimization stack** — each layer reduces overhead at a
different level of the register-write pipeline:

```
Layer 5: Push Constant Cache          │ Application-level (vkCmdPushConstants)
         ──────────────────────────   │ Layers above can be applied
Layer 4: Barrier Coalesce             │ independently. Each provides
         ──────────────────────────   │ incremental savings.
Layer 3: YOLO_SYNC (Barrier Strip)    │
         ──────────────────────────   │ Layers 0-2 are the core
Layer 2: STATE_THROTTLE               │ dirty-shadow system — they
         ──────────────────────────   │ compose together within
Layer 1: REG_BLAST (Emit Strategy)    │ tu_cs_flush_dirty().
         ──────────────────────────   │
Layer 0: Dirty Shadow Registers       │ (always on for A8XX+)
```

## Layer 0: Dirty Shadow Registers

**Core mechanism.** Every `tu_cs_emit_pkt4` call is replaced with `tu_cs_set_register`:

1. Writes register value into a 5120-slot shadow array (20KB per command stream)
2. Sets dirty bits in two-tier bitmap (3×64-bit blocks + 160×32-bit slots)
3. If the value equals what's already in shadow, returns immediately (redundant write suppression)
4. PKT4 emission is deferred to `tu_cs_flush_dirty()`, which scans dirty bits and emits compact batches

**Vanilla:**
```c
tu_cs_emit_pkt4(cs, REG_HLSQ_UPDATE_CNTL, 1);
tu_cs_emit(cs, value);
```

**Hacked:**
```c
tu_cs_set_register(cs, REG_HLSQ_UPDATE_CNTL, value);
// ... later ...
tu_cs_flush_dirty(cs);  // emits all dirty registers as compact PKT4
```

## Layer 1: REG_BLAST (Emission Strategy)

Controls *how* dirty registers are emitted in `tu_cs_flush_dirty()`:

- **Default (ffs-scan):** Uses `__builtin_ffs` to find each dirty bit, merges consecutive
  dirty slots into multi-register PKT4 packets. Good for sparse dirty sets.
- **REG_BLAST:** Ignores the dirty-slot bitmap entirely. Finds dirty blocks (64-reg granularity),
  emits entire 32-register blocks as PKT4(reg, 32) + 32 values. Good for dense dirty sets
  where scanning overhead exceeds the cost of emitting a few extra registers.

Activates when `TU_REG_BLAST=1` is set.

## Layer 2: STATE_THROTTLE

When the application is CPU-bound on draw calls, consecutive draws often set the *same*
register state. STATE_THROTTLE intentionally skips `tu_cs_flush_dirty()` for N-1 out of
every N draws — the GPU continues using the last committed register values.

This trades correctness (some draws may see stale state) for performance (N× fewer PKT4
emissions). Safe when state doesn't change between consecutive draws (common in draw-call
heavy workloads).

Activates when `TU_SKIP_STATE=N` (N > 1).

## Layer 3: YOLO_SYNC (Barrier Stripping)

**The most aggressive hack.** Replaces per-draw cache flushes (~10 PKT7 barrier packets)
with a deferred mega-flush at EndRenderPass.

Inside `tu_emit_cache_flush()` and `tu_emit_cache_flush_renderpass()`:
1. Sets `yolo_barrier_needed = true`
2. Returns immediately — no barrier packets emitted

At EndRenderPass, `tu_cs_flush_barrier_yolo()` emits a single `CP_EVENT_WRITE` +
`CP_WAIT_FOR_IDLE` pair, covering all draws in the renderpass.

**Vanilla (per draw):**
```
CP_WAIT_FOR_IDLE
CP_WAIT_FOR_ME
CP_EVENT_WRITE(LABEL)
CP_INVALIDATE_STATE
CP_EVENT_WRITE(CACHE_FLUSH_TS)
CP_WAIT_MEM_WRITES
CP_EVENT_WRITE(RB_DONE_TS)
...
```

**YOLO (once per renderpass):**
```
CP_EVENT_WRITE
CP_WAIT_FOR_IDLE
```

Risk: removes coherence guarantees between draws within a renderpass. Safe for workloads
where draws don't depend on each other's output (most DXVK rendering).

## Layer 4: Barrier Coalesce

Independent of YOLO_SYNC. Adds an early-return before the barrier emission pipeline:

```c
void tu_emit_cache_flush(struct tu_cmd_buffer *cmd_buffer) {
    // HACK: skip entire emission pipeline when no flush bits accumulated
    if (!cache->flush_bits && likely(!tu_env.debug))
        return;
    // ... rest of barrier emission ...
}
```

This catches cases where `vkCmdPipelineBarrier` or state changes queue flush bits
that later get resolved to zero — avoiding emitting a no-op WFI+WAIT_FOR_ME sequence.

## Layer 5: Push Constant Cache

DXVK-specific optimization. DXVK calls `vkCmdPushConstants` on every draw even when
the push constant data hasn't changed. This caches the last value per push constant
slot and skips re-emission when unchanged.

This is currently probe-level only (in `loader_probe.c`) — not integrated into the
hack injection script.

## Composition Examples

### DXVK Workload (100 draws)

```
Without hacks:
  100 × tu_emit_cache_flush      = ~1000 PKT7 barrier packets
  100 × ~150 register writes     = ~15,000 PKT4 dwords
  Total                          = ~16,000 dwords/draws

With dirty-shadow + YOLO_SYNC + BARRIER_COALESCE:
  1 × tu_cs_flush_barrier_yolo   = 7 PKT7 barrier packets
  100 × ~120 register writes*    = ~12,000 PKT4 dwords (after dedup)
  Total                          = ~12,007 dwords/draws  (-25%)
  *20% of writes are redundant (same value as previous draw)
```

### Blit Operation (500 blits)

```
Without hacks:
  500 × ~80 register writes      = ~40,000 PKT4 dwords

With dirty-shadow:
  500 × ~26 register writes*     = ~13,000 PKT4 dwords  (-67.5%)
  *blits often write the same registers repeatedly
```

## Memory Model

Each `tu_cs` (command stream) has a `dirty` struct:

```
struct dirty {
    uint32_t *shadow;           // lazy-allocated: 5120 × 4 = 20,480 bytes
    uint64_t  blocks[3];        // 3 × 8 = 24 bytes (Tier 1: block-level dirty bits)
    uint32_t *regs;             // lazy-allocated: 160 × 4 = 640 bytes (Tier 2: slot-level)
    bool      enabled;          // 1 byte (active for A8XX+)
    bool      yolo_sync;        // 1 byte (TU_YOLO_SYNC env var)
    bool      yolo_barrier_needed; // 1 byte (set when barriers skipped)
    uint8_t   throttle_n;       // 1 byte (TU_SKIP_STATE env var)
    uint8_t   throttle_counter; // 1 byte (increments each flush_dirty call)
    bool      reg_blast;        // 1 byte (TU_REG_BLAST env var)
};

Total per tu_cs: ~21KB (shadow + regs allocated on first tu_cs_set_register call)
```

Typical Turnip has 2-3 tu_cs objects (draw_cs, tile_cs, etc.) → ~60KB total overhead.
