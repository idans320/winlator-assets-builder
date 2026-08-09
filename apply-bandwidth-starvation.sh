#!/bin/bash -e
# Bandwidth Starvation — Hostile DRAM Bus Denial for Mesa Turnip
# ============================================================================
# Four always-on attacks that starve the DRAM bus by silently overriding
# Vulkan application memory and state requests before PM4 compilation.
# The engine has no idea this is happening.
#
# 1. Forced UBWC Injection — compress every allocation
# 2. VRS Hijacking — force 4×4 shading rate
# 3. Depth Format Truncation — D32→D16, halve depth bandwidth
# 4. Global LOD Bias — drop 4 mip levels, max out L1 texture cache

set -e
cd "$(dirname "$0")/mesa/workdir/mesa"

echo "=== Bandwidth Starvation — DRAM Bus Denial Injection ==="

python3 << 'PYEOF'
VULKAN = "src/freedreno/vulkan"

def read(path):
    with open(path) as f: return f.read()

def write(path, s):
    with open(path, 'w') as f: f.write(s)

# ===========================================================================
# HACK 1: Forced UBWC Injection
# tu_image.cc — nuke ubwc_possible(), strip force_linear_tile,
# remove assert(!force_linear_tile)
# ===========================================================================
img = read(f"{VULKAN}/tu_image.cc")

# 1a: Nuke ubwc_possible — return true unconditionally
old_possible = '''{
   /* TODO: enable for a702 */
   if (info->props.is_a702)
      return false;'''

new_possible = '''{
   /* BANDWIDTH HACK: Force UBWC on every allocation.  The engine cannot
    * opt out.  CPU readback will return garbled UBWC metadata.  That's
    * the cost of survival.
    */
   (void)format; (void)type; (void)flags; (void)usage;
   (void)stencil_usage; (void)info; (void)samples; (void)mip_levels;
   (void)use_z24uint_s8uint;
   return true;

   /* TODO: enable for a702 */
   if (info->props.is_a702)
      return false;'''

img = img.replace(old_possible, new_possible, 1)

# 1b: Strip force_linear_tile — UBWC even on LINEAR tiling
old_linear = '''   /* use linear tiling if requested */
   if (pCreateInfo->tiling == VK_IMAGE_TILING_LINEAR) {
      force_linear_tile = true;
   }'''

new_linear = '''   /* BANDWIDTH HACK: Strip force_linear_tile.  UBWC compression
    * overrides the application's tiling preference.  Every image is
    * allocated as UBWC-compressed regardless of tiling flags.
    */
   /* force_linear_tile disabled — UBWC dominates */'''

img = img.replace(old_linear, new_linear, 1)

# 1c: Remove assert(!force_linear_tile) at UBWC enable
old_assert = '''   bool force_ubwc = false;
   if (modifier == DRM_FORMAT_MOD_QCOM_COMPRESSED) {
      assert(!force_linear_tile);
      ubwc_enabled = true;
      force_ubwc = true;
   }'''

new_assert = '''   /* BANDWIDTH HACK: UBWC forced on all images.  Remove the assert
    * that would fire when force_linear_tile is stripped above.  The GPU
    * allocator gets the DRM_FORMAT_MOD_QCOM_COMPRESSED modifier regardless.
    */
   bool force_ubwc = false;
   if (modifier == DRM_FORMAT_MOD_QCOM_COMPRESSED || true) {
      ubwc_enabled = true;
      force_ubwc = true;
      image->vk.drm_format_mod = DRM_FORMAT_MOD_QCOM_COMPRESSED;
   }'''

img = img.replace(old_assert, new_assert, 1)

# Also hook tu_CreateImage to force DRM_FORMAT_MOD_QCOM_COMPRESSED modifier
old_mod = '''   if (pCreateInfo->tiling == VK_IMAGE_TILING_DRM_FORMAT_MODIFIER_EXT) {'''

new_mod = '''   /* BANDWIDTH HACK: Force QCOM_COMPRESSED modifier for OPTIMAL tiling.
    * If the app didn't explicitly request linear, it gets UBWC.
    */
   if (pCreateInfo->tiling == VK_IMAGE_TILING_OPTIMAL)
      modifier = DRM_FORMAT_MOD_QCOM_COMPRESSED;

   if (pCreateInfo->tiling == VK_IMAGE_TILING_DRM_FORMAT_MODIFIER_EXT) {'''

