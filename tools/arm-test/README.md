# ARM64 Wine test harness

Runs a Windows program under the locally built **aarch64 Wine** on an x86-64
host. The container is `linux/arm64` and executes through qemu-user, so this
reproduces the phone's Wine + FEX + DXVK environment without touching a device.

It exists because the interesting failures (ARM64EC dispatch, FEX thunks,
DXVK's DXGI) do not reproduce on x86-64 Wine, and debugging on the phone is
slow: no ptrace, flaky Wi-Fi, and no way to instrument a rebuild quickly.

## Requirements

- docker with binfmt for arm64 (`docker run --rm --platform linux/arm64 debian uname -m` → `aarch64`)
- a configured Wine build tree for aarch64 (see *Build tree* below)
- an X server is **not** needed: the harness starts Xvfb inside the container

## Quick start

```bash
# run a program, headless, from a directory mounted as Z:\app
tools/arm-test/run.sh --app-dir ~/games/app --exe bin/app.exe --args "-flag"

# with DXVK and FEX (i.e. like the phone)
tools/arm-test/run.sh --app-dir ~/games/app --exe bin/app.exe \
  --dxvk artifacts/dxvk-arm64ec --fex artifacts/fex

# rebuild one DLL in the build tree, then rerun
tools/arm-test/build-wine.sh dlls/ntdll/aarch64-windows/ntdll.dll
```

Every run writes its full output to `artifacts/arm-test/run-<timestamp>.log`
and prints that path, so a failing run is reproducible by re-running the same
command.

## Layout

| file | purpose |
|---|---|
| `Dockerfile` | arm64 runtime image: deps, Xvfb, mesa/lavapipe |
| `lib.sh` | shared config, image build helper |
| `build-wine.sh` | incremental `make` in the configured tree |
| `run.sh` | boots/reuses the prefix, installs DLLs, runs the exe, logs |

## What the harness handles for you

These are the things that cost hours to discover, now encoded:

- **Prefix freshness.** A prefix is created once; Wine copies its DLLs into
  `system32` at that moment. After rebuilding Wine, the prefix holds *stale*
  core DLLs and crash signatures move around. The harness refreshes `ntdll`
  automatically, or recreates the whole prefix with `--recreate`.
- **wineboot timing.** `wineboot -i` returns before services finish writing
  `system32`; the harness waits on `wineserver -w` so later DLL copies are not
  overwritten.
- **NLS files.** The prefix symlinks NLS data from the source tree, so the
  source is mounted read-only at `/src`. Without it, wineserver dies with
  `failed to load l_intl.nls`.
- **DLL overrides.** With `--dxvk`, `d3d11,dxgi` are forced native (`=n`);
  Wine otherwise prefers its own builtins and DXVK silently does nothing.
- **App-directory DLLs win.** A game shipping its own `d3d11.dll` beats
  `system32`. Check the `+loaddll` line when in doubt:
  `WINEDEBUG_OVERRIDE=+loaddll tools/arm-test/run.sh ...`

## Build tree

`WINE_BUILD` must point at a **configured** Wine tree (default:
`~/.cache/wine-arm-test/build`, falling back to `~/wine-arm64-native` if that
exists). A from-scratch configure is deliberately not automated: the arm64ec
cross toolchain lives inside the tree (`/build/toolchain`), and the configure
flags are specific to that setup.

Known wart: a full `make` fails to link `dlls/winebus.sys`
(`is_sdl_ignored_device` undefined) *after* everything useful is built.
`build-wine.sh` reports the status instead of hiding it; build the target you
need rather than `all` when that matters.

## Environment overrides

| variable | default |
|---|---|
| `WINE_SRC` | `wine/workdir/wine` |
| `WINE_BUILD` | `~/.cache/wine-arm-test/build`, else `~/wine-arm64-native` |
| `PREFIX_DIR` | `~/.cache/wine-arm-test/prefix` |
| `LOG_DIR` | `artifacts/arm-test` |
| `WINEDEBUG_OVERRIDE` | `-all` (e.g. `+loaddll`, `+seh`) |
| `WINEDLLOVERRIDES_EXTRA` | unset |

## Debugging notes

- **No gdb.** qemu-user does not implement `ptrace`, so breakpoints and
  watchpoints are unavailable. Use logging and instrumentation instead
  (`WINEDEBUG`, `ERR`/`WARN` in the Wine source, DXVK's `DXVK_LOG_LEVEL`).
- **Logs explode.** `WINEDEBUG=+seh` has produced 13 GB / 152M lines here.
  Prefer `+loaddll`, or targeted `ERR()` instrumentation.
- **qemu swallows some crashes.** A guest SIGSEGV can terminate the emulator
  itself (`qemu: uncaught target signal 11`), which reads like a host failure.
  When a log stops abruptly with no Wine backtrace, suspect that first.
- **Version mixing is fragile.** Dropping a rebuilt `dxgi.dll` from one Wine
  revision into an older install fails at load time (`Importing dlls ...
  status c0000135`). Rebuild from the tree matching the target.
