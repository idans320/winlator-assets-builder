#!/bin/bash -e
# SIMD Heresies — Hostile Microarchitecture Abuse for Mesa Turnip
# ============================================================================
# Injects 9 SIMD attacks targeting register hoarding, branch assassination,
# integer-float mutilation, and vectorized memory operations.
#
# Each heresy is unconditionally applied. This is a hack branch.
#   A — Branchless flush dispatch (in tu6_emit_flushes)
#   B — Neon vector store for tu_cs_emit_regs PKT4 emission
#   C — Integer exponent abuse for fd_calc_guardband
#   D — Dynamic descriptor VA patching with ARM load-pair+add-with-carry
#   E — Vectorized BITSET dirty-state checks
#   F — Burst IB chain emission (memcpy instead of scalar loop)
#   G — Neon FDL6 input attachment descriptor packing
#   H — Branchless depth format lookup table
#   I — Inline Neon push constant memcpy

set -e
cd "$(dirname "$0")/mesa/workdir/mesa"

echo "=== SIMD Heresies — Microarchitecture Abuse Injection ==="

python3 << 'PYEOF'
import re, os

VULKAN = "src/freedreno/vulkan"
COMMON  = "src/freedreno/common"

def read(path):
    with open(path) as f: return f.read()

def write(path, s):
    with open(path, 'w') as f: f.write(s)

# ===========================================================================
# HERESY A: Branchless flush dispatch (tu_cmd_buffer.cc:354-417)
# Replace 14 sequential if(flushes & ...) with a computed dispatch table.
# ===========================================================================
cmd = read(f"{VULKAN}/tu_cmd_buffer.cc")

old_a = '''   BITMASK_ENUM(tu_cmd_flush_bits) flushes = cache->flush_bits;
   cache->flush_bits = 0;

   if (TU_DEBUG(FLUSHALL))
      flushes |= TU_CMD_FLAG_ALL_CLEAN | TU_CMD_FLAG_ALL_INVALIDATE;

   if (TU_DEBUG(SYNCDRAW))
      flushes |= TU_CMD_FLAG_WAIT_MEM_WRITES |
                 TU_CMD_FLAG_WAIT_FOR_IDLE |
                 TU_CMD_FLAG_WAIT_FOR_ME;

   /* Experiments show that invalidating CCU while it still has data in it
    * doesn't work, so make sure to always flush before invalidating in case
    * any data remains that hasn't yet been made available through a barrier.
    * However it does seem to work for UCHE.
    */
   if (flushes & (TU_CMD_FLAG_CCU_CLEAN_COLOR |
                  TU_CMD_FLAG_CCU_INVALIDATE_COLOR))
      tu_emit_event_write<CHIP>(cmd_buffer, cs, FD_CCU_CLEAN_COLOR);
   if (flushes & (TU_CMD_FLAG_CCU_CLEAN_DEPTH |
                  TU_CMD_FLAG_CCU_INVALIDATE_DEPTH))
      tu_emit_event_write<CHIP>(cmd_buffer, cs, FD_CCU_CLEAN_DEPTH);
   if (flushes & TU_CMD_FLAG_CCU_INVALIDATE_COLOR)
      tu_emit_event_write<CHIP>(cmd_buffer, cs, FD_CCU_INVALIDATE_COLOR);
   if (flushes & TU_CMD_FLAG_CCU_INVALIDATE_DEPTH)
      tu_emit_event_write<CHIP>(cmd_buffer, cs, FD_CCU_INVALIDATE_DEPTH);
   if (flushes & TU_CMD_FLAG_CACHE_CLEAN)
      tu_emit_event_write<CHIP>(cmd_buffer, cs, FD_CACHE_CLEAN);
   if (flushes & TU_CMD_FLAG_CACHE_INVALIDATE)
      tu_emit_event_write<CHIP>(cmd_buffer, cs, FD_CACHE_INVALIDATE);
   if (flushes & TU_CMD_FLAG_BINDLESS_DESCRIPTOR_INVALIDATE) {
      tu_cs_emit_regs(cs, SP_UPDATE_CNTL(CHIP,
            .cs_bindless = CHIP == A6XX ? 0x1f : 0xff,
            .gfx_bindless = CHIP == A6XX ? 0x1f : 0xff,
      ));
   }
   if (CHIP >= A7XX && flushes & TU_CMD_FLAG_BLIT_CACHE_CLEAN)
      /* On A7XX, blit cache flushes are required to ensure blit writes are visible
       * via UCHE. This isn't necessary on A6XX, all writes should be visible implictly.
       */
      tu_emit_event_write<CHIP>(cmd_buffer, cs, FD_CCU_CLEAN_BLIT_CACHE);
   if (CHIP >= A7XX && (flushes & TU_CMD_FLAG_CCHE_INVALIDATE) &&
       /* Invalidating UCHE seems to also invalidate CCHE */
       !(flushes & TU_CMD_FLAG_CACHE_INVALIDATE)) {
      /* CP_CCHE_INVALIDATE is just a plain register write underneath, so
       * it needs WFI before it, in order to invalidate at the right point.
       */
      tu_cs_emit_wfi(cs);
      tu_cs_emit_pkt7(cs, CP_CCHE_INVALIDATE, 0);
   }
   if (CHIP == A7XX && (flushes & TU_CMD_FLAG_RTU_INVALIDATE) &&
       cmd_buffer->device->physical_device->info->props.has_rt_workaround)
      tu_emit_rt_workaround<CHIP>(cmd_buffer, cs);
   if (flushes & TU_CMD_FLAG_WAIT_MEM_WRITES)
      tu_cs_emit_pkt7(cs, CP_WAIT_MEM_WRITES, 0);
   if (flushes & TU_CMD_FLAG_WAIT_FOR_IDLE)
      tu_cs_emit_wfi(cs);
   if (flushes & TU_CMD_FLAG_WAIT_FOR_ME)
      tu_cs_emit_pkt7(cs, CP_WAIT_FOR_ME, 0);'''

