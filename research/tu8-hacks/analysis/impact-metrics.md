# Impact Metrics: Fork vs Vanilla Mesa

## Summary

| Metric | Vanilla | turnip/gen8 Fork |
|--------|---------|------------------|
| Changed files in turnip driver | 0 | 17 |
| Lines changed | 0 | +348 / -220 (net +128) |
| Lines removed (complexity) | 0 | 220 |
| Gen8 code sites modified | 0 | ~25 |
| New features | 0 | Speculation, Deck spoofing, VK1.3 force |
| Bug fixes | 0 | KGSL wait, register naming |
| Removed complexity | 0 | LRZ CP_REG_RMW, query multi-pass |

## Performance Impact by Change

### Speculative Descriptors (High Impact)
- **Affected:** All draw calls with texture/UBO/image access
- **Mechanism:** GPU can prefetch descriptors instead of stalling
- **Expected gain:** 5-15% in descriptor-heavy workloads (DXVK with many textures)
- **Risk:** Low (speculation only when compiler proves safety)

### LRZ Simplification (Moderate Impact)
- **Affected:** All renderpasses with depth attachment
- **Mechanism:** PKT4 write instead of CP_REG_RMW (avoids pipeline stall)
- **Expected gain:** 1-3% per renderpass (CP_REG_RMW is expensive)
- **Risk:** Moderate (A7XX+ may need force_lrz_dis that the simple write doesn't provide)

### KGSL Wait Robustness (Stability Impact)
- **Affected:** All GPU synchronization (fences, semaphores, queue wait)
- **Mechanism:** Mutex protection prevents race conditions, graceful error handling
- **Expected gain:** Stability improvement, not performance
- **Risk:** Very low (defensive coding)

### Device Spoofing (Compatibility Impact)
- **Affected:** Game compatibility checks
- **Mechanism:** RADV/Steam Deck impersonation
- **Expected gain:** Games that refuse to run on Turnip now work
- **Risk:** Low for spoofed apps, but Vulkan 1.3 force is non-conformant

### Forced Vulkan 1.3 (Compatibility Impact)
- **Affected:** All applications checking Vulkan version
- **Mechanism:** Report 1.3 even without multiview support
- **Expected gain:** Minecraft, other VK 1.2+ games work on non-multiview GPUs
- **Risk:** Application may try to use multiview and get unexpected behavior

## Code Quality Impact

### Removed Complexity
- **85 lines** removed from `tu_lrz.cc` (CP_REG_RMW paths, multi-chip templating)
- **70 lines** removed from `tu_query_pool.cc` (multi-pass counter system)
- **2 template instantiations** eliminated from `tu_lrz.h`

### Added Complexity
- **169 lines** added to `tu_shader.cc` (speculation safety analysis)
- **40 lines** added to `tu_device.cc` (spoofing + branding + debug flags)
- **24 lines** added to `tu_knl_kgsl.cc` (mutex + error handling)

### Net Complexity: Reduced
Removed 220 lines, added 348 → net +128 LOC. But the added code is straightforward
(conditionals, parameters) while the removed code was complex (multi-pass allocation,
CP_REG_RMW sequences, template metaprogramming). **Maintainability improved.**

## Stability Assessment

### Improvements
1. **KGSL error handling:** No more asserts on unexpected ioctl errors
2. **Mutex protection:** Thread-safe timestamp waiting
3. **EDEADLK handling:** Proper deadlock avoidance protocol
4. **GMEM disable:** Clean fallback for buggy GPUs

### Regressions (Potential)
1. **LRZ on A7XX+:** Simple `GRAS_LRZ_VIEW_INFO` write may not fully disable LRZ
2. **VK 1.3 conformance:** Exposing features that may not work
3. **DECK_EMU bit collision:** Debug bit 37 shared with COMPUTE_ROUND_ROBIN
4. **A830 chip_id:** Suspicious `0xffff44050000 || 0x44050001` — first always truthy

## Build Impact

The fork's `tu_version.h` and driver branding changes have no build impact —
they compile the same as vanilla. The `mesa/build.sh` in this repo cross-compiles
using the NDK without any fork-specific flags.

## Comparison with apply-all-hacks.sh

| Aspect | Fork Changes | apply-all-hacks.sh |
|--------|-------------|-------------------|
| Scope | 17 files, 603 lines | 3 files, ~140 lines injected |
| Permanence | Always active in fork code | Active based on env vars |
| Main benefit | Speculation + stability + compatibility | PKT4 deduplication + barrier reduction |
| Overhead | Compile-time only | 21KB per tu_cs + runtime checks |
| Risk | Low (well-tested in fork) | High (aggressive, env-var gated) |
| Relationship | Base layer | Optional experimental layer on top |
