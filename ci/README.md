# CI for the Winlator asset builder

Three layers, deliberately separated:

| Layer | Runs on | Proves |
|---|---|---|
| **Cross-build** (`ci/Dockerfile.build`) | x86_64 | every PE component compiles with the pinned toolchains |
| **Static gate** (`ci/check-machine-types.sh`) | x86_64 | the artifacts have the right PE machine types and package layout |
| **Runtime** (`ci/build-android-rootfs.sh` + `ci/run-android-rootfs-tests.sh`) | ARM64 Android runtime | Wine's loader maps the DLLs, imports resolve, DllMain runs |

The first two run on every PR. The runtime jobs execute **our built
`wine-arm64-*.wcp`**: they do not require a Wine build. `resolve-wine` takes the
package from `build-wine` when that job ran, or from an existing package supplied
by URL (dispatch input `wine_package_url` or repository variable
`WINE_PACKAGE_URL`). A fresh `build-wine` is optional and only needed when no
package exists yet.

---

## Job matrix

| Job | Runner | Needs | What it executes | Default trigger |
|---|---|---|---|---|
| `build-x64` | `ubuntu-latest` | — | `fexcore/build.sh`, `dxvk/build.sh`, `ci/build-test-programs.sh` in `Dockerfile.build` | PR, push, dispatch |
| `static-check` | `ubuntu-latest` | `build-x64` | `ci/check-machine-types.sh` (no execution) | PR, push, dispatch |
| `build-wine` | `ubuntu-latest` | — | optional `wine/build.sh --setup` in `Dockerfile.build`, then the gate | schedule, dispatch (`build_wine`) |
| `resolve-wine` | `ubuntu-latest` | `build-wine` (optional) | selects our `wine-arm64-*.wcp` (artifact or URL), fetches Termux deps, runs the gate | when a package is available |
| `test-android-rootfs` | `ubuntu-24.04-arm` | build + gate + resolve-wine | the **shipped bionic Wine** inside a chroot built from an Android system image | schedule, dispatch |
| `test-android` | `ubuntu-24.04-arm` (+ KVM) | same | the same Wine in an `arm64-v8a` AVD, over adb | schedule, dispatch |
| `test-android-device` | `[self-hosted, android, arm64]` | same | the same Wine on a physical Android/Termux device | repo var `ANDROID_DEVICE_RUNNER=true` |
| `test-linux-dlls` | `ubuntu-24.04-arm` | build + gate | the PE DLLs under a **Linux glibc ARM64X Wine** (not the Android package) | dispatch (`run_linux_dll_tests`) |

Repository variables that gate the runtime jobs:

| Variable | Effect |
|---|---|
| `ANDROID_ROOTFS=false` | skip the rootfs job |
| `ANDROID_EMULATOR=false` | skip the emulator job |
| `ANDROID_DEVICE_RUNNER=true` | enable the self-hosted Android/Termux job |

### Using an existing Wine package

The runtime jobs never build Wine unless asked. Point them at an existing
`wine-arm64-*.wcp`:

- Dispatch with `wine_package_url=<direct URL>` (and optionally
  `termux_rootfs_url=<termux-rootfs.txz>`).
- Or set the repository variable `WINE_PACKAGE_URL` to the same URL so scheduled
  runs use it.

`resolve-wine` downloads it, runs `ci/check-machine-types.sh` against it, fetches
the Termux deps with `wine/build.sh --setup-deps` when no rootfs archive was
supplied, and uploads `wine-package` + `termux-rootfs` for the Android jobs. For
private repositories, use a signed URL or an uploaded artifact, since the job
uses plain `curl`.

`ubuntu-24.04-arm` is GitHub-hosted for public repositories. Private repos must
either enable the larger ARM64 runners or set the job to a self-hosted ARM64
label. No runtime job runs on an x86_64 runner: see the next section.

---

## Why the runtime test needs Android (and cannot use a Linux container)

The Wine package is **not** a Linux build:

```
$ readelf -l wine/workdir/package/lib/wine/aarch64-unix/wine
  [Requesting program interpreter: /system/bin/linker64]
```

- It is built with `--host=aarch64-linux-android28` (bionic, NDK sysroot).
- Its RUNPATH is `/data/data/com.termux/files/usr/lib`, hardcoded in Wine and in
  the Termux libraries.
- The Termux rootfs that `wine/build.sh --setup-deps` bootstraps contains
  `data/data/com.termux/files/usr` **only** — no `/system`, no `libc.so`, no
  `libdl.so`. Those come from the Android platform.
- `bin/wine` is a symlink to `lib/wine/aarch64-unix/wine`; the interpreter path
  is absolute.

Therefore:

| Approach | Result |
|---|---|
| `ubuntu-24.04-arm` + the `.wcp` directly | **fails** — no `/system/bin/linker64`, no bionic libc |
| `proot` over the downloaded Termux rootfs | **fails** — the rootfs has no `/system` and no `libc.so`/`libdl.so` |
| chroot with an Android `/system` image + Termux prefix | **works** (this is what `test-android-rootfs` does) |
| arm64-v8a Android emulator / real device | **works** (adb path) |
| `qemu-user` emulating the whole Wine process | works only as a slow fallback; FEX needs to execute ARM64 itself, and full-process emulation is extremely slow and has crashed. Never use it as the primary gate. |

The supplementary `test-linux-dlls` job builds a **glibc** ARM64X Wine
(`ci/build-wine-test.sh`, `--enable-archs=arm64ec,aarch64,i386 --with-mingw=clang`).
It validates the PE DLLs (machine types, imports, DllMain) but **not** the
Android Wine package.

---

## Android runtime rootfs (exact recipe)

`ci/build-android-rootfs.sh` implements this; the manual steps are:

### 1. Get an Android system image (needs `/system/bin/linker64`)

```bash
# arm64-v8a, API 28 (matches the Wine target sdk 28). ~1 GiB download.
sdkmanager --install "system-images;android-28;default;arm64-v8a"
SYSIMG="$ANDROID_HOME/system-images/android-28/default/arm64-v8a/system.img"
```

The image is a sparse ext4:

```bash
simg2img "$SYSIMG" system.raw.img                 # android-sdk-libsparse-utils
debugfs -R "rdump /system $PWD/system" system.raw.img   # e2fsprogs
```

### 2. Assemble the rootfs

```bash
ROOT=ci-root
mkdir -p "$ROOT"
cp -a system "$ROOT/system"                                  # /system/bin/linker64, lib64, ...
mkdir -p "$ROOT/data/data/com.termux/files"
cp -a wine/workdir/termux-rootfs/data/data/com.termux/files/usr \
      "$ROOT/data/data/com.termux/files/usr"                 # exact path, do not relocate
mkdir -p "$ROOT/opt/wine"
tar -xJf wine-arm64-*.wcp -C "$ROOT/opt/wine"                 # bin/, lib/, share/, prefixPack.txz
mkdir -p "$ROOT/opt/prefix"
tar -xJf "$ROOT/opt/wine/prefixPack.txz" -C "$ROOT/opt/prefix"
```

`ci/build-android-rootfs.sh` additionally extracts `fexcore`/`dxvk`, extracts the
test exes, scans every `.so` RUNPATH and fails if an absolute path does not
resolve inside the rootfs (it auto-symlinks `/usr/lib` to the Termux lib dir
when a binary still references it).

### 3. Run

```bash
chroot ci-root /system/bin/sh /opt/ci/run-in-rootfs.sh
# or let the driver do it, with logging:
ci/run-android-rootfs-tests.sh --root ci-root
```

The in-rootfs driver sets `ANDROID_ROOT=/system`, `ANDROID_DATA=/data` and

```
LD_LIBRARY_PATH=/opt/wine/lib/wine/aarch64-unix:/data/data/com.termux/files/usr/lib:/system/lib64
```

then:

1. asserts `bin/wineboot` exists (packaging requirement),
2. uses the pre-extracted prefix, retargets `dosdevices/c:` and runs
   `bin/wineboot -u` once (best effort),
3. copies `libarm64ecfex.dll` + `libFEXCore.dll` + `libwow64fex.dll` into the
   prefix `system32` (Wine's ntdll loads `C:\windows\system32\libarm64ecfex.dll`
   for x64 guests),
4. copies DXVK into `system32` (ARM64EC/ARM64X) and `syswow64` (i386),
5. runs `load_dlls_x64.exe` (FEX/ARM64EC), `load_dlls_i386.exe` (WoW64) and
   `load_dlls_arm64.exe` (native).

On x86_64, the same rootfs works with qemu-user-static and binfmt registered
(`docker run --privileged multiarch/qemu-user-static --reset -p yes`), then
`ci/run-android-rootfs-tests.sh --root ci-root --runner qemu`. Slow; not the
default.

### Emulator / device variant

`ci/android-run-tests.sh` pushes the same layout:

1. `adb root`, `mkdir -p /data/data/com.termux/files`,
2. push the Termux prefix to `/data/data/com.termux/files/usr`,
3. push the extracted Wine payload, components, tests and prefix to
   `/data/local/tmp/winlator-ci`,
4. run `/system/bin/sh /data/local/tmp/winlator-ci/ci/run-in-rootfs.sh` with the
   env vars pointing at those directories,
5. pull the logs.

`/system` already exists on the device, so only the Termux prefix and the Wine
payload are pushed. `adb root` is required for `/data/data`; the CI emulator uses
the userdebug AOSP image (`target: default`), where it works. On a real device,
either use a rooted image or run the script with `--local` from Termux.