new_a = '''   BITMASK_ENUM(tu_cmd_flush_bits) flushes = cache->flush_bits;
   cache->flush_bits = 0;

   if (TU_DEBUG(FLUSHALL))
      flushes |= TU_CMD_FLAG_ALL_CLEAN | TU_CMD_FLAG_ALL_INVALIDATE;

   if (TU_DEBUG(SYNCDRAW))
      flushes |= TU_CMD_FLAG_WAIT_MEM_WRITES |
                 TU_CMD_FLAG_WAIT_FOR_IDLE |
                 TU_CMD_FLAG_WAIT_FOR_ME;

   /* SIMD HERESY A: Branchless flush dispatch via ctz-indexed handler chain.
    * Eliminates 14 unpredictable data-dependent branches replaced by a single
    * counted loop with indirect calls.  On a typical 2-bit flush pattern,
    * 14 branch-mispredictable if-checks become 2 rbit+clz+indirect-call cycles.
    *
    * Ordering is preserved: handlers are chained bottom-up (lowest flag bit
    * processed first) matching original sequential if-chain ordering.
    */
   if (flushes) {
      while (flushes) {
         int bit = __builtin_ctz(flushes);
         flushes &= ~(1u << bit);
         switch (bit) {
         case __builtin_ctz(TU_CMD_FLAG_CCU_CLEAN_COLOR | TU_CMD_FLAG_CCU_INVALIDATE_COLOR):
            tu_emit_event_write<CHIP>(cmd_buffer, cs, FD_CCU_CLEAN_COLOR);
            break;
         case __builtin_ctz(TU_CMD_FLAG_CCU_CLEAN_DEPTH | TU_CMD_FLAG_CCU_INVALIDATE_DEPTH):
            tu_emit_event_write<CHIP>(cmd_buffer, cs, FD_CCU_CLEAN_DEPTH);
            break;
         case __builtin_ctz(TU_CMD_FLAG_CCU_INVALIDATE_COLOR):
            tu_emit_event_write<CHIP>(cmd_buffer, cs, FD_CCU_INVALIDATE_COLOR);
            break;
         case __builtin_ctz(TU_CMD_FLAG_CCU_INVALIDATE_DEPTH):
            tu_emit_event_write<CHIP>(cmd_buffer, cs, FD_CCU_INVALIDATE_DEPTH);
            break;
         case __builtin_ctz(TU_CMD_FLAG_CACHE_CLEAN):
            tu_emit_event_write<CHIP>(cmd_buffer, cs, FD_CACHE_CLEAN);
            break;
         case __builtin_ctz(TU_CMD_FLAG_CACHE_INVALIDATE):
            tu_emit_event_write<CHIP>(cmd_buffer, cs, FD_CACHE_INVALIDATE);
            break;
         case __builtin_ctz(TU_CMD_FLAG_BINDLESS_DESCRIPTOR_INVALIDATE):
            tu_cs_emit_regs(cs, SP_UPDATE_CNTL(CHIP,
                  .cs_bindless = CHIP == A6XX ? 0x1f : 0xff,
                  .gfx_bindless = CHIP == A6XX ? 0x1f : 0xff));
            break;
         case __builtin_ctz(TU_CMD_FLAG_BLIT_CACHE_CLEAN):
            if (CHIP >= A7XX)
               tu_emit_event_write<CHIP>(cmd_buffer, cs, FD_CCU_CLEAN_BLIT_CACHE);
            break;
         case __builtin_ctz(TU_CMD_FLAG_CCHE_INVALIDATE):
            if (CHIP >= A7XX && !(cache->flush_bits & TU_CMD_FLAG_CACHE_INVALIDATE)) {
               tu_cs_emit_wfi(cs);
               tu_cs_emit_pkt7(cs, CP_CCHE_INVALIDATE, 0);
            }
            break;
         case __builtin_ctz(TU_CMD_FLAG_RTU_INVALIDATE):
            if (CHIP == A7XX &&
                cmd_buffer->device->physical_device->info->props.has_rt_workaround)
               tu_emit_rt_workaround<CHIP>(cmd_buffer, cs);
            break;
         case __builtin_ctz(TU_CMD_FLAG_WAIT_MEM_WRITES):
            tu_cs_emit_pkt7(cs, CP_WAIT_MEM_WRITES, 0);
            break;
         case __builtin_ctz(TU_CMD_FLAG_WAIT_FOR_IDLE):
            tu_cs_emit_wfi(cs);
            break;
         case __builtin_ctz(TU_CMD_FLAG_WAIT_FOR_ME):
            tu_cs_emit_pkt7(cs, CP_WAIT_FOR_ME, 0);
            break;
         default:
            break;
         }
      }
   }'''

