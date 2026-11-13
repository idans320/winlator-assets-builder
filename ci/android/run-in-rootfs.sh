#!/system/bin/sh
# ci/android/run-in-rootfs.sh — runs INSIDE an Android runtime rootfs.
#
# Used by:
#   ci/run-android-rootfs-tests.sh   chroot on an ARM64 runner (or qemu-aarch64)
#   ci/android-run-tests.sh          adb push to an emulator / self-hosted device
#
# This script is deliberately POSIX sh: the Android system image provides
# /system/bin/sh (mksh), while the Termux prefix bootstrapped by wine/build.sh
# contains libraries only — no bash. The Wine binaries are bionic and are
# executed directly (their PT_INTERP is /system/bin/linker64).
#
# Layout (all overridable by env, defaults are what ci/build-android-rootfs.sh
# assembles inside the chroot):
#   /opt/wine                 extracted wine-arm64-*.wcp
#                             (bin/wine, bin/wineboot, lib/wine/..., prefixPack.txz)
#   /opt/components/fexcore   extracted fexcore package (system32/*.dll)
#   /opt/components/dxvk      extracted dxvk package (system32/, syswow64/)
#   /opt/tests                load_dlls_{x64,i386,arm64}.exe
#   /opt/prefix               pre-extracted prefixPack.txz (contains .wine/)
#
# Hard requirements encoded here:
#   - the Termux prefix lives at exactly /data/data/com.termux/files/usr because
#     Wine/Termux binaries have that RUNPATH hardcoded (TERMUX_USR overrides the
#     path for non-standard layouts, but it must match the RUNPATH).
#   - bin/wineboot must exist in the package (Winlator's prefix creation uses it).
#   - FEX must be in the prefix system32 (Wine's ntdll loads
#     C:\windows\system32\libarm64ecfex.dll for x64 guests).
#   - DXVK system32 must be ARM64EC/ARM64X and syswow64 i386.
#
# Exit code: 0 all tests passed, 1 failure, 2 preflight failure.

set -u

WINE_ROOT="${WINE_ROOT:-/opt/wine}"
COMPONENTS="${COMPONENTS:-/opt/components}"
TESTS="${TESTS:-/opt/tests}"
PREFIX_DIR="${PREFIX_DIR:-/opt/prefix}"
WORK="${WORK:-/opt/work}"
TERMUX_USR="${TERMUX_USR:-/data/data/com.termux/files/usr}"
TEST_TIMEOUT="${TEST_TIMEOUT:-180}"

LOGS="$WORK/logs"

export ANDROID_ROOT="${ANDROID_ROOT:-/system}"
export ANDROID_DATA="${ANDROID_DATA:-/data}"
export PATH="/system/bin:$TERMUX_USR/bin:${PATH:-}"
export LD_LIBRARY_PATH="$WINE_ROOT/lib/wine/aarch64-unix:$TERMUX_USR/lib:/system/lib64${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
export WINEDLLOVERRIDES="d3d8,d3d9,d3d10core,d3d11,dxgi=n,b;winemenubuilder.exe=d"
export WINEDEBUG="${WINEDEBUG:--all}"
unset DISPLAY

mkdir -p "$LOGS" || exit 2

say() { printf '%s\n' "$*"; }
note() { printf '  %s\n' "$*"; }
pass_row() { printf '  %-4s %-28s %s\n' "PASS" "$1" "${2:-}"; }
fail_row() { printf '  %-4s %-28s %s\n' "FAIL" "$1" "${2:-}"; }
skip_row() { printf '  %-4s %-28s %s\n' "SKIP" "$1" "${2:-}"; }

die() {
    echo "error: $*" >&2
    exit 2
}

# --- preflight -------------------------------------------------------------

say "== android runtime =="
say "wine root:  $WINE_ROOT"
say "termux:     $TERMUX_USR"
say "prefix dir: $PREFIX_DIR"

WINE_BIN="$WINE_ROOT/lib/wine/aarch64-unix/wine"
[ -x "$WINE_BIN" ] || WINE_BIN="$WINE_ROOT/bin/wine"
[ -x "$WINE_BIN" ] || die "no wine binary under $WINE_ROOT"
say "wine:       $("$WINE_BIN" --version 2> /dev/null || echo 'FAILED --version')"

[ -e "$WINE_ROOT/bin/wineboot" ] ||
    die "package is missing bin/wineboot; prefix creation on device fails without it"
[ -f "$WINE_ROOT/profile.json" ] || note "no profile.json in $WINE_ROOT"

[ -d "$COMPONENTS/fexcore/system32" ] || die "no fexcore system32 in $COMPONENTS"
[ -d "$COMPONENTS/dxvk/system32" ] || die "no dxvk system32 in $COMPONENTS"
[ -d "$COMPONENTS/dxvk/syswow64" ] || die "no dxvk syswow64 in $COMPONENTS"
[ -x "$ANDROID_ROOT/bin/linker64" ] || die "$ANDROID_ROOT/bin/linker64 missing (rootfs is not an Android system)"