---

## Static gate rules (`ci/check-machine-types.sh`)

| Artifact | Assertion |
|---|---|
| `fexcore/system32/libarm64ecfex.dll` | exactly `0xA641` (ARM64EC) |
| `fexcore/system32/libFEXCore.dll`, `libwow64fex.dll` | `0xAA64` (ARM64) |
| `fexcore` layout | `profile.json`, `Config.json`, flat `system32/` |
| `dxvk/system32/*.dll` | `0xA641` or `0xA64E` — **never** `0xAA64` |
| `dxvk/syswow64/*.dll` | `0x14C` (i386) |
| `wine/lib/wine/aarch64-windows/{ntdll,kernel32,user32,gdi32,advapi32,ucrtbase,msvcrt,combase,ole32,shell32}.dll` | `0xA64E` (ARM64X) |
| `wine/lib/wine/aarch64-windows/*` | `{0xA64E, 0xAA64, 0xA641}`, anything else fails |
| `wine/lib/wine/i386-windows/*` | `0x14C` |
| `wine/bin/wine`, `bin/wineboot`, `bin/winecfg`, `bin/wineserver`, `bin/wine-preloader` | present (prefix creation fails without `bin/wineboot`) |
| `wine/lib/wine/aarch64-windows/wineboot.exe`, `winecfg.exe`, `prefixPack.txz` | present |
| `tests/load_dlls_x64.exe` / `_i386.exe` / `_arm64.exe` | `0x8664` / `0x14C` / `0xAA64` |

Machine types are read with `llvm-readobj --file-headers`, falling back to
`llvm-objdump`, then a small `python3` PE-header reader. `file(1)` is **not**
used for pass/fail: it reports ARM64X as "ARM64" and ARM64EC as "x86-64".

`--allow-legacy-dxvk-arm64` exists only as a transition escape hatch for the
period before DXVK's ARM64EC build landed; it prints a loud warning and should
not be used on `main`.

The gate also prints the ELF interpreter of `bin/wine`, so it is obvious in the
log whether a package is bionic (Android) or glibc.

---

## Toolchain pins

The build image bakes in the versions from `packages.yml`; the workflow fails if
they diverge:

| Tool | Pin | Where |
|---|---|---|
| upstream llvm-mingw | `20260519` | `packages.yml: {fexcore,wine}.mingw_ver` |
| ARM64EC llvm-mingw (bylaws) | `20250920` | `packages.yml: toolchain.arm64ec_mingw` |
| Android NDK | `29.0.14206865` | `devbox.json`, `ci/Dockerfile.build` |
| `yq` | `v4.44.3` | `ci/Dockerfile.build` |

The bylaws toolchain is the only one with ARM64EC runtime libraries; FEX's
`libarm64ecfex.dll` needs it. The upstream toolchain builds the plain ARM64 and
i686 DLLs, and Wine's ARM64X PE modules link fine against either.

Environment variables the component scripts read (set by the image):

| Variable | Consumer |
|---|---|
| `NDK`, `ANDROID_HOME` | `wine/build.sh`, `mesa/build.sh`, `box64/build.sh` |
| `ARM64EC_MINGW_ROOT` | `fexcore/build.sh` (ARM64EC step) |
| `LLVM_MINGW_ROOT` | this CI's helper scripts |

---

## Reproducing locally

```bash
# 1. builder image
docker build -f ci/Dockerfile.build -t winlator-builder ci/

# 2. cross-build + PE tests (artifacts land in the workspace)
docker run --rm -v "$PWD:/src" -w /src \
    --user "$(id -u):$(id -g)" -e HOME=/tmp winlator-builder bash -lc '
      ./fexcore/build.sh && ./dxvk/build.sh && ./ci/build-test-programs.sh'

# 3. static gate
ci/check-machine-types.sh --fexcore fexcore-*.wcp --dxvk dxvk-*.wcp \
    --tests ci/build/tests

# 4. Android package, runtime (on an ARM64 Linux host with root)
ci/build-android-rootfs.sh --root ci-root \
    --sdk-package "system-images;android-28;default;arm64-v8a" \
    --termux-rootfs wine/workdir/termux-rootfs \
    --wine wine-arm64-*.wcp --fexcore fexcore-*.wcp --dxvk dxvk-*.wcp \
    --tests ci/build/tests
ci/run-android-rootfs-tests.sh --root ci-root

# 5. device/emulator
ci/android-run-tests.sh --serial emulator-5554 \
    --wine wine-arm64-*.wcp --fexcore fexcore-*.wcp --dxvk dxvk-*.wcp \
    --termux-rootfs wine/workdir/termux-rootfs
```