img = img.replace(old_mod, new_mod, 1)

write(f"{VULKAN}/tu_image.cc", img)

# ===========================================================================
# HACK 2: VRS Hijacking — Force 4×4 shading rate
# tu_pipeline.cc:3817-3878 — override tu6_emit_fragment_shading_rate
# tu_clear_blit.cc:1795-1797 — force VRS config in clear/blit
# tu_cmd_buffer.cc:2178-2183 — coarse LUT injection
# ===========================================================================
pipe = read(f"{VULKAN}/tu_pipeline.cc")

# 2a: Force 4x4 VRS when VRS is disabled
old_vrs_disabled = '''   if (!fsr || (!fs_reads_fsr && vk_fragment_shading_rate_is_disabled(fsr))) {
      tu_cs_emit_regs(cs, A6XX_RB_VRS_CONFIG());
      tu_cs_emit_regs(cs, SP_VRS_CONFIG(CHIP));
      tu_cs_emit_regs(cs, GRAS_VRS_CONFIG(CHIP));
      return;
   }'''

new_vrs_disabled = '''   /* BANDWIDTH HACK: Force 4x4 VRS (2x2 log2) on every draw.
    * The rasterizer evaluates 1 fragment shader for 16 pixels.
    * SP load drops up to 93%.  GMEM tile footprint shrinks
    * proportionally.  Resolve pass flushes to DRAM in a fraction
    * of the time.  The image will look pixelated — that's the point.
    */
   if (!fsr || (!fs_reads_fsr && vk_fragment_shading_rate_is_disabled(fsr))) {
      tu_cs_emit_regs(cs, A6XX_RB_VRS_CONFIG(.unk2 = true, .pipeline_fsr_enable = true));
      tu_cs_emit_regs(cs, SP_VRS_CONFIG(CHIP, .pipeline_fsr_enable = true));
      tu_cs_emit_regs(cs, GRAS_VRS_CONFIG(CHIP,
         .pipeline_fsr_enable = true,
         .frag_size_x = 2, .frag_size_y = 2,
         .combiner_op_1 = FSR_COMBINER_OP_REPLACE,
         .combiner_op_2 = FSR_COMBINER_OP_REPLACE,
         .combiner_clamp_mode = FSR_COMBINER_CLAMP_16_SAMP));
      return;
   }'''

pipe = pipe.replace(old_vrs_disabled, new_vrs_disabled, 1)

# 2b: Also override VRS when it IS enabled — hardcode 4x4
old_vrs_frag = '''   tu_cs_emit_regs(
      cs, GRAS_VRS_CONFIG(CHIP,
                .pipeline_fsr_enable = enable_draw_fsr,
                .frag_size_x = util_logbase2(frag_width),
                .frag_size_y = util_logbase2(frag_height),'''

new_vrs_frag = '''   tu_cs_emit_regs(
      cs, GRAS_VRS_CONFIG(CHIP,
                .pipeline_fsr_enable = true,
                .frag_size_x = 2, .frag_size_y = 2,'''


pipe = pipe.replace(old_vrs_frag, new_vrs_frag, 1)

write(f"{VULKAN}/tu_pipeline.cc", pipe)

# 2c: Force VRS in clear_blit path
blit = read(f"{VULKAN}/tu_clear_blit.cc")
old_blit_vrs = '''      tu_cs_emit_regs(cs, A6XX_RB_VRS_CONFIG());
      tu_cs_emit_regs(cs, SP_VRS_CONFIG(CHIP));
      tu_cs_emit_regs(cs, GRAS_VRS_CONFIG(CHIP));'''

new_blit_vrs = '''      /* BANDWIDTH HACK: Force 4x4 VRS in clear/blit path */
      tu_cs_emit_regs(cs, A6XX_RB_VRS_CONFIG(.unk2 = true, .pipeline_fsr_enable = true));
      tu_cs_emit_regs(cs, SP_VRS_CONFIG(CHIP, .pipeline_fsr_enable = true));
      tu_cs_emit_regs(cs, GRAS_VRS_CONFIG(CHIP, .pipeline_fsr_enable = true,
         .frag_size_x = 2, .frag_size_y = 2,
         .combiner_op_1 = FSR_COMBINER_OP_REPLACE,
         .combiner_op_2 = FSR_COMBINER_OP_REPLACE,
         .combiner_clamp_mode = FSR_COMBINER_CLAMP_16_SAMP));'''

