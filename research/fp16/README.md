# FP16 Buffer Compression Research

DXVK GPU buffer traffic → FP16 compression analysis for Adreno Gen8.

## Tools

### fp16-buffer-analyze
Models DXVK buffer traffic per game frame and estimates bandwidth savings from FP16 compression.

```bash
go run ./fp16/cmd/fp16-buffer-analyze/ -output output/fp16_analysis.json
```

### bench-vulkan --fp16-bench
Termux-native Vulkan benchmark that measures FP16 vs FP32 compute throughput on real hardware.

```bash
./bench-vulkan --fp16-bench --iterations 50 --output fp16_results.json
```

Outputs device-level fp16_capabilities probe and FP16/FP32 median GPU times with speedup ratio.

## Compression Strategies (by insertion point)

| Strategy | Impact | Risk | Enable Flag |
|---|---|---|---|
| **vertex-fp16** | 50% VB bandwidth savings | Low (no shader changes) | `DXVK_VERTEX_FP16=1` |
| **uniform-fp16-pack** | 45% UBO bandwidth savings | Medium (precision loss on world coords) | `DXVK_UNIFORM_FP16=1` |
| **storage-fp16** | 35% SSBO bandwidth savings | Medium (needs VK_KHR_16bit_storage + shader recompile) | `DXVK_STORAGE_FP16=1` |

### Priority: vertex-fp16 (free) > uniform-fp16-pack (small shader cost) > storage-fp16 (biggest win, needs shader work)

## DXVK Insertion Points

### Level 1: Vertex Buffer Format (highest ROI, lowest risk)
- **File**: `src/dxvk/dxvk_format.h` / `dxvk_format.cpp`
- **Mechanism**: When creating vertex buffer views, detect if all bound attributes use float32 and can drop to float16. Change `VkFormat` to `VK_FORMAT_R16G16B16A16_SFLOAT` equivalents.
- **Shader cost**: None — vertex fetch hardware reads 16-bit natively.
- **Impact**: ~50% vertex bandwidth reduction. Directly reduces GMEM tile memory pressure.

### Level 2: Constant/Uniform Buffer Compression
- **File**: `src/dxvk/dxvk_context.cpp` (`pushData()`, `bindUniformBuffer()`)
- **Mechanism**: On host-side upload, pack float32 values into float16 pairs. Shader unpacks via `unpackHalf2x16()`.
- **Shader cost**: 1 ALU op per unpacked value. On Adreno, FP16 ALU is free (dedicated unit).
- **Impact**: ~45% uniform bandwidth reduction. Cuts per-draw descriptor traffic.

### Level 3: Storage Buffer 16-bit
- **File**: `src/spirv/spirv_module.h` / `spirv_code_buffer.h`
- **Mechanism**: Add `Float16` capability, `putFloat16()`, native `float16_t` type support.
- **Shader cost**: None with native FP16 — 2x ALU throughput vs FP32 on Adreno.
- **Impact**: ~35% SSBO bandwidth + halved ALU cost for FP16-heavy compute.

## Adreno Gen8 Context

- Adreno 8xx has dedicated FP16 ALUs with 2x the throughput of FP32
- Tile-based rendering (GMEM) — bandwidth is the primary bottleneck
- FP16 halves L2 cache eviction, directly improving binning/render pass bandwidth
- Turnip driver supports `VK_KHR_16bit_storage` and `VK_KHR_shader_float16_int8`
- Combined 3-strategy estimate: ~40% total buffer bandwidth reduction per frame

## Related

- `benchmark/` — Termux Vulkan benchmark with FP16 throughput comparison
- `dxvk/` — DXVK build component (target for FP16 patches)
- `mesa/` — Mesa Turnip driver (already has FP16 support at driver level)