# --- prefix ----------------------------------------------------------------

say ""
say "== prefix =="
if [ -f "$PREFIX_DIR/.wine/system.reg" ]; then
    WINEPREFIX="$PREFIX_DIR/.wine"
elif [ -f "$PREFIX_DIR/system.reg" ]; then
    WINEPREFIX="$PREFIX_DIR"
else
    die "no usable prefix in $PREFIX_DIR (extract prefixPack.txz on the host)"
fi
export WINEPREFIX
note "WINEPREFIX=$WINEPREFIX"
# dosdevices/c: inside the pack points at the device's storage; retarget it at
# the drive_c we have so path resolution works in this rootfs.
if [ -d "$WINEPREFIX/dosdevices" ]; then
    ln -sfn ../drive_c "$WINEPREFIX/dosdevices/c:" 2> /dev/null
    ln -sfn / "$WINEPREFIX/dosdevices/z:" 2> /dev/null
fi

# bin/wineboot is run once, best effort: a packed prefix is already usable and
# wineboot on a headless device can sit waiting for a display.
if [ -x "$WINE_ROOT/bin/wineboot" ]; then
    note "wineboot -u (best effort)"
    timeout 90 "$WINE_ROOT/bin/wineboot" -u > "$LOGS/wineboot.log" 2>&1 ||
        note "wineboot returned $? (continuing with the packed prefix)"
fi

# --- install components ----------------------------------------------------

say ""
say "== install =="
SYSTEM32="$WINEPREFIX/drive_c/windows/system32"
SYSWOW64="$WINEPREFIX/drive_c/windows/syswow64"
mkdir -p "$SYSTEM32" "$SYSWOW64" "$WINEPREFIX/drive_c/tests"

count=0
for dll in "$COMPONENTS/fexcore/system32"/*.dll; do
    [ -f "$dll" ] || continue
    cp -f "$dll" "$SYSTEM32/" || die "cannot copy $dll"
    count=$((count + 1))
done
note "fexcore: $count DLL(s) -> system32"
[ "$count" -gt 0 ] || die "fexcore package has no DLLs"

count=0
for dll in "$COMPONENTS/dxvk/system32"/*.dll; do
    [ -f "$dll" ] || continue
    cp -f "$dll" "$SYSTEM32/" || die "cannot copy $dll"
    count=$((count + 1))
done
note "dxvk: $count DLL(s) -> system32"

count=0
for dll in "$COMPONENTS/dxvk/syswow64"/*.dll; do
    [ -f "$dll" ] || continue
    cp -f "$dll" "$SYSWOW64/" || die "cannot copy $dll"
    count=$((count + 1))
done
note "dxvk: $count DLL(s) -> syswow64"

[ -f "$SYSTEM32/libarm64ecfex.dll" ] ||
    die "libarm64ecfex.dll was not installed into system32"

# --- tests -----------------------------------------------------------------

say ""
say "== tests =="
FAILED=0
RAN=0
SUMMARY=""

run_one() {
    key="$1"
    exe="$2"
    desc="$3"
    src="$TESTS/$exe"
    log="$LOGS/$key.log"

    if [ ! -f "$src" ]; then
        skip_row "$key" "missing $exe"
        SUMMARY="$SUMMARY$key: SKIP (missing binary)
"
        return
    fi

    cp -f "$src" "$WINEPREFIX/drive_c/tests/" || die "cannot copy $src"
    RAN=$((RAN + 1))
    timeout "$TEST_TIMEOUT" "$WINE_BIN" "C:\\tests\\$exe" > "$log" 2>&1
    rc=$?

    if [ "$rc" -eq 0 ]; then
        pass_row "$key" "$desc (exit 0)"
        SUMMARY="$SUMMARY$key: PASS
"
    else
        fail_row "$key" "$desc (exit $rc)"
        FAILED=$((FAILED + 1))
        SUMMARY="$SUMMARY$key: FAIL (exit $rc)
"
    fi

    if [ -s "$log" ]; then
        sed 's/^/    /' "$log" 2> /dev/null | head -40 || head -40 "$log"
    fi
    echo "    log: $log"
}

run_one x64 load_dlls_x64.exe "x86-64 guest -> ARM64EC/FEX"
run_one i386 load_dlls_i386.exe "i386 guest -> WoW64/FEX"
run_one arm64 load_dlls_arm64.exe "native ARM64"

say ""
say "== summary =="
printf '%s' "$SUMMARY"

if [ "$RAN" -eq 0 ]; then
    echo "no tests were found in $TESTS"
    exit 1
fi

if [ "$FAILED" -ne 0 ]; then
    cat << 'EOF'

A failure means a DLL did not load under the Android Wine. Error 126/193 is a
machine-type or import problem, "could not load kernel32.dll" is a broken
prefix/Wine install, and a timeout points at a DllMain hang (run with
WINEDEBUG=+loaddll to see how far it got).
EOF
    exit 1
fi

echo "All Android Wine tests passed."
exit 0
