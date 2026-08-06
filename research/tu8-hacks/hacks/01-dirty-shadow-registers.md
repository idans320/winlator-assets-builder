# Hack 01: Two-Tier Dirty-Bit Shadow Register System

## Vanilla Behavior

Every register write in vanilla Mesa is emitted immediately as a PKT4 command:

```c
tu_cs_emit_pkt4(cs, reg, 1);
tu_cs_emit(cs, value);
```

Problems:
1. **No deduplication** — writing the same register with the same value N times emits N PKT4 packets
2. **No batching** — each register write is a separate PKT4(reg, 1), preventing multi-register PKT4(reg, N) coalescing
3. **No throttling** — state flushes happen on every draw, even when state hasn't changed

## Hack: Two-Tier Dirty-Bit Shadow

### Data Structure

```
Tier 1: blocks[3] — 3 × uint64_t = 192 bits
        Each bit = one block of 32 register slots is dirty somewhere

Tier 2: regs[160] — 160 × uint32_t = 5120 bits
        Each bit = exact register slot (reg>>2) is dirty

Tier 3: shadow[5120] — 5120 × uint32_t = 20KB
        shadow[idx] = most recently written value for register (idx<<2)
```

**Lookup chain** (check if register `idx` is dirty):
1. `blocks[idx >> 11]` — block bit (tier 1, 64-bit granularity)
2. `regs[idx >> 5]` — slot bit (tier 2, 32-bit granularity)
3. `shadow[idx]` — actual value (tier 3)

### tu_cs_set_register(cs, reg, value)

```c
void tu_cs_set_register(struct tu_cs *cs, uint16_t reg, uint32_t value) {
    if (!cs->dirty.enabled || !tu_cs_reg_is_cacheable(reg))
        goto emit;  // fall through for non-cacheable registers

    uint32_t idx = reg >> 2;
    uint32_t block = idx >> 5;
    uint32_t bit = idx & 31;

    // Lazy allocation: first write allocates shadow + regs arrays
    if (unlikely(!cs->dirty.shadow)) {
        cs->dirty.shadow = calloc(DIRTY_MAX_IDX, sizeof(uint32_t));
        cs->dirty.regs   = calloc(DIRTY_BLOCKS, sizeof(uint32_t));
    }

    // REDUNDANT WRITE SUPPRESSION: skip if shadow matches
    if (cs->dirty.shadow[idx] == value)
        return;  // <-- THIS IS THE KEY OPTIMIZATION

    // Store value + mark dirty bits
    cs->dirty.shadow[idx] = value;
    cs->dirty.regs[block] |= (1u << bit);           // Tier 2: slot dirty
    cs->dirty.blocks[block >> 6] |= (1ULL << (block & 63)); // Tier 1: block dirty
    return;

emit:
    tu_cs_emit_pkt4(cs, reg, 1);  // direct PKT4 for uncacheable registers
    tu_cs_emit(cs, value);
}
```

### tu_cs_flush_dirty(cs)

Scans dirty bits and emits compact PKT4 batches. Two modes:

**Default (ffs-scan):**
```c
for (int t = 0; t < DIRTY_TIER1; t++) {       // for each tier-1 block
    uint64_t blocks = cs->dirty.blocks[t];
    while (blocks) {
        int b = __builtin_ffsll(blocks) - 1;  // find next dirty block
        uint32_t regs = cs->dirty.regs[(t<<6) + b];
        while (regs) {
            int bit = __builtin_ffs(regs) - 1; // find next dirty slot
            // Merge consecutive dirty slots into multi-register PKT4
            uint16_t cnt = 1;
            while ((regs >> (bit + cnt)) & 1) cnt++;
            tu_cs_emit_pkt4(cs, base, cnt);    // PKT4(base_reg, count)
            for (uint16_t c = 0; c < cnt; c++)
                tu_cs_emit(cs, cs->dirty.shadow[idx + c]);
        }
    }
}
```

### Non-Cacheable Registers

Certain register ranges bypass the shadow system and emit direct PKT4:

```c
bool tu_cs_reg_is_cacheable(uint32_t reg) {
    return (reg < 0x0010) || (reg >= 0x2000 && reg < 0x4000) ||
           (reg >= 0x4000 && reg < 0x5000);
}
```

- `reg < 0x0010`: CP registers (must be emitted immediately for synchronization)
- `0x2000-0x5000`: Shader/descriptor setup registers (cached)
- Others: Pass-through to direct PKT4

### Initialization in tu_cs_init

```c
if (device->physical_device->info->chip >= A8XX) {
    cs->dirty.enabled = true;
    cs->dirty.shadow = NULL;        // lazy-allocated on first write
    cs->dirty.regs = NULL;          // lazy-allocated on first write
    memset(cs->dirty.blocks, 0, sizeof(cs->dirty.blocks));
    cs->dirty.yolo_sync = !!getenv("TU_YOLO_SYNC");
    cs->dirty.yolo_barrier_needed = false;
    cs->dirty.throttle_n = getenv("TU_SKIP_STATE") ? atoi(getenv("TU_SKIP_STATE")) : 0;
    cs->dirty.throttle_counter = 0;
    cs->dirty.reg_blast = !!getenv("TU_REG_BLAST");
}
```

## Comparison: Hash Table vs Bitmap Approach

This repo contains two variants of the register cache:

| Aspect | Hash Table (patch 0003) | Two-Tier Bitmap (apply-all-hacks.sh) |
|--------|------------------------|--------------------------------------|
| Data structure | 512-slot open-addressing hash | 192-block + 5120-bit bitmap |
| Probe limit | 8 entries | O(1) direct indexing |
| Memory | ~4KB (512 × 8 bytes) | ~21KB (shadow + bitmaps) |
| Insert cost | Up to 16 probes (find + insert) | 1 array write + 2 bitmap ORs |
| Merge PKT4 | No — each register is separate PKT4 | Yes — merges consecutive dirty slots |
| Eviction | None (cache miss = new insert, old evicted) | None (infinite, spans entire 0-0x5000 range) |
| Regs covered | 512 most recent | All 5120 slots (0x0000-0x5000 range) |

The two-tier bitmap is the production variant — it supports PKT4 merging and has
lower insertion cost, at the cost of 5× memory.

## Code Locations

| File | Insertion Point | What's Added |
|------|----------------|-------------|
| `tu_cs.h:118` | After `external_iova` field | `struct dirty` declaration |
| `tu_cs.h:370` | Before `tu_cs_emit_qw()` | `tu_cs_set_register()`, `tu_cs_flush_dirty()`, `tu_cs_flush_barrier_yolo()` |
| `tu_cs.cc:35` | In `tu_cs_init()` | Dirty system initialization (env var reading, lazy alloc setup) |
| `tu_cs.cc:115` | In `tu_cs_finish()` | `free(shadow); free(regs)` |
