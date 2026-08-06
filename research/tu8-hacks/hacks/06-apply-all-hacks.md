# Change 06: The apply-all-hacks.sh Layer (NOT in the fork)

**This is NOT part of the turnip/gen8 fork.** It's a separate experimental
script applied post-clone before building.

## What It Is

`apply-all-hacks.sh` is a Bash+Python script that injects **6 additional hacks**
into the cloned Mesa source after the fork's changes are in place:

| Hack | Mechanism | Env Var |
|------|-----------|---------|
| Dirty-bit shadow registers | `tu_cs_set_register` caches writes in shadow[], `tu_cs_flush_dirty` emits compact PKT4 | (always on A8XX+) |
| YOLO_SYNC | Strip per-draw barriers, mega-flush at EndRenderPass | `TU_YOLO_SYNC=1` |
| STATE_THROTTLE | Skip flush_dirty every N draws | `TU_SKIP_STATE=N` |
| REG_BLAST | Dump 32-reg blocks (skip ffs scan) | `TU_REG_BLAST=1` |
| BARRIER_COALESCE | Skip flush when flush_bits==0 | `TU_BARRIER_COALESCE=1` |
| Push constant cache | Skip vkCmdPushConstants when unchanged | (probes only) |

## Files Modified

```
tu_cs.h           → +120 lines (struct dirty + 4 inline functions)
tu_cs.cc          → +15 lines (init, free, #include <stdlib.h>)
tu_cmd_buffer.cc  → +6 lines (YOLO barrier bypass)
```

## Relationship to the Fork

```
freedesktop.org/mesa (vanilla)
    │
    ├── turnip/gen8 fork changes (603 lines, 17 files)
    │     Speculative descriptors, LRZ simplification, device spoofing,
    │     KGSL robustness, register fixes, query simplification
    │
    └── apply-all-hacks.sh (injected on top, +140 lines, 3 files)
          Dirty-shadow registers, YOLO_SYNC, barrier coalescing, etc.
```

The fork changes are **always active**. The `apply-all-hacks.sh` hacks are
**conditionally active** — the dirty-shadow system runs automatically on A8XX+,
while YOLO_SYNC, STATE_THROTTLE, REG_BLAST, and BARRIER_COALESCE are
controlled by environment variables at runtime.

## Which to Use?

- **Fork changes:** Always apply — they're in the turnip/gen8 branch
- **apply-all-hacks.sh:** Experimental — for benchmarking and performance testing.
  Run after cloning if you want the extra optimization layer.

## Building with Apply-All-Hacks

```bash
# 1. Clone the fork
git clone --depth 1 --branch turnip/gen8 \
    https://github.com/whitebelyash/mesa-unified.git mesa/workdir/mesa

# 2. Apply experimental hacks
./apply-all-hacks.sh

# 3. Build
devbox run -- bash mesa/build.sh

# 4. Test with environment variables
TU_YOLO_SYNC=1 TU_SKIP_STATE=4 ./app
```
