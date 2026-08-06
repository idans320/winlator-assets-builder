# TU8 Hacks: whitebelyash/mesa-unified turnip/gen8 vs Vanilla Mesa

Complete diff analysis between the **turnip/gen8** fork (`whitebelyash/mesa-unified`)
and **upstream freedesktop.org Mesa main** (as of Mesa 26.3.0-devel).

## Overview

**603 lines changed across 17 files** in `src/freedreno/vulkan/`. The fork contains
real driver changes — NOT script-injected hacks. The `apply-all-hacks.sh` script
is a *separate* experimental injection layer applied post-clone.

## Quick Navigation

| Section | Description |
|---------|-------------|
| [Change Matrix](#change-matrix) | Every changed file with line counts |
| [Major Changes](#major-changes) | Deep dive into 9 categories of changes |
| [Vanilla vs Fork Architecture](#vanilla-vs-fork-architecture) | Code flow comparison |
| [Go Graph Analysis](analysis/gen8-neuron-paths.md) | Gen8 code site mapping, neuron paths |
| [Impact Assessment](analysis/impact-metrics.md) | Performance, stability, code quality |
| [Full Diff](analysis/full-diff.md) | Complete side-by-side diff of all 17 files |
| [SIMD Heresies](hacks/07-simd-heresies.md) | 9 CPU microarchitecture abuse attacks |

## Change Matrix

```
File                            +lines  -lines  Net     Category
───────────────────────────────────────────────────────────────
tu_shader.cc                    169     11      +158    Speculative descriptor access
tu_device.cc                    40      5       +35     Device spoofing + Vulkan 1.3 force
tu_knl_kgsl.cc                  24      12      +12     KGSL wait robustness
tu_descriptor_set.cc            12      0       +12     Partially-bound descriptor support
tu_descriptor_set.h             6       0       +6      Descriptor layout metadata
tu_subsampled_image.cc          6       1       +5      Speculative subsampled loads
tu_subsampled_image.h           2       1       +1      API change for speculation
tu_util.cc                      1       0       +1      DECK_EMU debug flag
tu_util.h                       1       0       +1      DECK_EMU enum bit
tu_version.h                    1       —       +1      New file: driver version string
tu_clear_blit.cc                1       1       0       A6XX register fix
tu_pipeline.cc                  12      5       +7      Target GPU flags + register fix
tu_event.cc                     7       8       -1      Function reorder
tu_cmd_buffer.h                 0       1       -1      Remove lrz_disable_for_next_rp
tu_lrz.h                        0       2       -2      De-template LRZ functions
tu_cmd_buffer.cc                21      18      +3      LRZ simplification + GMEM disable
tu_lrz.cc                       40      85      -45     LRZ code removal/simplification
tu_query_pool.cc                7       70      -63     Perf query simplification
───────────────────────────────────────────────────────────────
TOTAL                           348     220     +128     (net: 603 changed lines)
```

## Major Changes

### 1. Speculative Descriptor Access (+158 lines, tu_shader.cc)

**The largest single change.** Adds `ACCESS_CAN_SPECULATE` to bindless descriptor
loads. When descriptor indexing is static and the binding is fully bound, the
compiler marks the access as speculatable — the GPU can prefetch descriptors
without waiting for all dependencies to resolve.

```c
// Vanilla: always safe, no speculation
nir_def *bindless = nir_bindless_resource_ir3(b, 32, desc_offset,
    .desc_set = set);

// Fork: speculative when safe
bool can_speculate = !nir_src_is_const(deref->arr.index) ||
    (set_layout->has_variable_descriptors && ...);
*descriptor_valid = !bind_layout->partially_bound && can_speculate;
nir_def *bindless = nir_bindless_resource_ir3(b, 32, desc_offset,
    .desc_set = set,
    .access = can_speculate ? ACCESS_CAN_SPECULATE : 0);
```

**Why it matters:** Descriptor loads are a common pipeline stall point. Allowing
speculation means the GPU can fetch descriptors in parallel with other work,
reducing draw-call latency. The safety check ensures speculation is only done
when the descriptor index is provably valid.

### 2. LRZ (Low-Resolution Z) Simplification (-45 lines, tu_lrz.cc + tu_lrz.h)

The fork removes two complex LRZ disable mechanisms:

**Removed functions:**
- `tu_lrz_emit_force_disable_for_rp<CHIP>()` — emitted `CP_REG_RMW` on A7XX+ or
  `GRAS_LRZ_VIEW_INFO` on A6XX to force-disable LRZ for next renderpass
- `tu_lrz_emit_disable_write_for_rp<CHIP>()` — emitted dual `CP_REG_RMW` to disable
  LRZ write (GRAS_SC_BIN_CNTL + RB_CNTL)

**Replaced with:** Simple A6XX-style `GRAS_LRZ_VIEW_INFO` writes on all chips:
```c
// Fork: always use simple register write
tu6_write_lrz_reg(cmd, cs, A6XX_GRAS_LRZ_VIEW_INFO(
    .base_layer = 0b11111111111,
    .layer_count = 0b11111111111,
    .base_mip_level = 0b1111,
));
```

**Also removed:** `lrz_disable_for_next_rp` flag from `tu_render_pass_state` —
the fork uses simpler invalidation logic.

**Also de-templatized:** `tu_lrz_flush_valid_at_secondary_rp_boundary` and
`tu_lrz_flush_valid_at_suspending_rp_boundary` are no longer chip-templated,
reducing code duplication.

### 3. Device/Driver Spoofing (+35 lines, tu_device.cc)

**Steam Deck Emulation** (`TU_DEBUG=deck_emu`):
```c
if (TU_DEBUG(DECK_EMU)) {
    p->driverID = VK_DRIVER_ID_MESA_RADV;
    snprintf(p->driverName, ..., "radv");
    props->vendorID = 0x1002;   // AMD
    props->deviceID = 0x163F;   // Van Gogh (Steam Deck APU)
    strcpy(props->deviceName, "AMD Custom GPU 0405 (RADV VANGOGH)");
}
```

Spoofs the Turnip driver as AMD RADV (Steam Deck GPU) for game compatibility.
Many games check for RADV specifically and refuse to run on Turnip.

**Forced Vulkan 1.3:**
```c
// Vanilla: only VK 1.3 on devices with multiview
props->apiVersion = tu_has_multiview(pdevice)
    ? VK_MAKE_VERSION(1, 3, ...)
    : VK_MAKE_VERSION(1, 0, ...);

// Fork: VK 1.3 on all gen7+ devices
props->apiVersion = pdevice->info->chip >= 7
    ? TU_API_VERSION
    : VK_MAKE_VERSION(1, 3, ...);
```

Comment in code: "Minecraft checks VK1.2 presence and refuses to start on VK1.0"

**Driver branding:**
- Driver name: `"turnip Mesa driver"` → `"turnip Mesa driver (whitebelyash branch)"`
- Device name appended with `(TUGEN8_DRV_VERSION)` from `tu_version.h`

**Concurrent binning disabled:**
```c
tu_env.debug |= TU_DEBUG_NO_CONCURRENT_BINNING;
```

### 4. KGSL Wait Robustness (+12 lines, tu_knl_kgsl.cc)

**Mutex protection:** All `wait_timestamp_safe()` calls now take `&device->submit_mutex`
to prevent races between submission and wait.

**Error handling improvements:**
```c
// Vanilla: assert crash on non-timeout errors
assert(errno == ETIMEDOUT);
return VK_TIMEOUT;

// Fork: log and report device lost on unexpected errors
if (errno == ETIMEDOUT || errno == EINVAL) {
    return VK_TIMEOUT;
} else {
    fprintf(stderr, "TU_KNL_KGSL: wait_timestamp_safe errno=%d (%s)\n",
            errno, strerror(errno));
    return VK_ERROR_DEVICE_LOST;
}
```

**EDEADLK handling:** On `EDEADLK`, calls `sched_yield()` before retrying — handles
the mutex deadlock avoidance protocol.

### 5. Partially-Bound Descriptors (+12 lines, tu_descriptor_set.cc/h)

Adds `partially_bound` flag to `tu_descriptor_set_binding_layout`:
```c
set_layout->binding[b].partially_bound =
    (pCreateInfo->flags & VK_DESCRIPTOR_SET_LAYOUT_CREATE_DESCRIPTOR_BUFFER_BIT_EXT) ||
    (variable_flags && (binding_flags & VK_DESCRIPTOR_BINDING_PARTIALLY_BOUND_BIT));
```

This feeds into the speculative descriptor access logic — when a binding is
partially bound, the driver cannot speculate access because some descriptors
may be invalid.

Also included in the pipeline cache hash (BLAKE3) to prevent cache collisions
when this flag differs.

### 6. A6XX Register Fix (tu_clear_blit.cc, tu_pipeline.cc)

**Replaces `PC_RAST_STREAM_CNTL` with `VPC_UNKNOWN_9107` on A6XX:**
```c
// Vanilla:
tu_cs_emit_regs(cs, PC_RAST_STREAM_CNTL(CHIP,
    .stream = rs->rasterization_stream,
    .discard = rs->rasterizer_discard_enable));

// Fork:
tu_cs_emit_regs(cs, VPC_UNKNOWN_9107(CHIP,
    .raster_discard = rs->rasterizer_discard_enable));
```

This is a register name correction — `PC_RAST_STREAM_CNTL` at offset 0x9107 is
actually a VPC register on A6XX, confirmed by the Adreno register documentation.
The upstream name was misleading.

### 7. Target GPU Optimizations (tu_pipeline.cc)

**Disables FDM per-layer on target GPUs:**
```c
const bool is_target_gpu = is_a810 || is_a825 || is_a829 || is_a830;
keys[last_pre_rast_stage].fdm_per_layer =
    is_target_gpu ? false : builder->fdm_per_layer;
```

Fragment Density Map per-layer requires hardware support that may be buggy on
these specific GPUs. The fork disables it rather than risking rendering artifacts.

**Disables force_sample_interp on target GPUs:**
```c
keys[MESA_SHADER_FRAGMENT].force_sample_interp =
    is_target_gpu ? false : (!builder->rasterizer_discard && msaa_info && ...);
```

Force sample interpolation can cause performance regressions on A8XX.

### 8. Perf Query Simplification (-63 lines, tu_query_pool.cc)

Removes the multi-pass counter system. In vanilla, counters for a group can be
split across multiple passes (each pass gets a subset of counters). The fork
simplifies to single-pass counter allocation:

```c
// Vanilla: complex pass-based counter allocation
struct perfcntr_query_group_state *group_state = rzalloc_array(...);
if (state.next_counter == available_counters) {
    state.next_counter = 0; state.pass++;
}
perf_query->data[i].pass = state.pass;

// Fork: simple direct reservation
perf_query->data[i].counter =
    fd_perfcntr_reserve(device->perfcntrs, group, countable);
```

### 9. GMEM Disable for Unsupported GPUs (tu_cmd_buffer.cc)

```c
bool no_gmem = cmd->device->physical_device->dev_info.props.disable_gmem;
if (no_gmem) {
    cmd->state.rp.gmem_disable_reason = "Unsupported GPU";
    return true;
}
```

Allows GMEM (on-chip tile buffer) to be selectively disabled via physical device
properties, enabling sysmem-only rendering on GPUs with buggy GMEM.

## Files NOT Changed (Vanilla = Fork)

The following 56 files in `src/freedreno/vulkan/` are identical between the
fork and upstream Mesa — these were NOT modified by the turnip/gen8 branch:
`tu_cs.h`, `tu_cs.cc` (no dirty-shadow system), `tu_autotune.cc`, `tu_image.cc`,
`tu_pass.cc`, `tu_rmv.cc`, `tu_wsi.cc`, etc.

**This confirms the dirty-shadow register system, YOLO_SYNC, barrier coalescing,
etc. are NOT part of the fork — they're separate experimental hacks applied by
`apply-all-hacks.sh` post-clone.**

## Directory

```
tu8-hacks/
├── README.md                         # Overview, change matrix, major changes
├── hacks/
│   ├── 01-speculative-descriptors.md  # +158 lines, ACCESS_CAN_SPECULATE
│   ├── 02-lrz-simplification.md       # -45 lines, CP_REG_RMW removal
│   ├── 03-device-spoofing.md          # +35 lines, Deck emu + VK1.3 force
│   ├── 04-kgsl-wait-robustness.md     # +12 lines, mutex + error handling
│   ├── 05-minor-fixes.md              # Register rename, target GPU, query simplify
│   ├── 06-apply-all-hacks.md          # Separate script: dirty-shadow, YOLO, etc.
│   ├── 07-simd-heresies.md            # CPU microarchitecture abuse (9 attacks)
│   └── 08-bandwidth-starvation.md     # DRAM bandwidth denial (planned)
├── analysis/
│   ├── gen8-neuron-paths.md           # Go graph: changed code → gen8 sites
│   ├── impact-metrics.md              # Performance, stability, complexity
│   └── full-diff.md                   # Side-by-side diff of all 17 files
└── patches/
    ├── 0003-tu-cs-reg-cache.patch     # Hash-table regcache variant (legacy)
    └── 0004-barrier-coalesce.patch    # Barrier coalesce patch
```
