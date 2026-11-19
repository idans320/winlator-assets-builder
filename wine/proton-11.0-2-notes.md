# Wine: proton_11.0 → proton_11.0-2

`packages.yml` pins `proton-11.0-2-20261115` (head `a23e554`). The 11.0 branch
stopped moving in May 2026; -2 carries ~310 commits of ARM64EC/Android work
(native ARM64 WoW64, GetNativeSystemInfo reporting AMD64 on ARM64, userspace
ntsync wait-any, Sleep(0) throttling, winex11 XComposite/XFixes, atiadlxx
stubs, reverts of the old FEX TSO/unaligned-atomic hacks).

## What the branch changed under us

| 11.0 | 11.0-2 | Handled by |
|---|---|---|
| esync (patches in `android/patches/*.patch`) | removed, replaced by userspace ntsync | dropped `wine/patches/esync-server-include.patch` |
| flat `android/patches/*.patch` | `android/patches/{tools,common,arm64ec,x86_64}/` + curated order | `apply_android_patches()` parses the list out of `build-scripts/build-step-arm64ec.sh` (comment-aware: one entry is commented out upstream) |
| ntsync optional | `ntsync_sem_*` linked into ntdll/wineserver | `build_ntsync_android()` builds `joshuatam/ntsync-android` (Rust, pinned commit) into `$DEPS/lib` |
| tools list `winebuild+winegcc+...` | also needs `wineg++` (lsteamclient) | `make __tooldeps__` |
| `--without-xfixes` | winex11 warps the cursor via XFixes | `--with-xfixes` (Xinerama/XComposite still off — libs absent from deps) |

## Build requirements

- Rust with the `aarch64-linux-android` target. Added to `devbox.json` (`rustup`)
  and to `ci/Dockerfile.build` (pinned 1.100.0, `CARGO_HOME` writable for the
  CI uid).
- Termux deps must include `libXfixes.so` (they already did).

## Guards added (each caught a real failure)

- `make depend` failure is fatal — it used to be ignored, which produced a
  26 MB package with no PE DLLs.
- Packaging refuses a payload with ≤100 PE files (checked *after* assembly —
  the first version of this check looked in the wrong directory and rejected a
  good build).
- The remote-patch step no longer silently applies zero patches when the
  directory layout changes.
- `apply_android_patches` resets the tree before patching, so a re-run cannot
  stack patches on a half-patched source.

## Known gaps

- **No esync.** -2 routes fast sync through ntsync (kernel driver, or the
  userspace library on Android); fsync remains and `WINEFSYNC=1` is set in the
  package `Config.json`.
- No XComposite/Xinerama in the package (the termux deps do not ship them).
- Not yet validated on device.