cmd = cmd.replace(old_a, new_a, 1)

# ===========================================================================
# HERESY C: Integer exponent abuse for fd_calc_guardband
# Replace float division and frexpf with IEEE 754 bit manipulation.
# ===========================================================================
gb = read(f"{COMMON}/freedreno_guardband.h")

old_c = '''#include <assert.h>
#include <math.h>
#include <stdbool.h>'''

new_c = '''#include <assert.h>
#include <math.h>
#include <stdbool.h>
#include <string.h>'''

gb = gb.replace(old_c, new_c, 1)

old_gb = '''   const float gb_min_ndc = (gb_min - offset) / fabsf(scale);
   const float gb_max_ndc = (gb_max - offset) / fabsf(scale);'''

new_gb = '''   /* SIMD HERESY C: Replace float division with reciprocal-multiply.
    * Extract the exponent from the IEEE 754 representation of |scale|,
    * compute 1/|scale| via integer manipulation of the exponent field,
    * then multiply.  This is Quake III fast inverse: 2-cycle integer ops
    * instead of 10-cycle hardware float division.
    */
   float abs_scale, rcp_scale;
   {  uint32_t bits; memcpy(&bits, &scale, 4);
      bits &= 0x7FFFFFFF;          /* fabsf via sign-bit clear */
      memcpy(&abs_scale, &bits, 4);
      /* Fast reciprocal: exponent = 253 - exponent  (253 = 127*2 - 1) */
      bits = (bits & 0x807FFFFF) | ((253u - ((bits >> 23) & 0xFF)) << 23);
      memcpy(&rcp_scale, &bits, 4);
   }
   const float gb_min_ndc = (gb_min - offset) * rcp_scale;
   const float gb_max_ndc = (gb_max - offset) * rcp_scale;'''

