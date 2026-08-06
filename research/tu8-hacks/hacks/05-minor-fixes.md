# Change 05: Minor Fixes & Cleanups

**Files:** `tu_clear_blit.cc`, `tu_pipeline.cc`, `tu_query_pool.cc`, `tu_event.cc`, `tu_cmd_buffer.cc`

## A6XX Register Name Correction

**Affects:** `tu_clear_blit.cc` (+1/-1), `tu_pipeline.cc` (+7/-5)

The register at offset `0x9107` was named `PC_RAST_STREAM_CNTL` in upstream Mesa,
implying it belongs to the Primitive Controller. Physical register documentation
shows it's actually a VPC (Vertex Parameter Cache) register:

```c
// Vanilla (wrong hardware block):
tu_cs_emit_regs(cs, PC_RAST_STREAM_CNTL(CHIP,
    .stream = rs->rasterization_stream,
    .discard = rs->rasterizer_discard_enable));

// Fork (corrected to VPC):
tu_cs_emit_regs(cs, VPC_UNKNOWN_9107(CHIP,
    .raster_discard = rs->rasterizer_discard_enable));
```

The field name also changes from `.discard` to `.raster_discard` to better
describe the function. This affects both the clear/blit path (`tu_clear_blit.cc`)
and the pipeline emission path (`tu_pipeline.cc`).

## Target GPU Flags

**Affects:** `tu_pipeline.cc` (+7/-5)

```c
const bool is_a810 = chip_id == 0x44010000ull;
const bool is_a825 = chip_id == 0x44030000ull;
const bool is_a829 = chip_id == 0x44030A20ull;
const bool is_a830 = chip_id == 0xffff44050000 || 0x44050001;
const bool is_target_gpu = is_a810 || is_a825 || is_a829 || is_a830;
```

Used to:
- Disable FDM per-layer on target GPUs (`fdm_per_layer = is_target_gpu ? false : ...`)
- Disable force_sample_interp on target GPUs (`force_sample_interp = is_target_gpu ? false : ...`)

Note: the A830 chip_id check `0xffff44050000 || 0x44050001` is suspicious — the
first value will always be truthy (non-zero), making the second unreachable.

## Perf Query Simplification

**Affects:** `tu_query_pool.cc` (+7/-70)

Removes the multi-pass performance counter system. In vanilla, hardware
performance counters for a single GPU block could be split across multiple
render passes (each pass allocates a subset of counters). The fork simplifies:

- **Removed:** `raw_perfcntr_group_is_exposed()` — BV counters are no longer filtered
- **Removed:** `perfcntr_query_capacity()` — all counters available per pass
- **Removed:** `perfcntr_query_group_state` struct and pass-based allocation
- **Simplified counter reservation:** each countable gets its own `fd_perfcntr_reserve()` call
- **Simplified counter release:** no pass-0 alias tracking needed

**Before (vanilla):** `fd_perfcntr_reserve(device->perfcntrs, group, NULL)`  
**After (fork):** `fd_perfcntr_reserve(device->perfcntrs, group, countable)`

## tu_event.cc Cleanup

**Affects:** `tu_event.cc` (+7/-8)

Moves `tu_event_map()` from a file-static forward-declared function to after
`tu_DestroyEvent()`, using a more conventional definition order (define before
first use). No functional change.

## GMEM Disable for Unsupported GPUs

**Affects:** `tu_cmd_buffer.cc` (+3/-0)

```c
bool no_gmem = cmd->device->physical_device->dev_info.props.disable_gmem;
if (no_gmem) {
    cmd->state.rp.gmem_disable_reason = "Unsupported GPU";
    return true;
}
```

Allows driver-level control over GMEM (on-chip tile buffer) usage. When a GPU's
GMEM is unreliable or insufficient, rendering falls back to system memory
(sysmem mode), which is slower but correct.
