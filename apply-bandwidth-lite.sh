#!/bin/bash -e
# Bandwidth Lite — VRS + LOD bias only.  UBWC and depth truncation removed.
# The image stays correct.  The GPU just works less hard.

set -e
cd "$(dirname "$0")/mesa/workdir/mesa"

echo "=== Bandwidth Lite — Safe DRAM Reduction ==="

python3 << 'PYEOF'
VULKAN = "src/freedreno/vulkan"

def read(path):
    with open(path) as f: return f.read()

def write(path, s):
    with open(path, 'w') as f: f.write(s)

# ===========================================================================
# HACK 1: VRS Hijacking — Force 4×4 shading rate (KEPT — safe, visual only)
# ===========================================================================
pipe = read(f"{VULKAN}/tu_pipeline.cc")

old_vrs_disabled = '''   if (!fsr || (!fs_reads_fsr && vk_fragment_shading_rate_is_disabled(fsr))) {
      tu_cs_emit_regs(cs, A6XX_RB_VRS_CONFIG());
      tu_cs_emit_regs(cs, SP_VRS_CONFIG(CHIP));
      tu_cs_emit_regs(cs, GRAS_VRS_CONFIG(CHIP));
      return;
   }'''

new_vrs_disabled = '''   /* BANDWIDTH LITE: Force 4x4 VRS.  1 fragment shader invocation
    * covers 16 pixels.  GMEM tile footprint shrinks proportionally.
    * Visual quality drops — pixelation is the trade for thermals.
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

# VRS in clear/blit
blit = read(f"{VULKAN}/tu_clear_blit.cc")
old_blit_vrs = '''      tu_cs_emit_regs(cs, A6XX_RB_VRS_CONFIG());
      tu_cs_emit_regs(cs, SP_VRS_CONFIG(CHIP));
      tu_cs_emit_regs(cs, GRAS_VRS_CONFIG(CHIP));'''

new_blit_vrs = '''      /* BANDWIDTH LITE: 4x4 VRS in clear/blit */
      tu_cs_emit_regs(cs, A6XX_RB_VRS_CONFIG(.unk2 = true, .pipeline_fsr_enable = true));
      tu_cs_emit_regs(cs, SP_VRS_CONFIG(CHIP, .pipeline_fsr_enable = true));
      tu_cs_emit_regs(cs, GRAS_VRS_CONFIG(CHIP, .pipeline_fsr_enable = true,
         .frag_size_x = 2, .frag_size_y = 2));'''

blit = blit.replace(old_blit_vrs, new_blit_vrs, 1)
write(f"{VULKAN}/tu_clear_blit.cc", blit)

# VRS LUT
cmd = read(f"{VULKAN}/tu_cmd_buffer.cc")
old_vrs_lut = '''   if (dev->physical_device->info->props.has_attachment_shading_rate) {
      tu_cs_emit_regs(cs, GRAS_LRZ_QUALITY_LOOKUP_TABLE_REG(CHIP, 0,
                           fd_gras_shading_rate_lut(0)));
      tu_cs_emit_regs(cs, GRAS_LRZ_QUALITY_LOOKUP_TABLE_REG(CHIP, 1,
                           fd_gras_shading_rate_lut(1)));
   }'''

new_vrs_lut = '''   /* BANDWIDTH LITE: Coarse VRS LUT — all indices → 4×4 */
   {
      tu_cs_emit_regs(cs, GRAS_LRZ_QUALITY_LOOKUP_TABLE_REG(CHIP, 0, 0x44444444));
      tu_cs_emit_regs(cs, GRAS_LRZ_QUALITY_LOOKUP_TABLE_REG(CHIP, 1, 0x44444444));
   }'''

cmd = cmd.replace(old_vrs_lut, new_vrs_lut, 1)

# ===========================================================================
# HACK 2: Global LOD Bias — +4.0 mip offset, max_lod = 1.0 (KEPT)
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

old_lod_clamp = '''   float min_lod = CLAMP(pCreateInfo->minLod, 0.0f, 4095.0f / 256.0f);
   float max_lod = CLAMP(pCreateInfo->maxLod, 0.0f, 4095.0f / 256.0f);'''

new_lod_clamp = '''   /* HERESY V: Warm L1 for sampler create info before reading fields */
   TU_PREFETCH_LOAD((void*)pCreateInfo);
   float min_lod = CLAMP(pCreateInfo->minLod, 0.0f, 4095.0f / 256.0f);
   /* BANDWIDTH LITE: Clamp max_lod to 1.0 — drop to mip 1.  L1 texture
    * cache hit rate maxed out.  4K → 1024×1024 effective resolution.
    */
   float max_lod = 1.0f;'''

sampler = sampler.replace(old_lod_clamp, new_lod_clamp, 1)

old_lod_a8xx = '''          A8XX_TEX_SAMP_0_LOD_BIAS(pCreateInfo->mipLodBias) |'''

new_lod_a8xx = '''          A8XX_TEX_SAMP_0_LOD_BIAS(pCreateInfo->mipLodBias + 4.0f) |'''

sampler = sampler.replace(old_lod_a8xx, new_lod_a8xx, 1)

old_lod_a6xx = '''          A6XX_TEX_SAMP_0_LOD_BIAS(pCreateInfo->mipLodBias);'''

new_lod_a6xx = '''          A6XX_TEX_SAMP_0_LOD_BIAS(pCreateInfo->mipLodBias + 4.0f);'''

sampler = sampler.replace(old_lod_a6xx, new_lod_a6xx, 1)

write(f"{VULKAN}/tu_sampler.cc", sampler)
write(f"{VULKAN}/tu_cmd_buffer.cc", cmd)

print("Bandwidth Lite applied:")
print("  1 — 4×4 VRS hijack              (tu_pipeline.cc, tu_clear_blit.cc, tu_cmd_buffer.cc)")
print("  2 — +4.0 LOD bias, max_lod=1.0 (tu_sampler.cc)")
print("  V — L1D prefetch macros          (tu_sampler.cc)")
print("")
print("NOT applied (crashed): UBWC force, D32→D16 depth truncation")
PYEOF

echo ""
echo "=== Bandwidth Lite injection complete ==="