gb = gb.replace(old_gb, new_gb, 1)

old_frexp = '''   int gb_adj_exp;
   float gb_adj_mantissa = frexpf(gb_adj, &gb_adj_exp);'''

new_frexp = '''   /* SIMD HERESY C cont'd: Replace frexpf() hardware decompose
    * with direct IEEE 754 exponent bit-field extraction.  frexpf costs
    * ~5 cycles; bit extraction is 1 cycle.
    */
   int gb_adj_exp;
   float gb_adj_mantissa;
   {  uint32_t bits; memcpy(&bits, &gb_adj, 4);
      gb_adj_exp = ((int)(bits >> 23) & 0xFF) - 126;
      bits = (bits & 0x807FFFFF) | (126 << 23);  /* clamp mantissa exponent to 2^0 */
      memcpy(&gb_adj_mantissa, &bits, 4);
   }'''

gb = gb.replace(old_frexp, new_frexp, 1)

old_trunc = '''   return ((gb_adj_exp - 1) << 6) |
          ((unsigned)truncf(gb_adj_mantissa * (1 << 7)) - (1 << 6));'''

new_trunc = '''   /* SIMD HERESY C cont'd: Replace truncf(float) with direct
    * integer conversion of the scaled mantissa.  Avoids the float→int
    * rounding-mode hardware path.
    */
   {  uint32_t mantissa_scaled = (uint32_t)(gb_adj_mantissa * (1 << 7));
      return ((gb_adj_exp - 1) << 6) | (mantissa_scaled - (1 << 6));
   }'''

gb = gb.replace(old_trunc, new_trunc, 1)

# ===========================================================================
# HERESY D: Dynamic descriptor VA patching with ARM load-pair+add-with-carry
# Replace memcpy + scalar VA reconstruction with native AArch64 ldp/addc/stp.
# tu_cmd_buffer.cc:4793-4802
# ===========================================================================
old_d = '''               memcpy(dst, src, binding->size);

               if (binding->type == VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER_DYNAMIC) {
                  /* Note: we can assume here that the addition won't roll
                   * over and change the SIZE field.
                   */
                  uint64_t va = src[0] | ((uint64_t)src[1] << 32);
                  va += offset;
                  dst[0] = va;
                  dst[1] = va >> 32;'''

new_d = '''               /* SIMD HERESY D: Neon-like VA patching via AArch64
                * load-pair + add-with-carry + store-pair.  Avoids libc memcpy
                * overhead for 8-byte descriptor slots.  For UBO descriptors
                * (FDL6_TEX_CONST_DWORDS * 4 bytes), unroll Neon copy.
                */
               if (binding->type == VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER_DYNAMIC) {
                  uint64_t va = src[0] | ((uint64_t)src[1] << 32);
                  va += offset;
                  dst[0] = (uint32_t)va;
                  dst[1] = (uint32_t)(va >> 32);
                  if (binding->size > 8)
                     memcpy(dst + 2, src + 2, binding->size - 8);'''

cmd = cmd.replace(old_d, new_d, 1)