blit = blit.replace(old_blit_vrs, new_blit_vrs, 1)
write(f"{VULKAN}/tu_clear_blit.cc", blit)

# 2d: Coarse VRS LUT in cmd buffer init
cmd = read(f"{VULKAN}/tu_cmd_buffer.cc")
old_vrs_lut = '''   if (dev->physical_device->info->props.has_attachment_shading_rate) {
      tu_cs_emit_regs(cs, GRAS_LRZ_QUALITY_LOOKUP_TABLE_REG(CHIP, 0,
                           fd_gras_shading_rate_lut(0)));
      tu_cs_emit_regs(cs, GRAS_LRZ_QUALITY_LOOKUP_TABLE_REG(CHIP, 1,
                           fd_gras_shading_rate_lut(1)));
   }'''

new_vrs_lut = '''   /* BANDWIDTH HACK: Coarse VRS lookup table.  Maps all shading
    * rate indices to 4x4 regardless of what fd_gras_shading_rate_lut
    * computes.  The LUT entries are 4-bit fields packed into 32-bit
    * registers — we brute-force both entries to all-4x4.
    */
   if (true) {
      tu_cs_emit_regs(cs, GRAS_LRZ_QUALITY_LOOKUP_TABLE_REG(CHIP, 0, 0x44444444));
      tu_cs_emit_regs(cs, GRAS_LRZ_QUALITY_LOOKUP_TABLE_REG(CHIP, 1, 0x44444444));
   }'''

cmd = cmd.replace(old_vrs_lut, new_vrs_lut, 1)

# ===========================================================================
# HACK 3: Depth Format Truncation — D32→D16
# tu_util.h:368-384 — tu6_pipe2depth redirect
# ===========================================================================
util_h = read(f"{VULKAN}/tu_util.h")

old_depth = '''static inline enum a6xx_depth_format
tu6_pipe2depth(VkFormat format)
{
   /* SIMD HERESY H: Branchless depth format lookup.  The switch has 5
    * cases with fall-throughs.  Replace with a 16-entry unsigned char index
    * table.  format values are small integers (VK_FORMAT_D16_UNORM = 124, etc.)
    * but their low 4 bits disambiguate the 5 handled cases.  One indexed load,
    * zero branches, half the cache footprint.
    */
   static const uint8_t depth_map[16] = {
      [0] = DEPTH6_NONE,  [1] = DEPTH6_NONE,  [2] = DEPTH6_NONE,
      [3] = DEPTH6_NONE,  [4] = DEPTH6_NONE,  [5] = DEPTH6_NONE,
      [6] = DEPTH6_NONE,  [7] = DEPTH6_NONE,  [8] = DEPTH6_NONE,
      [9] = DEPTH6_NONE,  [10] = DEPTH6_NONE, [11] = DEPTH6_NONE,
      [12] = DEPTH6_NONE, [13] = DEPTH6_NONE, [14] = DEPTH6_NONE,
      [15] = DEPTH6_NONE,
   };
   switch (format) {
   case VK_FORMAT_D16_UNORM:                return DEPTH6_16;
   case VK_FORMAT_X8_D24_UNORM_PACK32:
   case VK_FORMAT_D24_UNORM_S8_UINT:        return DEPTH6_24_8;
   case VK_FORMAT_D32_SFLOAT:
   case VK_FORMAT_D32_SFLOAT_S8_UINT:
   case VK_FORMAT_S8_UINT:                  return DEPTH6_32;
   default:                                 return DEPTH6_NONE;
   }
}'''

new_depth = '''/* BANDWIDTH HACK: Depth format truncation.  All 32-bit depth formats
 * and separate stencil are silently redirected to 16-bit depth.  The LRZ
 * buffer and GMEM depth tile are physically halved.  Bus traffic is halved.
 * Distant geometry will z-fight through each other — precision is sacrificed
 * for cache capacity.
 */
static inline enum a6xx_depth_format
tu6_pipe2depth(VkFormat format)
{
   switch (format) {
   case VK_FORMAT_D16_UNORM:
      return DEPTH6_16;
   case VK_FORMAT_X8_D24_UNORM_PACK32:
   case VK_FORMAT_D24_UNORM_S8_UINT:
      return DEPTH6_24_8;
   case VK_FORMAT_D32_SFLOAT:
   case VK_FORMAT_D32_SFLOAT_S8_UINT:
   case VK_FORMAT_S8_UINT:
      return DEPTH6_16;   /* <— TRUNCATED: was DEPTH6_32 */
   default:
      return DEPTH6_NONE;
   }
}'''

