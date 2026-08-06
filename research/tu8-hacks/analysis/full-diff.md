# Full Diff: Fork vs Vanilla Mesa (src/freedreno/vulkan/)

Complete side-by-side of every changed function between the `turnip/gen8` fork
and upstream freedesktop.org Mesa main.

## tu_shader.cc (+169/-11)

### build_bindless() — Speculation parameter
```diff
+ bool can_speculate_descriptor = intrin->instr.pass_flags;
  nir_def *bindless =
-    nir_bindless_resource_ir3(b, 32, descriptor_idx, .desc_set = ...);
+    nir_bindless_resource_ir3(b, 32, descriptor_idx,
+      .desc_set = nir_scalar_as_uint(scalar_idx),
+      .access = can_speculate_descriptor ?
+        ACCESS_CAN_SPECULATE : (gl_access_qualifier)0);
```

### lower_load_push_constant() — Descriptor speculation analysis
```diff
+ bool can_speculate_descriptor = true;
+ if (!nir_src_is_const(deref->arr.index) ||
+     (set_layout->has_variable_descriptors &&
+      binding == set_layout->binding_count - 1) ||
+     nir_src_as_uint(deref->arr.index) >= bind_layout->array_size)
+   can_speculate_descriptor = false;
+ *descriptor_valid = !bind_layout->partially_bound && can_speculate_descriptor;
```

### lower_image_deref() — Image instruction speculation
```diff
+ if ((instr->intrinsic == nir_intrinsic_image_deref_load || ...) &&
+     descriptor_valid) {
+   nir_intrinsic_set_access(instr,
+     (gl_access_qualifier)(nir_intrinsic_access(instr) | ACCESS_CAN_SPECULATE));
+ }
```

## tu_lrz.cc (+40/-85)

### Removed: tu_lrz_emit_force_disable_for_rp()
```diff
- if (CHIP >= A7XX) {
-   tu_cs_emit_pkt7(cs, CP_REG_RMW, 3);
-   tu_cs_emit(cs, CP_REG_RMW_0_DST_REG(GRAS_SC_BIN_CNTL.reg));
-   tu_cs_emit(cs, ~0u);
-   tu_cs_emit(cs, GRAS_SC_BIN_CNTL.force_lrz_dis.value);
- } else {
-   tu6_write_lrz_reg(cmd, cs, A6XX_GRAS_LRZ_VIEW_INFO(...));
- }
```

### Removed: tu_lrz_emit_disable_write_for_rp()
```diff
- tu_cs_emit_pkt7(cs, CP_REG_RMW, 3);  // GRAS_SC_BIN_CNTL
- tu_cs_emit_pkt7(cs, CP_REG_RMW, 3);  // RB_CNTL
```

### Simplified: tu_lrz_flush_valid_at_secondary_rp_boundary()
```diff
+ tu6_write_lrz_reg(cmd, cs, A6XX_GRAS_LRZ_VIEW_INFO(
+   .base_layer = 0b11111111111, ...));
```
Replaces both `tu_lrz_emit_disable_write_for_rp()` and
`tu_lrz_emit_force_disable_for_rp()`.

### Renamed: tu_lrz_invalidate() → tu_lrz_disable_reason()
```diff
- cmd->state.lrz.valid = false;
- cmd->state.rp.lrz_disable_for_next_rp = true;
+ cmd->state.rp.lrz_disable_reason = reason;
```

## tu_device.cc (+40/-5)

### Driver name spoofing
```diff
- "turnip Mesa driver"
+ "turnip Mesa driver (whitebelyash branch)"
```

### Deck emulation
```diff
+ if (TU_DEBUG(DECK_EMU)) {
+   p->driverID = VK_DRIVER_ID_MESA_RADV;
+   snprintf(p->driverName, ..., "radv");
+   props->vendorID = 0x1002;
+   props->deviceID = 0x163F;
+   strcpy(props->deviceName, "AMD Custom GPU 0405 (RADV VANGOGH)");
+ }
```