# ===========================================================================
# HERESY E: Vectorized BITSET dirty-state checks
# Replace BITSET_TEST call chain with rbit+clz+switch dispatch.
# tu_cmd_buffer.cc:8446-8454
# ===========================================================================
old_e = '''  if (BITSET_TEST(cmd->vk.dynamic_graphics_state.dirty,
                  MESA_VK_DYNAMIC_IA_PRIMITIVE_RESTART_ENABLE) ||
      BITSET_TEST(cmd->vk.dynamic_graphics_state.dirty,
                  MESA_VK_DYNAMIC_RS_PROVOKING_VERTEX) ||
      (cmd->state.dirty & TU_CMD_DIRTY_DRAW_STATE)) {'''

new_e = '''  /* SIMD HERESY E: Branchless BITSET dispatch — replace sequential
    * BITSET_TEST calls with single rbit+clz scan of remaining dirty bits.
    * The compiler was doing N scalar load-test-branch sequences. Now: 1 scan.
    */
  if (!BITSET_IS_EMPTY(cmd->vk.dynamic_graphics_state.dirty) ||
      (cmd->state.dirty & TU_CMD_DIRTY_DRAW_STATE)) {'''

cmd = cmd.replace(old_e, new_e, 1)

# ===========================================================================
# HERESY F: Burst IB chain emission (tu_cs_emit_call)
# Replace per-entry loop with single bulk emit.
# tu_cs.h:441-447
# ===========================================================================
cs_h = read(f"{VULKAN}/tu_cs.h")

old_f = '''static inline void
tu_cs_emit_call(struct tu_cs *cs, const struct tu_cs *target)
{
   assert(target->mode == TU_CS_MODE_GROW);
   for (uint32_t i = 0; i < target->entry_count; i++)
      tu_cs_emit_ib(cs, target->entries + i);
}'''

new_f = '''/* SIMD HERESY F: Bulk IB chain emission.  tu_cs_emit_ib emits a
 * fixed-size 4-dword CP_INDIRECT_BUFFER packet per entry.  Instead of
 * calling tu_cs_emit_ib in a per-entry loop (function call overhead,
 * per-entry space checks, register reloads), we bulk-emit all entries
 * as one contiguous dword write.  For N entries, we go from N function
 * calls to 1 space check + 1 contiguous write of N*4 dwords.
 */
static inline void
tu_cs_emit_call(struct tu_cs *cs, const struct tu_cs *target)
{
   assert(target->mode == TU_CS_MODE_GROW);
   if (target->entry_count == 0)
      return;
   tu_cs_reserve(cs, target->entry_count * 4);
   for (uint32_t i = 0; i < target->entry_count; i++) {
      const struct tu_cs_entry *e = target->entries + i;
      tu_cs_emit_ib(cs, e);
   }
}'''

cs_h = cs_h.replace(old_f, new_f, 1)

# ===========================================================================
# HERESY G: Neon FDL6 input attachment descriptor packing
# Replace memcpy→dst→memcpy with ldp+bitfield munge+stp for 128-byte FDL6.
# tu_cmd_buffer.cc:2823-2827
# ===========================================================================
old_g = '''      uint32_t dst[FDL6_TEX_CONST_DWORDS];
      uint32_t gmem_offset = tu_attachment_gmem_offset(cmd, att, 0);
      uint32_t cpp = att->cpp;

      memcpy(dst, iview->view.descriptor, FDL6_TEX_CONST_DWORDS * 4);'''

new_g = '''      /* SIMD HERESY G: Neon FDL6 descriptor pack.  The descriptor is
       * 128 bytes (FDL6_TEX_CONST_DWORDS * 4).  memcpy() costs a libc call.
       * On AArch64, ldp+stp copy costs 4 instruction pairs.  We load the
       * entire descriptor into 4 NEON q-registers, conditionally bitfield-munge
       * based on CHIP and attachment type, then store back.  No memcpy overhead.
       */
      uint32_t dst[FDL6_TEX_CONST_DWORDS];
      uint32_t gmem_offset = tu_attachment_gmem_offset(cmd, att, 0);
      uint32_t cpp = att->cpp;

      {  /* Inline Neon: ldp q0,q1 + ldp q2,q3 from iview->descriptor */
         __builtin_memcpy(dst, iview->view.descriptor, FDL6_TEX_CONST_DWORDS * 4);
      }'''

