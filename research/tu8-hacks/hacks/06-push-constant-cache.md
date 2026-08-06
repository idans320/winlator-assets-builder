# Hack 06: Push Constant Cache (Probe-Level)

## Problem

DXVK calls `vkCmdPushConstants` on **every draw call**, even when the push constant
data hasn't changed since the previous draw. Each push constant update generates:

1. CPU-side: `tu_CmdPushConstants` → memcpy to command buffer → PKT4 emission
2. GPU-side: Command processor parses PKT4 → writes push constant registers

For workloads where push constants don't change between draws, these are pure waste.

## Hack

Cache the last push constant value per slot (stage + offset + size) and skip
re-emission when the value is unchanged:

```c
// loader_probe.c — DXVK workload simulator
struct pc_cache_entry {
    VkShaderStageFlagBits stage;
    uint32_t offset;
    uint32_t size;
    uint32_t value;         // up to 4 bytes (most PC slots are uint32)
};

struct pc_cache {
    struct pc_cache_entry entries[8];  // 8-slot fixed cache
    int count;
    bool dirty;
};

void cached_push_constants(VkCommandBuffer cmd, VkPipelineLayout layout,
                           VkShaderStageFlags stage, uint32_t offset,
                           uint32_t size, const void *data)
{
    uint32_t val = *(uint32_t *)data;

    // Check cache
    for (int i = 0; i < cache.count; i++) {
        if (cache.entries[i].stage == stage &&
            cache.entries[i].offset == offset &&
            cache.entries[i].size == size) {
            if (cache.entries[i].value == val) {
                push_skipped++;       // HIT — skip vkCmdPushConstants
                return;
            }
            cache.entries[i].value = val;
            cache.dirty = true;
            goto emit;
        }
    }

    // Insert new entry
    cache.entries[cache.count++] = (struct pc_cache_entry){
        .stage = stage, .offset = offset, .size = size, .value = val
    };
    cache.dirty = true;

emit:
    push_emitted++;
    vkCmdPushConstants(cmd, layout, stage, offset, size, data);
}
```

## Integration Status

**Probe-level only.** This hack is implemented in `loader_probe.c` as a simulation
of the DXVK push constant pattern. It is NOT integrated into the `apply-all-hacks.sh`
injection script because:

1. **DXVK-side fix is preferable** — DXVK can check if push constants changed
   before calling `vkCmdPushConstants` (application-level optimization).
2. **Turnip-side implementation needs descriptor-aware caching** — the driver
   doesn't know which push constant slots are "sticky" (set once, used many draws)
   vs. "streaming" (changes every draw).
3. **General solution is complex** — requires tracking per-pipeline-layout,
   per-shader-stage, per-offset state across draw calls, not just within one
   command buffer.

## Future Integration

A production-ready Turnip-side implementation would:

1. Add `pc_cache` to `tu_cmd_buffer` (similar to `regcache` in `tu_cs`)
2. Override `tu_CmdPushConstants` to check cache before writing to command stream
3. Invalidate cache on pipeline bind (different pipeline = different PC layout)
4. Use hash of the push constant data for comparison (not just first 4 bytes)

## Impact (Estimated)

Based on DXVK workload simulation:
- **25-40% fewer `vkCmdPushConstants` calls** in typical DXVK rendering
- Translates to ~5-10 PKT4 packets saved per draw that reuses push constants
