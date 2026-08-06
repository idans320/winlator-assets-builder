# Hack 03: STATE_THROTTLE — Skip PKT4 every N draws

## Problem

In draw-call heavy workloads (e.g. DXVK with `vkCmdDraw` in a tight loop), consecutive
draws often set the **same GPU register state**. The dirty shadow system tracks and
deduplicates writes, but still flushes on every draw. When state is truly identical
between draws, even the flush is wasted.

## Hack

Intentionally skip `tu_cs_flush_dirty()` for N-1 out of every N draws. The GPU
continues using the last committed register values — which is correct when state
hasn't changed.

### Implementation

```c
void tu_cs_flush_dirty(struct tu_cs *cs)
{
    if (!cs->dirty.enabled || !cs->dirty.shadow)
        return;

    // HACK: skip flush_dirty every N draws
    if (cs->dirty.throttle_n > 1) {
        if (++cs->dirty.throttle_counter < cs->dirty.throttle_n)
            return;  // skip this flush
        cs->dirty.throttle_counter = 0;
    }
    // ... normal flush logic ...
}
```

## Behavior

| `TU_SKIP_STATE` | Behavior |
|----------------|----------|
| 0 (default) | Flush on every draw (normal) |
| 2 | Flush every 2nd draw (skip 50%) |
| 4 | Flush every 4th draw (skip 75%) |
| 16 | Flush every 16th draw (skip 93.75%) |

## Risk Assessment

| Risk | Severity | Mitigation |
|------|----------|-----------|
| Stale state between draws | CRITICAL | Only safe when draws within a frame use identical state |
| Missing descriptor updates | HIGH | Descriptor binding goes through separate PKT4 path |
| Incorrect rendering | HIGH | Test thoroughly per-workload |

## Use Case

Optimal for workloads where:
1. Application issues many `vkCmdDraw` calls back-to-back
2. State setup (descriptors, pipeline, push constants) doesn't change between draws
3. CPU is the bottleneck (not GPU)

**Bad fit** for workloads where:
1. State changes between every draw
2. GPU is the bottleneck (reducing CPU overhead has diminishing returns)
3. Correctness is critical (e.g. CAD, GPGPU compute)

## Activation

```bash
TU_SKIP_STATE=4 ./app  # flush dirty registers every 4th draw
```
