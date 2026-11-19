# Winlator Assets Builder

> ⚠️ **Active development.** Currently tuned and tested only for **Snapdragon 8 Elite X** (Oryon-1 CPU).

Devbox-based cross-compilation build system that produces custom asset packages (`.wcp`/`.zip` archives) for [Winlator](https://winlator.org/) — an Android app that runs Windows x86/x86_64 applications on ARM64 Android devices.

## What is this?

Winlator loads components (Wine, graphics drivers, CPU emulators) as installable `.wcp`/`.zip` asset packages. This builder cross-compiles five upstream open-source projects for ARM64 Android/Windows-on-ARM targets with aggressive Qualcomm Oryon-1 CPU tuning, producing ready-to-install asset packages.

All builds are optimized for the **Snapdragon 8 Elite X** platform (`-mcpu=oryon-1`, tuned Config.json presets) and may require adjustments for other SoCs.

## Components

| Package | Description |
|---|---|
| **Mesa Turnip** | Freedreno Turnip Vulkan driver for Adreno GPUs, built for Android (Bionic libc) targeting the KGSL kernel interface. Produces `vulkan.turnip.so` with metadata. |
| **FEXCore** | CPU emulation layer (core of FEX-Emu) cross-compiled as Windows ARM64 PE DLLs via llvm-mingw. Provides x86/x86_64 emulation for Wine on ARM64. Ships with a performance-tuned `Config.json`. |
| **Wine 11 Proton** | Valve's Proton fork of Wine (Proton 11.0-2), built for ARM64 Linux with ARM64EC support. Multi-architecture: `arm64ec, aarch64, i386, x86_64`. Links a userspace ntsync library (Rust) for fast synchronisation on Android, ships with `Config.json`, `profile.json`, and ELF interpreter patching. |
| **Box64** | x86_64-to-ARM64 dynamic recompiler built as an Android native binary via the NDK. ARM dynarec enabled. |
| **DXVK 3.x** | Upstream DXVK (dxbc-spirv shader compiler, descriptor heaps). D3D8/9/10/11 to Vulkan translation layer, cross-compiled as ARM64EC + i686 PE DLLs. Needs a Vulkan 1.4 driver (Mesa Turnip 26.3+). |

## Prerequisites

- [Devbox](https://www.jetify.com/devbox/docs/installing_devbox/)
- Git
- Internet connection (fetches Android SDK/NDK, source repos, llvm-mingw toolchains)

## Quick Start

Build all packages:

```bash
./build-all.sh
```

Build a single package:

```bash
devbox run -- bash mesa/build.sh
devbox run -- bash fexcore/build.sh
devbox run -- bash wine/build.sh
devbox run -- bash box64/build.sh
devbox run -- bash dxvk/build.sh
```

Or using Make:

```bash
make mesa
make fexcore
make wine
make box64
make dxvk
```

## Outputs

| Component | File |
|---|---|
| Mesa Turnip | `mesa-<version>.zip` |
| FEXCore | `fexcore-<version>.wcp` |
| Wine 11 Proton | `wine-arm64-<version>.wcp` |
| Box64 | `box64-<version>.zip` |
| DXVK 3.x | `<version>-arm64ec.wcp` |

These files are the asset packages that Winlator loads at runtime.

## Project Structure

```
.
├── build-all.sh            # Orchestrator: iterates packages.yml, runs builds in Devbox shells
├── Makefile                # Convenience targets for single-package builds
├── packages.yml            # Package manifest (repos, branches, versions, presets)
├── devbox.json             # Devbox config: hermetic build environment, SDK/NDK, toolchains
├── devbox.lock             # Pinned Devbox dependencies for reproducibility
├── presets/
│   └── elitex.yml          # CPU tuning presets (Oryon-1 CFLAGS/CXXFLAGS/LDFLAGS)
├── include/                # Vendored headers (drm.h) for Mesa build
├── mesa/
│   └── build.sh            # Builds Turnip Vulkan driver
├── fexcore/
│   └── build.sh            # Builds FEXCore WOW64 DLLs
├── wine/
│   ├── build.sh            # Builds Wine 11 Proton ARM64 + ARM64EC
│   └── patches/            # Local patches applied after upstream android/patches
├── box64/
│   └── build.sh            # Builds Box64 emulator
└── dxvk/
    └── build.sh            # Builds DXVK 3.x (ARM64EC + i686 PE)
```

## How It Works

1. `packages.yml` defines the five components with source repos, branches, output filenames, and optional tuning presets.
2. `build-all.sh` reads the manifest and runs each component's `build.sh` under `devbox run`.
3. `devbox.json` provisions Android SDK/NDK, LLVM/Clang toolchains, Meson, Ninja, CMake, and other build dependencies. Each `build.sh` downloads llvm-mingw on demand for PE cross-compilation.
4. Each `build.sh` clones the upstream repository, configures and builds using the native build system (Meson, CMake, Autotools) with Oryon-1 CPU tuning from `presets/elitex.yml`, and packages the output into a `.wcp` or `.zip`.

All tooling is pinned by Devbox for fully reproducible builds.

## Contributing

Contributions are welcome. Feel free to open issues or submit pull requests for:

- Adding support for other SoCs / CPU targets
- New asset/component packages
- Build system improvements
- Documentation fixes

## License

MIT