`ci/Dockerfile.test` packages the adb driver + static gate for a self-hosted
runner (`ci/docker-build-test-image.sh` stages the context and builds it for
`linux/arm64`). It is a **driver kit, not a runtime**: it cannot execute the
bionic Wine, and its `docker run` needs an attached Android target (or `static`
mode).

### Caching / runtime budget

| Path | Cached by | Why |
|---|---|---|
| builder image layers (NDK, llvm-mingw) | `docker/build-push-action` GHA cache | ~1.5 GiB of downloads |
| `wine/workdir/termux-rootfs`, `wine/workdir/deb-cache` | `actions/cache` | `--setup-deps` network bootstrap |
| `wine/workdir/wine` | `actions/cache` | shallow clone, refreshed with `git fetch` |
| `~/android-sdk` (system image) | `actions/cache` | ~1 GiB `sdkmanager` download |
| `ci/build/wine-test`, `toolchain/` | `actions/cache` | Linux ARM64X Wine + ARM64EC toolchain |

Typical wall clock: `build-x64` 30–60 min (fexcore is two FEX builds, dxvk is two
meson builds), `static-check` ~2 min, `build-wine` 60–120 min, rootfs job
10–20 min after the wine artifact exists, emulator job 20–40 min (1.9 GiB adb
push).

---

## Known limitations and gaps

- **Runtime coverage is schedule/dispatch only.** PR CI is the x64 static gate.
  A PR that changes Wine/FEX/DXVK semantics without changing machine types can
  still merge without a runtime signal.
- **`build-wine` is optional.** Runtime jobs consume `resolve-wine`, which takes
  an existing package by URL; `build-wine` is only for producing a fresh one. It
  was the expensive dependency; every runtime job consumes its
  artifacts, so they only run when Wine was built in the same workflow run.
  There is no "reuse the last good wine artifact" mechanism yet.
- **Emulator availability.** The arm64-v8a AVD needs an ARM64 host with KVM.
  GitHub's `ubuntu-24.04-arm` runners are the intended host but KVM availability
  is an assumption to verify; the rootfs job is the more portable primary path.
- **`adb root` is required** for `/data/data/com.termux/files`. Only userdebug/
  eng images allow it. Physical devices need `--local` inside Termux instead.
- **The Termux prefix is a build artifact.** It is produced by
  `wine/build.sh --setup-deps` (network, `packages.termux.dev`); without it the
  rootfs job cannot run. Only the library packages are cached, not `bash` —
  the in-rootfs driver is POSIX sh on purpose.
- **No GPU.** The tests prove loader machine types, imports and DllMain. They do
  not create a D3D device, so DXVK's Vulkan init path is untested.
- **Linux ARM64X Wine is a test fixture**, built from the same branch but
  without the repo's Android patches; it validates the PE artifacts only.
- **`file(1)` cannot distinguish ARM64X/ARM64EC.** The gate requires
  `llvm-readobj` or `python3`; both are present in CI images.
- **The PE test programs are `-nostdlib`**, import only `kernel32`, and use
  `mainCRTStartup`, so the test cannot fail because of CRT startup.
- **First-run risk in `build-wine`**: the Wine configure/Makefile is tuned for
  Android; the CI job has not been validated end-to-end here. Expect iteration
  on configure flags (`wine/build.sh` is unchanged and remains the source of
  truth for the real package).

---

## File map

```
ci/
├── Dockerfile.build              x86_64 cross-build image (NDK, both llvm-mingw)
├── Dockerfile.test               ARM64 Android test kit (adb driver + static gate)
├── build-test-programs.sh        builds load_dlls_{x64,i386,arm64}.exe
├── tests/                        stdlib-free Win32 PE smoke tests
│   ├── pe_test_common.h
│   ├── load_dlls_x64.c           d3d11/dxgi/libarm64ecfex, ARM64EC path
│   ├── load_dlls_i386.c          d3d11/dxgi from syswow64, WoW64 path
│   └── load_dlls_arm64.c         ntdll/kernel32/libFEXCore, native path
├── check-machine-types.sh        static gate (machine types + package layout)
├── lib/pe-machine.sh             PE machine reader (llvm-readobj → python3)
├── build-android-rootfs.sh       system image + Termux + wine → rootfs
├── run-android-rootfs-tests.sh   chroot/qemu driver for that rootfs
├── android/run-in-rootfs.sh      POSIX sh driver executed inside the rootfs
├── android-run-tests.sh          adb driver (emulator / device)
├── build-wine-test.sh            Linux glibc ARM64X Wine (test fixture)
├── run-wine-tests.sh             tests under a Linux ARM64X Wine
├── docker-build-test-image.sh    stages the Dockerfile.test context
├── docker-entrypoint.sh          auto|static|android|shell
└── README.md

.github/workflows/ci.yml          the job matrix above
```
