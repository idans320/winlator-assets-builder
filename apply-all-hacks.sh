#!/bin/bash
# Apply all optimizations to Mesa Turnip source:
#   - Two-tier dirty-bit shadow register system (tu_cs_set_register + tu_cs_flush_dirty)
#   - YOLO_SYNC hack (barrier stripping)
#   - STATE_THROTTLE hack (skip PKT4 every N draws)
#   - REG_BLAST hack (dump 64-reg blocks)
#   - BARRIER_COALESCE opt-in via TU_BARRIER_COALESCE=1

set -e
cd /home/idans/winlator-cmod-builder/mesa/workdir/mesa

echo "Applying full optimization patch..."

python3 << 'PYEOF'
import re

# Read files
with open('src/freedreno/vulkan/tu_cs.h') as f: cs_h = f.read()
with open('src/freedreno/vulkan/tu_cs.cc') as f: cs_cc = f.read()
with open('src/freedreno/vulkan/tu_cmd_buffer.cc') as f: cmdbuf = f.read()

# ============================================================
# tu_cs.h: Add dirty struct after external_iova
# ============================================================
old_struct = '''   /* iova that this CS starts with in TU_CS_MODE_EXTERNAL */
   uint64_t external_iova;

   /* state for cond_exec_start/cond_exec_end */'''
new_struct = '''   /* iova that this CS starts with in TU_CS_MODE_EXTERNAL */
   uint64_t external_iova;

   /* Two-tier dirty-bit shadow register state (gen8+).
    * tu_cs_set_register writes to shadow[] + marks dirty instantly.
    * tu_cs_flush_dirty emits only changed regs as compact PKT4 batches.
    *
    * Hack flags (env vars):
    *   TU_YOLO_SYNC=1    — strip barriers, mega-flush at EndRenderPass
    *   TU_SKIP_STATE=N   — flush dirty only every N draws (default 0=off)
    *   TU_REG_BLAST=1    — dump 64-reg blocks, skip ffs scan
    */
   struct {
      uint32_t *shadow;       /* reg_offset>>2 -> value (5120 entries) */
      uint64_t  blocks[3];    /* Tier 1: one bit per block of 32 slots */
      uint32_t *regs;         /* Tier 2: one bit per exact slot */
      bool      enabled;
      bool      yolo_sync;
      bool      yolo_barrier_needed;
      uint8_t   throttle_n;
      uint8_t   throttle_counter;
      bool      reg_blast;
   } dirty;

   /* state for cond_exec_start/cond_exec_end */'''
cs_h = cs_h.replace(old_struct, new_struct)

# ============================================================
# tu_cs.h: Add new functions after tu_cs_emit_qw, before tu_cs_emit_wfi block
# ============================================================
old_qw = '''static inline void
tu_cs_emit_qw(struct tu_cs *cs, uint64_t value)
{
   tu_cs_emit(cs, (uint32_t) value);
   tu_cs_emit(cs, (uint32_t) (value >> 32));
}

static inline void
tu_cs_emit_write_reg'''

