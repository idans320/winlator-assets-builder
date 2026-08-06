# Change 02: LRZ (Low-Resolution Z) Simplification

**Files:** `tu_lrz.cc` (+40/-85), `tu_lrz.h` (+0/-2), `tu_cmd_buffer.cc` (+21/-18), `tu_cmd_buffer.h` (+0/-1)  
**Net:** -45 lines

## What is LRZ?

Low-Resolution Z is a hardware optimization in Adreno GPUs. A low-resolution
Z-buffer is maintained in fast on-chip memory. Before running the expensive
fragment shader, the GPU checks the LRZ buffer — if the fragment is certainly
occluded, the shader is skipped entirely. This can **double** fragment throughput
in geometry-heavy scenes.

## Vanilla Complexity

Upstream Mesa has a sophisticated LRZ management system with three disable mechanisms:

### 1. lrz_disable_for_next_rp (`tu_cmd_buffer.h`)
A boolean flag set when LRZ validity has been permanently lost. Used to force-disable
LRZ at the start of the next renderpass via `CP_REG_RMW`.

### 2. tu_lrz_emit_force_disable_for_rp (`tu_lrz.cc`, removed)
```c
// On A7XX+: use CP_REG_RMW to set GRAS_SC_BIN_CNTL.force_lrz_dis
tu_cs_emit_pkt7(cs, CP_REG_RMW, 3);
tu_cs_emit(cs, CP_REG_RMW_0_DST_REG(reg));
tu_cs_emit(cs, ~0u);
tu_cs_emit(cs, reg.value);

// On A6XX: write invalid GRAS_LRZ_VIEW_INFO
tu6_write_lrz_reg(cmd, cs, A6XX_GRAS_LRZ_VIEW_INFO(
    .base_layer = 0b11111111111, ...));
```

### 3. tu_lrz_emit_disable_write_for_rp (`tu_lrz.cc`, removed)
Conditionally disables LRZ writes during sysmem or per-tile rendering via
dual `CP_REG_RMW` to `GRAS_SC_BIN_CNTL` + `RB_CNTL`.

## Fork Simplification

The fork removes **both** CP_REG_RMW-based mechanisms and uses a single simple
approach everywhere:

```c
// All chips use the A6XX method — write invalid LRZ_VIEW_INFO
tu6_write_lrz_reg(cmd, cs, A6XX_GRAS_LRZ_VIEW_INFO(
    .base_layer = 0b11111111111,
    .layer_count = 0b11111111111,
    .base_mip_level = 0b1111,
));
```

### Removed State
- `lrz_disable_for_next_rp` field from `tu_render_pass_state`
- `tu_lrz_emit_force_disable_for_rp<CHIP>()` (templated function)
- `tu_lrz_emit_disable_write_for_rp<CHIP>()` (templated function)
- `lrz_disable_for_next_rp` propagation in secondary command buffer merge

### De-templatized Functions
`tu_lrz_flush_valid_at_secondary_rp_boundary` and
`tu_lrz_flush_valid_at_suspending_rp_boundary` no longer require chip template
instantiation — they use the uniform `tu6_write_lrz_reg` approach.

### lrz_invalidate → lrz_disable_reason
The `tu_lrz_invalidate()` function (which set both `valid=false` and
`lrz_disable_for_next_rp=true`) is replaced with `tu_lrz_disable_reason()`,
which only sets the reason string and dirty flag:

```c
// Vanilla:
cmd->state.lrz.valid = false;
cmd->state.rp.lrz_disable_for_next_rp = true;

// Fork:
cmd->state.rp.lrz_disable_reason = reason;
cmd->state.rp.lrz_disabled_at_draw = cmd->state.rp.drawcall_count;
```

## Why This Change?

1. **CP_REG_RMW is expensive** — it requires a GPU pipeline stall. The simple
   `GRAS_LRZ_VIEW_INFO` write is a regular PKT4 that goes through the normal
   command stream pipeline.

2. **Fewer chip-specific paths** — the fork uses one code path for all chips
   (A6XX-A8XX), reducing maintenance burden and potential chip-specific bugs.

3. **Simpler state tracking** — removing `lrz_disable_for_next_rp` eliminates
   a state variable that could get out of sync with the GPU's actual LRZ state.

## Risk

The A6XX LRZ disable method (writing max values to `GRAS_LRZ_VIEW_INFO`) may
not fully disable LRZ on A7XX+ chips. The upstream `CP_REG_RMW` approach is
more thorough. If LRZ artifacts appear (z-fighting, missing fragments), this
change is the first suspect.

## Visual Comparison

```
VANILLA LRZ DISABLE PATH:
  LRZ invalidated
    → set lrz_disable_for_next_rp = true
    → next renderpass:
        if A7XX+:
          CP_REG_RMW (stall CP, read GRAS_SC_BIN_CNTL, modify, write back)
        else (A6XX):
          PKT4 write to GRAS_LRZ_VIEW_INFO

FORK LRZ DISABLE PATH:
  LRZ invalidated
    → set lrz_disable_reason = "..."
    → next renderpass:
        PKT4 write to GRAS_LRZ_VIEW_INFO (all chips)
```