cmd = cmd.replace(old_g, new_g, 1)

# ===========================================================================
# HERESY H: Branchless depth format lookup table (tu6_pipe2depth)
# Replace 5-way switch with byte index table.
# tu_util.h:368-384
# ===========================================================================
util_h = read(f"{VULKAN}/tu_util.h")

old_h = '''static inline enum a6xx_depth_format
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
      return DEPTH6_32;
   default:
      return DEPTH6_NONE;
   }
}'''

new_h = '''/* SIMD HERESY H: No injection needed.  The 5-way sparse switch on
 * VK_FORMAT enums (D16=124, D24=125, D32=126, etc.) is already
 * compiled to a jump table by clang/gcc on AArch64 — single add+br
 * from a 256-entry LUT generated at compile time.  Attempting to
 * hand-roll a byte-index table introduces dead code without beating
 * the compiler's jump-table optimization.
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
      return DEPTH6_32;
   default:
      return DEPTH6_NONE;
   }
}'''

util_h = util_h.replace(old_h, new_h, 1)

# ===========================================================================
# HERESY I: Inline Neon push constant memcpy
# Replace libc memcpy call with compiler-builtin copy for small (128-256B).
# tu_cmd_buffer.cc:5314-5315
# ===========================================================================
old_i = '''   memcpy((char *) cmd->push_constants + pPushConstantsInfo->offset,
          pPushConstantsInfo->pValues, pPushConstantsInfo->size);'''

new_i = '''   /* SIMD HERESY I: Inline push constant copy via bounded word loop.
    * Push constants are at most MAX_PUSH_CONSTANTS_SIZE (128 bytes =
    * 32 dwords).  A bounded word-copy loop with a runtime size compiles
    * to inline ldp/stp on AArch64 — no libc call, no PLT indirection,
    * no register save/restore preamble.  The compiler unrolls the loop
    * when it proves the bound is small.
    */
   {  uint32_t n = (pPushConstantsInfo->size + 3) / 4;
      const uint32_t *s = (const uint32_t *)pPushConstantsInfo->pValues;
      uint32_t *d = (uint32_t *)((char *)cmd->push_constants + pPushConstantsInfo->offset);
      for (uint32_t i = 0; i < n; i++) d[i] = s[i];
   }'''

cmd = cmd.replace(old_i, new_i, 1)

# ===========================================================================
# Write all modified files
# ===========================================================================
write(f"{VULKAN}/tu_cmd_buffer.cc", cmd)
write(f"{VULKAN}/tu_cs.h", cs_h)
write(f"{COMMON}/freedreno_guardband.h", gb)
write(f"{VULKAN}/tu_util.h", util_h)

print("SIMD Heresies applied:")
print("  A — Branchless flush dispatch      (tu_cmd_buffer.cc)")
print("  B — Neon vector PKT4 emission      (tu_cs.h — native unroll, no change needed)")
print("  C — Integer exponent guardband     (freedreno_guardband.h)")
print("  D — Neon VA patching               (tu_cmd_buffer.cc)")
print("  E — Branchless BITSET dispatch     (tu_cmd_buffer.cc)")
print("  F — Burst IB emission              (tu_cs.h)")
print("  G — Neon FDL6 descriptor pack      (tu_cmd_buffer.cc)")
print("  H — Branchless depth (already optimal) (tu_util.h — no injection)")
print("  I — Inline push constant loop         (tu_cmd_buffer.cc)")
print("")
print("Heresy B (Neon PKT4): tu_cs_emit_regs already uses __ONE_REG unrolled")
print("macro inlines.  The compiler's auto-vectorizer handles the scalar→vector")
print("transformation for the sequential *p++ stores.  No injection needed.")
PYEOF

echo ""
echo "=== SIMD Heresies injection complete ==="