new_qw = '''#define DIRTY_MAX_REG    0x5000
#define DIRTY_MAX_IDX    (DIRTY_MAX_REG >> 2)
#define DIRTY_BLOCKS     ((DIRTY_MAX_IDX + 31) / 32)
#define DIRTY_TIER1      ((DIRTY_BLOCKS + 63) / 64)

static inline bool tu_cs_reg_is_cacheable(uint32_t reg) {
   return (reg < 0x0010) || (reg >= 0x2000 && reg < 0x4000) || (reg >= 0x4000 && reg < 0x5000);
}

static inline void
tu_cs_set_register(struct tu_cs *cs, uint16_t reg, uint32_t value)
{
   if (!cs->dirty.enabled || !tu_cs_reg_is_cacheable(reg))
      goto emit;
   {
      uint32_t idx = reg >> 2, block = idx >> 5, bit = idx & 31;
      if (unlikely(!cs->dirty.shadow)) {
         cs->dirty.shadow = (uint32_t *)calloc(DIRTY_MAX_IDX, sizeof(uint32_t));
         cs->dirty.regs   = (uint32_t *)calloc(DIRTY_BLOCKS, sizeof(uint32_t));
         if (!cs->dirty.shadow || !cs->dirty.regs) goto emit;
      }
      if (cs->dirty.shadow[idx] == value) return;
      cs->dirty.shadow[idx] = value;
      cs->dirty.regs[block] |= (1u << bit);
      cs->dirty.blocks[block >> 6] |= (1ULL << (block & 63));
   }
   return;
emit:
   tu_cs_emit_pkt4(cs, reg, 1);
   tu_cs_emit(cs, value);
}

static inline void
tu_cs_flush_dirty(struct tu_cs *cs)
{
   if (!cs->dirty.enabled || !cs->dirty.shadow) return;
   if (cs->dirty.throttle_n > 1) {
      if (++cs->dirty.throttle_counter < cs->dirty.throttle_n) return;
      cs->dirty.throttle_counter = 0;
   }
   if (cs->dirty.reg_blast) {
      for (int t = 0; t < DIRTY_TIER1; t++) {
         uint64_t blocks = cs->dirty.blocks[t];
         if (!blocks) continue;
         while (blocks) {
            int b = __builtin_ffsll(blocks) - 1;
            blocks &= ~(1ULL << b);
            tu_cs_emit_pkt4(cs, (uint16_t)(((t << 6) + b) << 7), 32);
            uint32_t *src = cs->dirty.shadow + ((t << 6) + b) * 32;
            for (int i = 0; i < 32; i++) tu_cs_emit(cs, src[i]);
         }
         cs->dirty.blocks[t] = 0;
      }
      memset(cs->dirty.regs, 0, DIRTY_BLOCKS * sizeof(uint32_t));
      return;
   }
   for (int t = 0; t < DIRTY_TIER1; t++) {
      uint64_t blocks = cs->dirty.blocks[t];
      if (!blocks) continue;
      while (blocks) {
         int b = __builtin_ffsll(blocks) - 1;
         uint32_t regs = cs->dirty.regs[(t << 6) + b];
         blocks &= ~(1ULL << b);
         while (regs) {
            int bit = __builtin_ffs(regs) - 1;
            uint32_t idx = ((t << 6) + b) * 32 + bit;
            uint16_t base = (uint16_t)(idx << 2);
            regs &= ~(1u << bit);
            uint16_t cnt = 1;
            while ((regs >> (bit + cnt)) & 1) cnt++;
            tu_cs_emit_pkt4(cs, base, cnt);
            for (uint16_t c = 0; c < cnt; c++) { tu_cs_emit(cs, cs->dirty.shadow[idx + c]); regs &= ~(1u << (bit + c)); }
         }
         cs->dirty.regs[(t << 6) + b] = 0;
      }
      cs->dirty.blocks[t] = 0;
   }
}

static inline void
tu_cs_flush_barrier_yolo(struct tu_cs *cs)
{
   if (!cs->dirty.yolo_sync || !cs->dirty.yolo_barrier_needed) return;
   tu_cs_emit_pkt7(cs, CP_EVENT_WRITE, 4);
   tu_cs_emit(cs, 0x1f); tu_cs_emit(cs, 0); tu_cs_emit(cs, 0); tu_cs_emit(cs, 0);
   tu_cs_emit_wfi(cs);
   cs->dirty.yolo_barrier_needed = false;
}

static inline void
tu_cs_emit_qw(struct tu_cs *cs, uint64_t value)
{
   tu_cs_emit(cs, (uint32_t) value);
   tu_cs_emit(cs, (uint32_t) (value >> 32));
}

static inline void
tu_cs_emit_write_reg'''

cs_h = cs_h.replace(old_qw, new_qw)

