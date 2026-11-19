# DXVK 3.1.1 on ARM64EC — device results

Stock upstream DXVK 3.1.1 vs the previous 2.6.8 GPLAsync-LowLatency build, both
cross-compiled here for ARM64EC, run on the phone in the same container.

**3.1.1 is the default since this measurement** (`packages.yml`): D3D11 is ~2.3x
faster end-to-end, and the D3D9 draw-cost gap only shows up on synthetic
trivial draws — 2002-2009 D3D9 titles are far below the threshold where it
matters. D3D9 only runs at all with Turnip 26.3+, which is a packaging
requirement, not an optional pairing.

## Environment

| | |
|---|---|
| Device | CPH2653 (OnePlus 13), Adreno 830v1 |
| Wine | Wine-11-arm64ec-8 |
| DXVK 3.x | `3.1.1-arm64ec-upstream-1` (this repo, no patches) |
| DXVK 2.x | `2.6.8-arm64ec-gplasync-1` (this repo) |
| Graphics stack | Winlator wrapper (`libvulkan_wrapper.so`) + our Mesa Turnip 26.3.0-devel (`git-0eac30c884`), Vulkan advertised as 1.3.363 |
| Container | "Test" (id 5), 1280x720, FEXCore, DXVK+HUD, `DXVK_LOG_LEVEL=info` |
| Probe | `tools/arm-test/apps/d3d_smoke/smoke.exe`, x86_64 PE through FEX |

Both builds report `Vulkan: 1.4` class features and `Graphics pipeline libraries
supported`; 3.1.1 takes the `Legacy` binding model on this driver (no descriptor
buffer), 2.6.8 does not log a binding model.

## Which driver actually ran

"Wrapper driver 26.2.99" in DXVK's log is **not** the loaded driver's version — it
is the wrapper's own string. Verified from inside the guest by dumping the
wrapper's plumbing from the probe:

```
ADRENOTOOLS_DRIVER_PATH=.../contents/adrenotools/Mesa Turnip Driver 26.3.0-devel/
ADRENOTOOLS_DRIVER_NAME=vulkan.turnip.so
WRAPPER_VK_VERSION=1.3.363
```

- That `.so` on the device is byte-identical to our package
  (`md5 bc0467ff7e97bdc68e2bff301c7967bf`) and `strings` reports
  `Mesa 26.3.0-devel (git-0eac30c884)` — so the guest **is** on our driver.
- `WRAPPER_VK_VERSION` comes from `graphicsDriverConfig.vulkanVersion`, which
  was `1.3` → the wrapper advertises Vulkan **1.3** even though the driver is
  1.4.363. Setting it to `1.4` yields `WRAPPER_VK_VERSION=1.4.363`.
- The wrapper does **not** expose descriptor buffers. DXVK's own feature dump:
  `VK_EXT_descriptor_buffer: descriptorBuffer: 0`,
  `VK_EXT_descriptor_heap: descriptorHeap: 0`. Hence
  `Binding model: Legacy` on every run, under both 1.3 and 1.4 — one of 3.x's
  headline features is simply unreachable on this stack today.

Raising the version to 1.4 changed nothing in the D3D9 result (11.1 ms vs
13.2 ms, same order), so the gap below is not a binding-model artifact.

## 3.1.1 needs a newer driver than the bundled one

Same shortcut, only the container's driver switched to the APK's bundled
`turnip26.2.0`:

| | bundled turnip 26.2.0 | our Turnip 26.3.0-devel |
|---|---|---|
| D3D11 (bench11) | works — 0.20 / 2.23 ms | works — 0.17 / 1.87 ms |
| D3D9 (bench9) | **aborts** | works — 0.79 / 11.10 ms |
| D3D9 (2.6.8) | works — 0.74 / 2.04 ms | works — 0.69 / 1.77 ms |

The 3.1.1 D3D9 abort is a `vkCreateGraphicsPipelines` failure surfacing as a
Wine assert:

```
err:msvcrt:_wassert (L"!status && \"vkCreateGraphicsPipelines\"",
                      L"dlls/winevulkan/loader_thunks.c",3326)
```

So DXVK 3.x's D3D9 path is not usable on the APK's bundled Turnip, while 2.6.8
is fine on both drivers. Whoever ships 3.x has to ship 26.3 alongside it.


## Results

60-frame paths are end-to-end (device + swapchain + present, first-use pipeline
compile included). Bench paths are offscreen, no present.

| Test | 2.6.8 GPLAsync | 3.1.1 upstream |
|---|---|---|
| D3D11 smoke, 60 frames | 113.1 ms — 530 fps | **48.4 ms — 1239 fps** |
| D3D9 smoke, 60 frames | 65.0 ms — 924 fps | **52.2 ms — 1150 fps** |
| bench11 steady, 4096 draws — cpu / sync | 0.18 / 2.01 ms | 0.17 / **1.87** ms |
| bench11 churn, 128 pipelines — cpu / sync | 0.02 / 2.72 ms | 0.02 / **1.95** ms |
| bench9 steady, 4096 draws — cpu / sync | 0.69–1.62 / **1.77–2.46** ms | 0.62–1.40 / 11.3–12.6 ms |
| bench9 churn, 128 states — cpu / sync | 0.08 / **1.06–3.68** ms | 0.08 / 15.0–15.2 ms |

Reproduced: 3.1.1 bench9 was run twice (11.28, 12.64 ms), 2.6.8 twice
(2.46, 1.77 ms). `cpu_ms` is time inside D3D calls; `sync_ms` is until the GPU
drains, forced with an event query.

## Reading

- **D3D11: 3.1.1 wins.** ~2.3× faster end-to-end on the same trivial scene, with
  equal submit cost once pipelines exist. Expected: dxbc-spirv compiles off the
  app thread and the pipeline libraries are cached.
- **D3D9: mixed.** 3.1.1 is faster end-to-end (1150 vs 924 fps) but its
  per-draw GPU drain is ~6× slower (12 ms vs 2 ms for 4096 draws). The
  fixed-function ubershader path is the prime suspect. This is the one number
  that could show up as a regression in D3D9-heavy games and it needs a real
  game to confirm.
- **Async is not a factor at this workload.** Churn cost is within noise on
  D3D11 — 3.x already dispatches pipeline work to workers, so 2.6.8's
  `DXVK_ASYNC` has nothing left to win here. Porting async to 3.x looks
  unnecessary unless a real game disagrees.

## Caveats

- Single runs on a phone, no thermal control; treat ±20 % as noise. Only the
  D3D9 6× gap is large enough to be structural.
- Both builds ran on the same driver (our Turnip 26.3, see above) and the same
  wrapper settings, so the comparison stands.
- The container advertises Vulkan 1.3 (`graphicsDriverConfig.vulkanVersion`).
  Raising it to 1.4 did not change the D3D9 result.
- 32-bit (`syswow64`) DLLs are built but were never run: the harness has no
  wow64 prefix and no 32-bit test was done on device.

## Reproducing

```bash
tools/winlator-debug/deploy-smoke.sh 5                      # 3.1.1 shortcuts
DXVK_VERSION=2.6.8-arm64ec-gplasync-1 SUFFIX=-268 \
  tools/winlator-debug/deploy-smoke.sh 5                    # 2.6.8 shortcuts
# launch one, then read the report the guest writes next to itself:
adb shell "run-as com.ludashi.benchmark \
  cat files/imagefs/home/xuser-5/.wine/drive_c/smoke/result.txt"
```

The guest's stdout/stderr never reaches Winlator's log, so the probe writes
`C:\smoke\result.txt` and every line is pulled from there.

The 2.6.8 comparison build is no longer produced by `dxvk/build.sh`; re-testing
it means pointing `packages.yml` at `Digger1955/dxvk-gplall`
(`GPLALL-master-2.6.8`) again — the script detects the source change and rebuilds
from a clean tree.