util_h = util_h.replace(old_depth, new_depth, 1)
write(f"{VULKAN}/tu_util.h", util_h)

# ===========================================================================
# HACK 4: Global LOD Bias — Force +4.0 mip offset, clamp max_lod to 1.0
# tu_sampler.cc:110-124 (A8XX), 110-158 (A6XX)
# ===========================================================================
sampler = read(f"{VULKAN}/tu_sampler.cc")

# Inject V asm macros after the first #include
v_macros_sampler = '''
/* HERESY V: L1D footprint management asm templates (inlined for bandwidth scripts).
 * Guaranteed AArch64 instructions — prfm pldl1keep / prfm pstl1keep.
 * Write-allocate avoids DRAM fetch when overwriting sampler descriptor fields. */
#ifdef __aarch64__
#define TU_PREFETCH_LOAD(ptr) \\
    __asm__ __volatile__("prfm pldl1keep, [%0]" :: "r"(ptr) : "memory")
#define TU_PREFETCH_WRITE(ptr) \\
    __asm__ __volatile__("prfm pstl1keep, [%0]" :: "r"(ptr) : "memory")
#else
#define TU_PREFETCH_LOAD(ptr)  (void)(ptr)
#define TU_PREFETCH_WRITE(ptr) (void)(ptr)
#endif
'''

old_sampler_inc = '#include "tu_sampler.h"'
new_sampler_inc = '#include "tu_sampler.h"\n' + v_macros_sampler
sampler = sampler.replace(old_sampler_inc, new_sampler_inc, 1)

# 4a: Clamp max_lod after the existing CLAMP
old_lod_clamp = '''   float min_lod = CLAMP(pCreateInfo->minLod, 0.0f, 4095.0f / 256.0f);
   float max_lod = CLAMP(pCreateInfo->maxLod, 0.0f, 4095.0f / 256.0f);'''

new_lod_clamp = '''   /* HERESY V: Warm L1 for sampler create info before reading fields */
   TU_PREFETCH_LOAD((void*)pCreateInfo);
   float min_lod = CLAMP(pCreateInfo->minLod, 0.0f, 4095.0f / 256.0f);
   /* BANDWIDTH HACK: Force max_lod to 1.0.  No sampler sees beyond the
    * second mip level.  A 4K texture drops to 1024x1024.  The active
    * working set of textures fits entirely in L1 cache.
    */
   float max_lod = 1.0f;'''

sampler = sampler.replace(old_lod_clamp, new_lod_clamp, 1)

# 4b: A8XX LOD bias +4.0
old_lod_a8xx = '''          A8XX_TEX_SAMP_0_LOD_BIAS(pCreateInfo->mipLodBias) |'''

new_lod_a8xx = '''          A8XX_TEX_SAMP_0_LOD_BIAS(pCreateInfo->mipLodBias + 4.0f) |'''

sampler = sampler.replace(old_lod_a8xx, new_lod_a8xx, 1)

# 4c: A6XX LOD bias +4.0
old_lod_a6xx = '''          A6XX_TEX_SAMP_0_LOD_BIAS(pCreateInfo->mipLodBias);'''

new_lod_a6xx = '''          A6XX_TEX_SAMP_0_LOD_BIAS(pCreateInfo->mipLodBias + 4.0f);'''

sampler = sampler.replace(old_lod_a6xx, new_lod_a6xx, 1)

write(f"{VULKAN}/tu_sampler.cc", sampler)

# Finally write the modified cmd buffer
write(f"{VULKAN}/tu_cmd_buffer.cc", cmd)

print("Bandwidth Starvation applied:")
print("  1 — Forced UBWC on all images    (tu_image.cc)")
print("  2 — 4×4 VRS hijack              (tu_pipeline.cc, tu_clear_blit.cc, tu_cmd_buffer.cc)")
print("  3 — D32→D16 depth truncation    (tu_util.h)")
print("  4 — +4.0 LOD bias, max_lod=1.0 (tu_sampler.cc)")
print("  V — L1D prefetch macros          (tu_sampler.cc)")
PYEOF

echo ""
echo "=== Bandwidth Starvation injection complete ==="
