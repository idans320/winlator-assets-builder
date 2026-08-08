# Oryon Microarchitecture — Reverse-Engineered Heresy Opportunities

Sources:
- Chips and Cheese 2024-07-09: "Qualcomm's Oryon Core: A Long Time in the Making"
- jia.je CPU diagrams: oryon.svg (pipeline layout, scheduler counts)
- LLVM AArch64SchedOryon.td (instruction latencies, port bindings)

## Core Specifications (Laptop / X Elite)

| Structure                | Oryon            | Zen 4 (for reference) |
|--------------------------|------------------|-----------------------|
| Decode width             | 8-wide           | 6-wide (uop cache)    |
| ROB entries              | 680              | 320                   |
| Int register file        | 384+32 = 416     | 224+?                 |
| Int scheduler entries    | 120 (6 pipes)    | 96 (shared w/ mem)    |
| FP/vector scheduler      | 192 (4 pipes)    | 64 (non-sched 128)    |
| Load queue               | 192              | 116                   |
| Store queue              | **56**           | 64                    |
| L1 instruction cache     | 192 KB           | 32 KB                 |
| L1 data cache            | 96 KB            | 32 KB                 |
| L2 (per cluster)         | 12 MB (20 cyc)   | 1 MB (priv) + 16 MB L3|
| L1 DTLB                  | 224 entries      | 72                    |
| L2 TLB                   | >8K entries      | ~2K                   |
| Branch mispredict penalty| 13 cycles        | 13 cycles             |
| Return stack             | 48 entries       | 32                    |
| Indirect branch targets  | 2048 entries     | 3072                  |
| L0 BTB (zero-bubble)     | 8 KB             | 4 KB (M1 only)        |
| L1 BTB (3-cycle)         | 192 KB           | via L1i               |
| Load bandwidth           | 4×128-bit/cyc    | 2×256-bit/cyc         |
| Single-core DRAM BW      | **80 GB/s**      | ~25 GB/s              |

## Oryon-M (Phone / Snapdragon 8 Elite)

Cut-down for mobile power budget:
- 4-wide decode (not 8)
- 4 integer pipes (not 6)
- 2 load/store pipes (not 4)
- 2 FP/SIMD pipes (not 4)

---

## Heresy Q: Return Stack Buffer Spill Prevention

**Uarch fact**: Oryon has a 48-entry RSB. When exceeded, the RSB is CLEARED,
causing a ~13-cycle mispredict on every subsequent `ret`. There is no graceful
overflow mechanism — it's a catastrophic flush.

**Turnip impact**: The Vulkan command buffer recording path has deeply nested
call chains (vkCmdDraw → tu_CmdDraw → tu6_emit_* → tu_cs_emit_* → etc).
If any hot path exceeds 48 call depth, every draw call pays 13+ cycles of
mispredict penalty.

**Proposed injection**: Identify the 3 deepest call chains in command recording
and flatten them via forced inlining or tail-call optimization.
Use `__attribute__((always_inline))` on leaf helpers or restructure
deep recursion into iterative dispatch.

**Expected win**: 5-15% reduction in vkCmdDraw CPU overhead (eliminates RSB
overflow mispredicts on deep call stacks).

**Implementation risk**: Medium (code bloat from forced inlining, need to
measure actual call depth via `perf record -e BR_RETURN_RET` on device).

---

## Heresy R: L0 BTB Hot-Branch Clustering (8 KB Footprint)

**Uarch fact**: Branches within an 8 KB code window get single-cycle
"zero bubble" prediction. Branches outside 8 KB but within 192 KB L1i
get 3-cycle latency. Beyond L1i, it gets much worse.

**Turnip impact**: The command recording hot loop (tu_cmd_buffer.cc) is
spread across multiple translation units. Critical branches in
tu6_emit_flushes, tu6_emit_gmem_restore, tu_cs_reserve, etc. may
straddle 8 KB boundaries, paying 3-cycle latencies instead of zero-bubble.

**Proposed injection**: Use `__attribute__((section(".text.hot")))` and
a linker script to cluster the 8 hottest functions within a single 8 KB
page. Functions: tu6_emit_flushes, tu_cs_reserve, tu_cs_emit_pkt7,
tu_cs_emit_wfi, tu6_emit_gmem_restore, tu_cs_emit_regs.

**Expected win**: 2-5% on draw-call-bound workloads (saves 2 cycles per
taken branch that crosses the 8 KB L0 BTB boundary).

**Implementation risk**: Low (pure linker-level change, no source modification).

---

## Heresy S: Store Queue Pressure Relief

**Uarch fact**: Oryon's store queue is only 56 entries, surprisingly small
compared to the 192-entry load queue and 680-entry ROB. Store-heavy code
can stall the pipeline when the SQ fills up.

**Turnip impact**: PM4 command stream emission is almost entirely stores
(tu_cs_emit_pkt7 writes 4 dwords, tu_cs_emit_regs writes N dwords).
Sequential store bursts (like emitting a register group) fill the SQ
quickly. Oryon's SQ drain rate (to L1) limits sustained store throughput.

