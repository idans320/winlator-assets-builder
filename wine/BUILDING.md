# Building Wine ARM64EC for Winlator Cmod

This is a living document of everything learned during the Proton 11 build.
It assumes Wine 12 will have the same class of problems. Read before you compile.

---

## 1. Architecture overview

Winlator Cmod runs on Android/ARM64 hardware. It loads x86_64 Windows binaries
via ARM64EC (emulated x86_64 inside ARM64 processes). This is Wine's CHPE
(Compiled Hybrid PE) support, introduced in Proton 10.

### Build targets

| WIN_ARCH component | What it produces | Why |
|---|---|---|
| `aarch64` | `aarch64-windows/*.dll` (ARM64 PE), `aarch64-unix/*.so` (ARM64 ELF) | Native ARM64 Windows DLLs + Unix libraries |
| `i386` | `i386-windows/*.dll` (32-bit PE) | WoW64 32-bit support |
| `arm64ec` | **No separate directory** — enables ARM64EC thunks and CHPE metadata in aarch64 DLLs | ARM64EC redirection tables, `.hexpthk`/`.a64xrm` sections |

**Critical:** `arm64ec` in WIN_ARCH does NOT create `arm64ec-windows/` directories.
It modifies the aarch64 PE DLLs to include ARM64EC redirection metadata.
The packaging loops must NOT include `arm64ec` — only `aarch64` and `i386`.

Empty `arm64ec-windows/` or `arm64ec-unix/` directories in the .wcp cause
the Winlator container installer to fail silently.

### Source repo

```
repo:   https://github.com/GameNative/proton-wine.git
branch: proton_11.0
```

This is a fork of ValveSoftware/wine with Android-specific patches applied
in-tree. The `android/patches/` directory contains 45+ patches that must be
applied during configure. Some are idempotent (can be skipped), some are not.

---

## 2. Toolchain

### Cross-compiler

```
NDK: r29 (29.0.14206865) from Android SDK
CC:  aarch64-linux-android28-clang (LLVM 21.0.0-based)
CXX: aarch64-linux-android28-clang++

NDK r27 FAILS with "C compiler cannot create executables" because
-mcpu=oryon-1 is only supported in NDK r29+. Do not downgrade.
```

### PE cross-compiler (MinGW)

```
LLVM MinGW version: 20260519 (LLVM 20+)
Required for ARM64EC: LLVM 18.1.0 or newer
```

### Strip

```
DO NOT USE GNU strip on ARM64EC PE DLLs. It destroys CHPE metadata
and hybrid thunks. Our packaging step skips stripping entirely
("Skipping strip (debug symbols retained)").
```

---

## 3. Patches

### Remote patches (in-tree, applied during configure)

Location: `android/patches/*.patch` (45 patches)

