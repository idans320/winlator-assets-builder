#!/bin/bash -e
# Bandwidth Lite — VRS 4x4 hijack only.  LOD bias removed (unleash A800).
# Fragment shader work reduced 16x.  Image stays correct.

set -e
cd "$(dirname "$0")/mesa/workdir/mesa"

echo "=== Bandwidth Lite — VRS Fragment Rate Reduction ==="

python3 << 'PYEOF'
VULKAN = "src/freedreno/vulkan"

def read(path):
    with open(path) as f: return f.read()

def write(path, s):
    with open(path, 'w') as f: f.write(s)

# ===========================================================================
# HACK 1: VRS Hijacking — Force 4×4 shading rate
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
write(f"{VULKAN}/tu_cmd_buffer.cc", cmd)

# LOD bias hack REMOVED — unleash the A800, full texture resolution.

print("Bandwidth Lite applied:")
print("  1 — 4×4 VRS hijack  (tu_pipeline.cc, tu_clear_blit.cc, tu_cmd_buffer.cc)")
print("")
print("Removed: LOD bias +4.0 (unleash A800 — full mip resolution)")
PYEOF

echo ""
echo "=== Bandwidth Lite injection complete ==="