**Proposed injection**: 
1. Interleave loads between store bursts to prevent SQ stall.
   After every 8 consecutive stores (tu_cs_emit calls), insert a
   dummy load from the command stream pointer to force SQ drain.
2. Use write-combining store instructions (`stnp` non-temporal hints)
   for PM4 emission — write-combining bypasses the store queue entirely
   and goes to a write-combining buffer.

**Expected win**: 3-8% on PM4-heavy workloads (many register writes per draw).

**Implementation risk**: Medium (non-temporal stores may have coherence
issues on some memory types; needs testing on device).

---

## Heresy T: Quad-Load Burst Alignment for Descriptor Packing

**Uarch fact**: Oryon can sustain 4×128-bit loads per cycle (64 bytes/cyc).
This bandwidth is only achieved when loads are:
1. Aligned to 16-byte boundaries
2. From L1 cache
3. Issued as `ldp q0, q1, [xN]` / `ldp q2, q3, [xN, #32]` pairs

**Turnip impact**: FDL6 descriptor copies (128 bytes = 32 dwords) are
already optimized by Heresy G+P (`__builtin_memcpy(dst, src, 128)`).
But other descriptor structures (like `tu_descriptor_set_binding_layout`,
`tu_image_view`) may not be aligned for optimal quad-load bursts.

**Proposed injection**: 
1. Align `tu_image_view::descriptor` to 64 bytes (cache line)
2. Replace `memcpy` of smaller descriptors (64B, 96B) with 
   `__builtin_memcpy` using compile-time-known sizes for quad-load emission
3. Structure descriptor-set binding to load 4 descriptors at a time
   (extends Heresy DJ to cover the layout walk, not just UBO patching)

**Expected win**: 5-10% on BindDescriptorSets overhead.

**Implementation risk**: Low (alignment annotations, memcpy→builtin_memcpy).

---

## Heresy U: Indirect Branch Predictor Thrift

**Uarch fact**: Oryon's indirect branch predictor has 2048 entries. Zen 4
has 3072. Virtual function dispatch (vtable calls like `cmd->vtbl->Draw()`)
consumes indirect predictor entries. If the Turnip PM4 emission path has
many distinct virtual call sites, the 2048-entry limit could be exceeded.

**Turnip impact**: The Vulkan runtime dispatches many operations through
function pointers (vk_device_dispatch_table, tu_cs callbacks). Hot paths
like draw-call dispatch hit multiple indirect branches in sequence.

**Proposed injection**: 
1. Convert the top 3 virtual calls in the draw path (identified by
   `perf record -e BR_INDIRECT_SPEC`) to switch-based dispatch
   (extends Heresy A pattern to vtable call sites)
2. Use `__builtin_expect()` or profile-guided optimization (PGO) to
   hint the most common target for each indirect call

**Expected win**: 2-5% on heavy descriptor-set-binding workloads.

**Implementation risk**: Medium (needs PGO data from device, or manual
identification of hot vtable calls).

---

## Heresy V: 96 KB L1 Data Cache Footprint Management

**Uarch fact**: Oryon's 96 KB L1D is large (3x Zen 4's 32 KB) but still
finite. Data that spills to L2 pays 20-cycle latency. The L1D is 8-way
set-associative, meaning 8 cache lines compete per set.

**Turnip impact**: PM4 command stream state (`tu_cmd_buffer`, `tu_cs`)
is accessed on every emit call. Descriptor sets can be large (many KB).
If active state exceeds 96 KB, L1 thrashing begins.

**Proposed injection**:
1. Prefetch next descriptor set's layout into L1 during current draw's
   GPU execution (using `__builtin_prefetch` — reinforces Heresy O)
2. Pack frequently-accessed `tu_cmd_buffer` fields into a single
   cache-line-sized struct (64 bytes) at the top of the struct
   (`__attribute__((aligned(64)))` on the hot subset)
3. Use `DC CIVAC` (clean+invalidate to point of coherency) before
   large descriptor updates to avoid unnecessary L1→L2 writeback

**Expected win**: 3-7% on scene-graph workloads with many descriptor sets.

**Implementation risk**: Low (prefetch and struct reordering are safe).

---

## Implementation Priority

| Priority | Heresy | Expected Win | Risk | Effort |
|----------|--------|-------------|------|--------|
| **1** | Q (RSB spill) | 5-15% | Medium | Medium |
| **2** | V (L1D footprint) | 3-7% | Low | Low |
| **3** | S (Store queue) | 3-8% | Medium | Low |
| **4** | T (Quad-load alignment) | 5-10% | Low | Low |
| **5** | R (L0 BTB clustering) | 2-5% | Low | Low |
| **6** | U (Indirect predictor) | 2-5% | Medium | High |

Note: None of these have been probed in assembly yet. Each needs a
prove_heresies-style microbenchmark before injection.
