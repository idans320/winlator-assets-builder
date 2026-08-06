# Hack 02: YOLO_SYNC — Barrier Stripping

## Problem

Vanilla Turnip emits a full cache flush barrier sequence on every draw call:

```
tu_emit_cache_flush() → tu6_emit_flushes<A8XX>(cmd_buffer, cs, cache)
  CP_WAIT_FOR_IDLE          ← stalls CP until GPU pipeline is idle
  CP_WAIT_FOR_ME            ← stalls until previous commands complete
  CP_EVENT_WRITE(LABEL)
  CP_INVALIDATE_STATE
  CP_EVENT_WRITE(CACHE_FLUSH_TS)
  CP_WAIT_MEM_WRITES
  CP_EVENT_WRITE(RB_DONE_TS)
  ... (up to 10 PKT7 packets, ~40 dwords)
```

For DXVK workloads doing 100+ draws per frame, this is **1,000+ barrier packets per frame**.
Most of these barriers are unnecessary — the GPU pipeline doesn't need to be flushed between
every draw when draws are independent.

## Hack

Replace per-draw barrier emission with a deferred mega-flush at EndRenderPass.

### Injection Point 1: tu_emit_cache_flush()

```c
// tu_cmd_buffer.cc:483
void tu_emit_cache_flush(struct tu_cmd_buffer *cmd_buffer)
{
    struct tu_cs *cs = &cmd_buffer->cs;
    struct tu_cache_state *cache = &cmd_buffer->state.cache;

    // HACK: YOLO — skip all barriers, defer to EndRenderPass
    if (unlikely(cmd_buffer->cs.dirty.yolo_sync)) {
        cmd_buffer->cs.dirty.yolo_barrier_needed = true;
        return;  // <-- immediate return, no barriers emitted
    }

    BITMASK_ENUM(tu_cmd_flush_bits) flushes = cache->flush_bits;
    tu6_emit_flushes<CHIP>(cmd_buffer, cs, cache);
    // ...
}
```

### Injection Point 2: tu_emit_cache_flush_renderpass()

```c
// tu_cmd_buffer.cc (renderpass variant)
void tu_emit_cache_flush_renderpass(struct tu_cmd_buffer *cmd_buffer)
{
    // HACK: YOLO — same pattern
    if (unlikely(cmd_buffer->draw_cs.dirty.yolo_sync)) {
        cmd_buffer->draw_cs.dirty.yolo_barrier_needed = true;
        return;
    }

    if (!cmd_buffer->state.renderpass_cache.flush_bits && likely(!tu_env.debug))
        return;
    // ...
}
```

### Mega-Flush at EndRenderPass

```c
// tu_cs.h — called at EndRenderPass
void tu_cs_flush_barrier_yolo(struct tu_cs *cs)
{
    if (!cs->dirty.yolo_sync || !cs->dirty.yolo_barrier_needed)
        return;

    // Single CP_EVENT_WRITE + CP_WAIT_FOR_IDLE for all draws in renderpass
    tu_cs_emit_pkt7(cs, CP_EVENT_WRITE, 4);
    tu_cs_emit(cs, 0x1f);  // RB_DONE_TS | CACHE_FLUSH_TS | LABEL
    tu_cs_emit(cs, 0);
    tu_cs_emit(cs, 0);
    tu_cs_emit(cs, 0);
    tu_cs_emit_wfi(cs);    // CP_WAIT_FOR_IDLE

    cs->dirty.yolo_barrier_needed = false;
}
```

## Risk Assessment

| Risk | Severity | Mitigation |
|------|----------|-----------|
| Coherence violation between dependent draws | HIGH | Only enable for workloads with independent draws |
| Missing texture cache flush | MEDIUM | CP_EVENT_WRITE with CACHE_FLUSH_TS at EndRenderPass |
| GPU hang | HIGH | CP_WAIT_FOR_IDLE ensures pipeline drain at EndRenderPass |
| Depth attachment artifacts | MEDIUM | RB_DONE_TS ensures render backend completion |

## Performance Impact

Per draw saved:
- ~10 PKT7 barrier packets removed
- ~40 dwords of command stream saved

For 100 draws/frame:
- ~1,000 PKT7 packets → 1 at EndRenderPass (99.9% reduction)
- ~4,000 dwords → 7 dwords (99.8% reduction)

## Activation

```bash
TU_YOLO_SYNC=1 ./app
```

Reads `TU_YOLO_SYNC` from environment at `tu_cs_init` time.
