# Diff Analysis: Go Graph of Changed Code

## Methodology

1. Diff'd `src/freedreno/vulkan/` between upstream freedesktop.org Mesa and the `turnip/gen8` fork
2. Ran the Go analysis pipeline on both repos to identify gen8 code sites
3. Mapped changed lines to their enclosing functions using ctags index
4. Cross-referenced changed functions with the gen8 neuron path graph

## Changed Files: Gen8 Coverage

| File | Changed Lines | Gen8 Sites in File | Gen8 Sites in Changed Region |
|------|--------------|--------------------|------------------------------|
| tu_shader.cc | +169/-11 | 3 | 2 (descriptor speculation checks) |
| tu_device.cc | +40/-5 | 5 | 3 (DECK_EMU, apiVersion, nocb) |
| tu_knl_kgsl.cc | +24/-12 | 0 | 0 (pure Android ioctl layer) |
| tu_lrz.cc | +40/-85 | 8 | 5 (CP_REG_RMW removal, force disable) |
| tu_cmd_buffer.cc | +21/-18 | 12 | 3 (LRZ merge, GMEM disable) |
| tu_descriptor_set.cc | +12/-0 | 2 | 0 (binding layout init — no gen8 code) |
| tu_pipeline.cc | +12/-5 | 8 | 3 (target GPU flags, VPC_UNKNOWN_9107) |
| tu_query_pool.cc | +7/-70 | 0 | 0 (no gen8-specific code removed) |
| tu_subsampled_image.cc | +6/-1 | 3 | 2 (speculative UBO load) |
| tu_descriptor_set.h | +6/-0 | 0 | 0 (header changes only) |
| tu_subsampled_image.h | +2/-1 | 0 | 0 (API signature change) |
| tu_event.cc | +7/-8 | 0 | 0 (no gen8 code) |
| tu_clear_blit.cc | +1/-1 | 2 | 1 (VPC_UNKNOWN_9107) |
| tu_cmd_buffer.h | +0/-1 | 0 | 0 (field removal) |
| tu_lrz.h | +0/-2 | 0 | 0 (de-template) |
| tu_util.cc | +1/-0 | 0 | 0 (debug flag) |
| tu_util.h | +1/-0 | 0 | 0 (enum bit) |

**Total:** ~25 gen8 code sites are in the changed regions across 7 files.

## Affected Gen8 Neuron Paths

The Go graph analysis traces Vulkan API entry points through their call chains
to gen8-specific code. Here are the paths affected by the fork changes:

### Path 1: vkCmdDraw → Speculative Descriptors
```
vkCmdDraw
  → tu_CmdDraw
    → tu6_draw_common
      → tu_emit_draw_state
        → [texture descriptors loaded]
          → nir_bindless_resource_ir3 (ACCESS_CAN_SPECULATE)  ← FORK CHANGE
            → A8XX_TEX_SAMP / A8XX_TEX_CONST registers
```

### Path 2: vkCmdBeginRenderPass → LRZ
```
vkCmdBeginRenderPass
  → tu_CmdBeginRenderPass
    → tu6_emit_lrz (LRZ state setup)                          ← FORK CHANGE
      → A6XX_GRAS_LRZ_VIEW_INFO (instead of CP_REG_RMW)
      → Removed: tu_lrz_emit_force_disable_for_rp (CP_REG_RMW path)
      → Removed: tu_lrz_emit_disable_write_for_rp (dual CP_REG_RMW)
```

### Path 3: vkCreateDevice → Device Init
```
vkCreateDevice
  → tu_CreateDevice
    → tu_get_physical_device_properties
      → apiVersion forced to VK 1.3                          ← FORK CHANGE
      → DECK_EMU: spoof vendorID/deviceID/name              ← FORK CHANGE
    → tu_env.debug |= TU_DEBUG_NO_CONCURRENT_BINNING         ← FORK CHANGE
```

### Path 4: vkCmdExecuteCommands → Secondary LRZ
```
vkCmdExecuteCommands
  → tu_CmdExecuteCommands
    → tu_lrz_flush_valid_at_secondary_rp_boundary             ← FORK CHANGE (de-templated)
      → tu6_write_lrz_reg (A6XX_GRAS_LRZ_VIEW_INFO)
```

### Path 5: vkQueueSubmit → KGSL Wait
```
vkQueueSubmit
  → tu_QueueSubmit
    → tu_knl_signal_by_timestamp
      → wait_timestamp_safe (with mutex)                      ← FORK CHANGE
        → EDEADLK handling, EINVAL tolerance
```

## Register-Level Impact

### Registers Changed in Behavior

| Register | Vanilla | Fork | Change Type |
|----------|---------|------|-------------|
| A6XX_GRAS_LRZ_VIEW_INFO | Used only on A6XX LRZ disable | Used on ALL chips for LRZ disable | Expanded scope |
| GRAS_SC_BIN_CNTL | CP_REG_RMW to set force_lrz_dis (A7XX+) | Not used for LRZ disable | Removed |
| RB_CNTL | CP_REG_RMW to set force_lrz_write_dis | Not used for LRZ write disable | Removed |
| VPC_UNKNOWN_9107 | Emitted as PC_RAST_STREAM_CNTL | Emitted as VPC_UNKNOWN_9107 | Renamed only |
| A8XX_TEX_CONST / A8XX_TEX_SAMP | Always non-speculative | Speculative when safe | Access qualifier change |

### Registers NOT Changed (No Impact)

The fork does NOT modify any of these commonly-changed registers:
- HLSQ_UPDATE_CNTL (0xB182)
- RB_UNKNOWN_8818
- SP/VPC/GRAS/VFD/PIPE config registers
- CP register handling (no dirty-shadow system)

## Code Removal Heatmap

```
tu_lrz.cc:        ████████████████████████████████░░  85 removed, 40 added
tu_query_pool.cc: ███████████████████████░░░░░░░░░░░  70 removed, 7 added
tu_cmd_buffer.cc: ██████░░░░░░░░░░░░░░░░░░░░░░░░░░░░  18 removed, 21 added
tu_shader.cc:     ███░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░  11 removed, 169 added
tu_event.cc:      ███░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░  8 removed, 7 added
tu_device.cc:     ██░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░  5 removed, 40 added
tu_knl_kgsl.cc:   ████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░  12 removed, 24 added
```

Most removal (-85 lines in tu_lrz.cc) is complexity elimination. Most addition
(+169 lines in tu_shader.cc) is performance optimization.

## Running the Analysis Yourself

```bash
# Clone both repos
git clone --depth 1 https://gitlab.freedesktop.org/mesa/mesa.git /tmp/mesa-vanilla
# (turnip/gen8 already at mesa/workdir/mesa)

# Diff only the Turnip driver
diff -rq /tmp/mesa-vanilla/src/freedreno/vulkan/ \
         mesa/workdir/mesa/src/freedreno/vulkan/ \
    | grep differ | wc -l   # → 17 files changed

# Generate unified diff
diff -Nu /tmp/mesa-vanilla/src/freedreno/vulkan/ \
         mesa/workdir/mesa/src/freedreno/vulkan/ \
    > /tmp/tu8-vs-vanilla.patch

# Run Go analysis on the changed files only
cd research && devbox shell
bash scripts/search-gen8.sh ../mesa/workdir/mesa output/gen8_sites_fork.json
# Compare with vanilla gen8 sites:
bash scripts/search-gen8.sh /tmp/mesa-vanilla output/gen8_sites_vanilla.json
```