### Forced Vulkan 1.3
```diff
- props->apiVersion = tu_has_multiview(pdevice)
-   ? VK_MAKE_VERSION(1, 3, VK_HEADER_VERSION)
-   : VK_MAKE_VERSION(1, 0, VK_HEADER_VERSION);
+ props->apiVersion = pdevice->info->chip >= 7
+   ? TU_API_VERSION
+   : VK_MAKE_VERSION(1, 3, VK_HEADER_VERSION);
```

### Device naming
```diff
+ char devname[128];
+ strcpy(devname, pdevice->name);
+ strcat(devname, " (" TUGEN8_DRV_VERSION ")");
+ strcpy(props->deviceName, devname);
```

### Disable concurrent binning
```diff
+ tu_env.debug |= TU_DEBUG_NO_CONCURRENT_BINNING;
```

## tu_knl_kgsl.cc (+24/-12)

### wait_timestamp_safe() — Mutex parameter
```diff
- wait_timestamp_safe(int fd, unsigned int context_id,
-                     unsigned int timestamp, uint64_t abs_timeout_ns)
+ wait_timestamp_safe(int fd, unsigned int context_id,
+                     unsigned int timestamp, uint64_t abs_timeout_ns,
+                     pthread_mutex_t *mutex)
```

### Mutex-protected ioctl
```diff
+ pthread_mutex_lock(mutex);
  int ret = ioctl(fd, IOCTL_KGSL_DEVICE_WAITTIMESTAMP_CTXTID, &wait);
+ pthread_mutex_unlock(mutex);
```

### EDEADLK tolerance
```diff
- if (ret == -1 && (errno == EINTR || errno == EAGAIN)) {
+ if (ret == -1 && (errno == EINTR || errno == EAGAIN || errno == EDEADLK)) {
+   if (errno == EDEADLK) sched_yield();
```

### Graceful error handling
```diff
- assert(errno == ETIMEDOUT);
- return VK_TIMEOUT;
+ if (errno == ETIMEDOUT || errno == EINVAL) {
+   return VK_TIMEOUT;
+ } else {
+   fprintf(stderr, "TU_KNL_KGSL: wait_timestamp_safe errno=%d (%s)\n", ...);
+   return VK_ERROR_DEVICE_LOST;
+ }
```

## Other Files

### tu_descriptor_set.cc (+12/-0)
Adds `partially_bound` flag initialization in descriptor set layout creation.
Includes BLAKE3 hash update for pipeline cache consistency.

### tu_descriptor_set.h (+6/-0)
Adds `bool partially_bound` field to `tu_descriptor_set_binding_layout`.

### tu_pipeline.cc (+12/-5)
- Target GPU chip_id detection (A810/A825/A829/A830)
- Conditional FDM per-layer and force_sample_interp disabling
- Register rename: `PC_RAST_STREAM_CNTL` → `VPC_UNKNOWN_9107`

### tu_query_pool.cc (+7/-70)
- Removes `raw_perfcntr_group_is_exposed()`
- Removes `perfcntr_query_capacity()`
- Removes `perfcntr_query_group_state` struct
- Simplifies counter reservation to single-pass

### tu_subsampled_image.{cc,h} (+8/-2)
Adds `can_speculate` parameter to `tu_get_subsampled_coordinates()`,
propagates `ACCESS_CAN_SPECULATE` to metadata UBO loads.

### tu_clear_blit.cc (+1/-1)
Register rename: `PC_RAST_STREAM_CNTL` → `VPC_UNKNOWN_9107`.

### tu_event.cc (+7/-8)
Moves `tu_event_map()` definition after `tu_DestroyEvent()`. No functional change.

### tu_cmd_buffer.{cc,h} (+21/-19)
- Removes `lrz_disable_for_next_rp` field and propagation
- Adds GMEM disable check for unsupported GPUs
- De-templates `tu_next_subpass_lrz` (no longer CHIP-templated)
- Simplifies secondary command buffer LRZ merge

### tu_lrz.h (-2)
De-templates `tu_lrz_flush_valid_at_secondary_rp_boundary` and
`tu_lrz_flush_valid_at_suspending_rp_boundary`.

### tu_util.{cc,h} (+2/-0)
Adds `TU_DEBUG_DECK_EMU` flag (bit 37) and `"deck_emu"` debug string.

### tu_version.h (NEW)
Contains `#define TUGEN8_DRV_VERSION "..."` for driver branding.