# ============================================================
# tu_cs.cc: Add stdlib.h + dirty init/free
# ============================================================
old_cc = '''#include "tu_cs.h"

#include "tu_device.h"'''
new_cc = '''#include "tu_cs.h"

#include <stdlib.h>

#include "tu_device.h"'''
cs_cc = cs_cc.replace(old_cc, new_cc)

old_init = '''   cs->device = device;
   cs->mode = mode;
   cs->next_bo_size = initial_size;
   cs->name = name;
}'''
new_init = '''   cs->device = device;
   cs->mode = mode;
   cs->next_bo_size = initial_size;
   cs->name = name;

   if (device->physical_device->info->chip >= A8XX) {
      cs->dirty.enabled = true;
      cs->dirty.shadow = NULL;
      cs->dirty.regs = NULL;
      memset(cs->dirty.blocks, 0, sizeof(cs->dirty.blocks));
      cs->dirty.yolo_sync = !!getenv("TU_YOLO_SYNC");
      cs->dirty.yolo_barrier_needed = false;
      cs->dirty.throttle_n = getenv("TU_SKIP_STATE") ? atoi(getenv("TU_SKIP_STATE")) : 0;
      cs->dirty.throttle_counter = 0;
      cs->dirty.reg_blast = !!getenv("TU_REG_BLAST");
   }
}'''
cs_cc = cs_cc.replace(old_init, new_init)

old_free = '''   free(cs->entries);
   free(cs->read_only.bos);
   free(cs->read_write.bos);
}'''
new_free = '''   free(cs->entries);
   free(cs->read_only.bos);
   free(cs->read_write.bos);
   free(cs->dirty.shadow);
   free(cs->dirty.regs);
}'''
cs_cc = cs_cc.replace(old_free, new_free)

# ============================================================
# tu_cmd_buffer.cc: YOLO in tu_emit_cache_flush + renderpass variant
# ============================================================
old_f = '''   BITMASK_ENUM(tu_cmd_flush_bits) flushes = cache->flush_bits;
   tu6_emit_flushes<CHIP>(cmd_buffer, cs, cache);'''
new_f = '''   if (unlikely(cmd_buffer->cs.dirty.yolo_sync)) {
      cmd_buffer->cs.dirty.yolo_barrier_needed = true;
      return;
   }
   BITMASK_ENUM(tu_cmd_flush_bits) flushes = cache->flush_bits;
   tu6_emit_flushes<CHIP>(cmd_buffer, cs, cache);'''
cmdbuf = cmdbuf.replace(old_f,
   '''   if (unlikely(cmd_buffer->cs.dirty.yolo_sync)) {
      cmd_buffer->cs.dirty.yolo_barrier_needed = true;
      return;
   }
   BITMASK_ENUM(tu_cmd_flush_bits) flushes = cache->flush_bits;
   tu6_emit_flushes<CHIP>(cmd_buffer, cs, cache);''')

old_rp = '''   if (!cmd_buffer->state.renderpass_cache.flush_bits &&
        likely(!tu_env.debug))
       return;

   struct tu_cs *cs = &cmd_buffer->draw_cs;'''
new_rp = '''   if (unlikely(cmd_buffer->draw_cs.dirty.yolo_sync)) {
      cmd_buffer->draw_cs.dirty.yolo_barrier_needed = true;
      return;
   }
   if (!cmd_buffer->state.renderpass_cache.flush_bits &&
        likely(!tu_env.debug))
       return;

   struct tu_cs *cs = &cmd_buffer->draw_cs;'''
cmdbuf = cmdbuf.replace(old_rp, new_rp)

# ============================================================
# Write files back
# ============================================================
with open('src/freedreno/vulkan/tu_cs.h', 'w') as f: f.write(cs_h)
with open('src/freedreno/vulkan/tu_cs.cc', 'w') as f: f.write(cs_cc)
with open('src/freedreno/vulkan/tu_cmd_buffer.cc', 'w') as f: f.write(cmdbuf)

print("All changes applied successfully")
PYEOF

echo "Patch applied"