These are applied during `configure_wine()` by iterating all `.patch` files.
Some patches already exist in the proton_11.0 branch source and will be
skipped (they don't apply cleanly). The `apply_patches` function has `|| true`
to tolerate this — skipped patches are non-fatal.

### Local patches (builder repo, applied during configure)

Location: `wine/patches/*.patch` (3 patches)

| Patch | What it does | Required? |
|---|---|---|
| `0003-disable-uffd-on-android.patch` | Wraps UFFD_WRITEWATCH in `#ifndef __ANDROID__` | YES — SIGSYS crash |
| `esync-server-include.patch` | Adds `#include "esync.h"` to dlls/ntdll/unix/server.c | YES — compile fails without |
| `pe-writecopy-gog.patch` | Changes header page protection to READ|WRITECOPY | Needed for GOG installers |

The UFFD patch was corrupt (index hash 0000000..0000000) in older versions.
Fixed by regenerating with `git diff`.

### Double-patch prevention

`server_protocol.def.patch` is applied separately during `generate_sources()`
(before autoreconf). After application it's renamed to `.applied` so the
subsequent `apply_patches()` loop skips it. This prevents the build from
failing on a double-apply.

---

## 4. The Preloader Nightmare

### What the preloader does

The wine-preloader is a tiny static ELF binary that wine exec's during
startup. It reserves fixed address ranges for Windows PE DLL mappings
and then loads the actual Windows binary.

On ARM64, it MUST load at virtual address **0x7d400000** (2GB mark).
If it loads at a lower address (e.g., 0x200000 default), it conflicts
with ntdll.so and other libraries, causing immediate SIGSEGV.

### LLD vs GNU ld: file offset mapping

**GNU ld:** Keeps file offsets separate from virtual addresses (VMAs).
A PHDR at VMA 0x200000 and .text at VMA 0x7d400000 can be stored at
file offsets 0x0 and 0x1000 — a compact 44KB ELF.

**LLD 21:** Maps file offsets to match VMAs by default.
PHDR at VMA 0x200000 = file offset 0x200000.
.text at VMA 0x7d400000 = file offset 0x7d400000.
The gap between them = 0x7d200000 = 2GB sparse file.

`-Wl,-Ttext=0x7d400000` works with LLD, but produces a 2GB sparse ELF.
GNU tar without `-S` expands sparse files, creating a 2GB .wcp.
`--image-base=0x7d400000` sets the VMA without moving file offsets,
producing a compact 12KB ELF.

### The build flow

```
1. configure generates WINEPRELOADER_LDFLAGS="-Wl,-Ttext=0x7d400000"
2. fixup_makefile() STRIPS this flag (prevents 2GB sparse file)
3. make all builds preloader without the flag → loads at ~0x200000 (WRONG)
4. build_aarch64_unix_libs() calls build_preloader()
5. build_preloader() deletes old preloader, compiles preloader.o,
   links with --image-base=0x7d400000 → correct, compact preloader
```

### What went wrong in Proton 11

The original `build_preloader()` had this early-return check:

```bash
[ -f "loader/wine-preloader" ] && [ "$(stat -c%s "loader/wine-preloader")" -le 1048576 ] && return
```

**Intent:** Skip if preloader already has the correct high-VMA layout
(which would be ~2GB on disk with LLD).

**Reality:** `make all` produces a 13KB preloader (because -Ttext was stripped),
which is ≤1MB, so the function ALWAYS skipped. The preloader was never rebuilt.

**Fix:** Removed the early-return. `build_preloader()` always runs.

---

## 5. rpath Poison

When cross-compiling with termux deps, LDFLAGS includes:

```
-Wl,-rpath=/data/data/com.termux/files/usr/lib
```

This gets baked into every .so as RUNPATH. On a Winlator Cmod device
the app is `com.winlator.cmod`, not `com.termux`. The RUNPATH points
to a non-existent directory, causing library resolution failures.

**Fix:** After `build_aarch64_unix_libs()`, strip all .so files:

```bash
find . -name "*.so" -type f | while read -r so; do
    patchelf --remove-rpath "$so" 2>/dev/null || true
done
```

---

## 6. packaging Pitfalls

### profile.json type

Must be `"type": "Wine"` not `"type": "Proton"`. Winlator Cmod's
container manager uses this to determine how to set up the wine
environment. "Proton" type caused container creation to fail.

### packaging arch loops

```bash
for pe_arch in aarch64 i386; do    # NOT arm64ec, NOT x86_64
```

Do NOT include `arm64ec` or `x86_64` in packaging loops. They create
empty directories that break the installer.

### Config.json

Included in the package with `WINEESYNC=1` and `WINEFSYNC=1`.
Required for esync/fsync to work.

---

## 7. Common Failure Modes and Their Fixes

### "Segmentation fault" on fresh prefix

Cause: Preloader loaded at wrong address.
Check: `readelf -l loader/wine-preloader | grep LOAD`
Should show VMA 0x7d400000. If not, build_preloader() didn't run.
Fix: Remove early-return in build_preloader().

### wine: failed to load ntdll.dll error c00000bb (STATUS_NOT_SUPPORTED)

Cause: ARM64EC mode sets machine=AMD64 but ntdll.dll has ARM64 machine type.
The wineserver rejects the mismatch.
Fix: This was a red herring — our build was fine. The actual issue was
missing ARM64EC support in the aarch64 DLLs because WIN_ARCH was wrong.

### wine: failed to load ntdll.dll error 4000000e (STATUS_IMAGE_MACHINE_TYPE_MISMATCH)

Same root cause as above. A WARNING-level status, but `load_ntdll()` treats
any non-zero status as fatal.

### Container not creating in Winlator Cmod

Possible causes:
1. profile.json has wrong `type` field (must be "Wine")
2. Empty arm64ec-* directories in .wcp
3. Missing Config.json
4. Wrong directory structure (e.g., x86_64-windows instead of i386-windows)

### "C compiler cannot create executables" during configure

Cause: Using NDK r27 with -mcpu=oryon-1.
Fix: Use NDK r29 (remove /tmp/android-ndk-r27 or point NDK env var elsewhere).

---

## 8. Quick Test Checklist

Before pushing to device:

```bash
# 1. Check preloader base
readelf -l workdir/wine/loader/wine-preloader | grep -c 0x7d400000
# Must return: 2

# 2. Check ntdll.dll machine type
python3 -c "import struct; f=open('workdir/wine/dlls/ntdll/aarch64-windows/ntdll.dll','rb'); f.seek(0x3c); pe=struct.unpack('<I',f.read(4))[0]; f.seek(pe+4); m=struct.unpack('<H',f.read(2))[0]; print(f'Machine: 0x{m:04x}')"
# Must print: Machine: 0xaa64 (ARM64)

# 3. Check ntdll.so has no rpath
readelf -d workdir/wine/dlls/ntdll/ntdll.so | grep RUNPATH
# Must return: (nothing)

# 4. Check .wcp has correct directories
tar -tJf wine-arm64-*.wcp | grep -E "lib/wine/(aarch64|i386)-" | sort -u
# Must contain: aarch64-unix, aarch64-windows, i386-unix, i386-windows
# Must NOT contain: arm64ec-*, x86_64-*

# 5. Check .wcp profile type
tar -xJf wine-arm64-*.wcp -O profile.json | grep '"type"'
# Must be: "type": "Wine"
```

On-device quick test (after installing via Winlator):

```bash
adb shell run-as com.winlator.cmod \
  /data/data/.../Proton/11-arm64ec-*/bin/wine --version
# Must print: wine-11.0 (no segfault)
```

---

## 9. Wine 12 Upgrade Notes

When upgrading to Wine 12:

1. **Source repo and branch** — GameNative/proton-wine will likely have a
   `proton_12.0` branch. Use that.

2. **Patches** — The 45 remote patches in `android/patches/` are tied to the
   source version. Some will fail to apply on a new version. Check each one
   with `git apply --check`. Update context hunks as needed.

3. **Local patches** — Same deal. Regenerate from git diffs if they fail.

4. **LLD behavior** — If LLD 22+ changes file offset mapping behavior,
   the `--image-base` workaround might become unnecessary (or need updating).

5. **ARM64EC** — This is still bleeding-edge. Check Wine release notes for
   CHPE/ARM64EC changes. The WIN_ARCH components might change.

6. **WINEPRELOADER_LDFLAGS** — Check config.status after configure. If Wine 12
   uses a different flag for the preloader base address, update `fixup_makefile`.

7. **MinGW version** — ARM64EC needs LLVM 18+. Check packages.yml for the
   `mingw_ver` field. Bump if needed.
