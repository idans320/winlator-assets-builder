# Hack 04: REG_BLAST — Brute-Force 64-Register Block Dump

## Problem

The default `tu_cs_flush_dirty` ffs-scan mode iterates through dirty bits one-by-one
using `__builtin_ffs`/`__builtin_ffsll` and merges consecutive dirty slots into
multi-register PKT4 packets. While compact, the bit-scanning loop has overhead:

```
// For each dirty block (64-bit granularity):
while (blocks) {
    int b = __builtin_ffsll(blocks) - 1;  // find next dirty block
    while (regs) {
        int bit = __builtin_ffs(regs) - 1;  // find next dirty slot
        uint16_t cnt = 1;
        while ((regs >> (bit + cnt)) & 1) cnt++;  // merge consecutive
        tu_cs_emit_pkt4(cs, base, cnt);  // emit
        for (c=0; c<cnt; c++) tu_cs_emit(cs, shadow[idx+c]);  // copy 32-bit values
    }
}
```

When the dirty set is **dense** (most registers in a block are dirty), the ffs scan
overhead exceeds the cost of just dumping the whole block.

## Hack

Skip the ffs scan entirely. For each block that has ANY dirty bit set, dump the entire
32-register block as a single PKT4(reg, 32) + 32 dwords:

```c
if (cs->dirty.reg_blast) {
    // For each tier-1 block (groups of 64 blocks × 32 slots = 2048 registers):
    for (int t = 0; t < DIRTY_TIER1; t++) {
        uint64_t blocks = cs->dirty.blocks[t];
        if (!blocks) continue;  // skip entirely clean tier-1 blocks
        while (blocks) {
            int b = __builtin_ffsll(blocks) - 1;  // still need tier-1 ffs
            blocks &= ~(1ULL << b);

            // Dump 32 registers, one PKT4(reg<<7, 32) + 32 dwords
            tu_cs_emit_pkt4(cs, (uint16_t)(((t << 6) + b) << 7), 32);
            uint32_t *src = cs->dirty.shadow + ((t << 6) + b) * 32;
            for (int i = 0; i < 32; i++)
                tu_cs_emit(cs, src[i]);
        }
        cs->dirty.blocks[t] = 0;
    }
    memset(cs->dirty.regs, 0, DIRTY_BLOCKS * sizeof(uint32_t));
    return;
}
```

## Trade-Off

| Aspect | ffs-scan (default) | REG_BLAST |
|--------|-------------------|-----------|
| CPU (sparse dirty set) | Efficient | Wastes dwords on clean registers |
| CPU (dense dirty set) | ffs overhead + merge loop | Single loop, O(blocks) |
| Command stream size | Minimal | Larger (up to 32× per block) |
| GPU command processor load | Lower | Higher (more PKT4 to parse) |
| Best for | Isolated writes (few registers changed) | Batched writes (most registers changed) |

## When to Use

REG_BLAST outperforms ffs-scan when:
- >60% of registers in a block are dirty (break-even: ffs overhead ≈ extra dwords)
- The workload is heavily CPU-bound (GPU can handle more PKT4, CPU saved)
- State transitions involve near-complete register rewrites

## Activation

```bash
TU_REG_BLAST=1 ./app
```
