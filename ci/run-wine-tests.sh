#!/bin/bash
# ci/run-wine-tests.sh — run the built PE DLLs under a *Linux* ARM64X Wine.
#
# This is the glibc/ARM64 path: it uses a Wine built with
# --enable-archs=arm64ec,aarch64,i386 (see ci/build-wine-test.sh) and validates
# that the PE DLLs have the right machine types, that their imports resolve and
# that DllMain runs through Wine's ARM64X loader.
#
# It does NOT run the shipped wine-arm64-*.wcp: that package is
# aarch64-linux-android (bionic, interpreter /system/bin/linker64) and can only
# execute on Android. For the Android package use ci/build-android-rootfs.sh +
# ci/run-android-rootfs-tests.sh (chroot on an ARM64 runner), or
# ci/android-run-tests.sh (adb device/emulator).
#
# What it does:
#   1. verifies the Wine can run on this host and is ARM64X (plain ARM64 Wine
#      cannot map ARM64EC DLLs),
#   2. creates a WINEPREFIX (or unpacks a wcp prefixPack.txz),
#   3. installs FEX into prefix system32 (Wine's ntdll hardcodes
#      C:\windows\system32\libarm64ecfex.dll for x64 guests) and DXVK into
#      system32 (ARM64EC/ARM64X) + syswow64 (i386),
#   4. runs the load_dlls_*.exe smoke tests, one pass/fail line each.
#
# Usage:
#   ci/run-wine-tests.sh --fexcore PATH --dxvk PATH [options]
#
#   --fexcore PATH      fexcore .wcp/.zip or extracted directory (required)
#   --dxvk PATH         dxvk .wcp/.zip or extracted directory (required)
#   --tests DIR         directory with load_dlls_*.exe (default: ci/build/tests)
#   --wine PATH         wine binary (default: $WINE, then `command -v wine`)
#   --prefix DIR        WINEPREFIX parent (default: <work>/prefix)
#   --prefix-pack PATH  unpack a Wine prefixPack.txz (or a pre-extracted dir)
#                       instead of running wineboot
#   --work DIR          scratch directory (default: mktemp -d)
#   --only LIST         comma list of x64,i386,arm64 (default: x64,i386,arm64)
#   --require LIST      tests that must exist; missing ones fail instead of skip
#   --timeout SECONDS   per-test timeout (default: 180)
#   --skip-prechecks    skip the PE machine preflight (the test exes still print
#                       machine types at runtime)
#   --reuse-prefix      keep an existing prefix at --prefix
#   --debug             WINEDEBUG=+loaddll (verbose; upload the logs)
#   --keep              keep the work directory and print its path
#   --no-xvfb           do not start a private Xvfb
#
# Exit codes: 0 all selected tests passed, 1 test failure, 2 usage/preflight.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
if [ -f "$SCRIPT_DIR/lib/pe-machine.sh" ]; then
    # Flattened layout used on an Android device (ci/ pushed as a directory).
    CILIB="$SCRIPT_DIR/lib/pe-machine.sh"
    REPO_ROOT="$(dirname "$SCRIPT_DIR")"
    DEFAULT_TESTS="$SCRIPT_DIR/tests"
else
    REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
    CILIB="$REPO_ROOT/ci/lib/pe-machine.sh"
    DEFAULT_TESTS="$REPO_ROOT/ci/build/tests"
fi
# shellcheck source=lib/pe-machine.sh
. "$CILIB"

FEXCORE=""
DXVK=""
TESTS="$DEFAULT_TESTS"
WINE_BIN="${WINE:-}"
PREFIX=""
PREFIX_PACK=""
WORK=""
ONLY="x64,i386,arm64"
REQUIRE=""
TEST_TIMEOUT=180
REUSE_PREFIX=0
DEBUG=0
KEEP=0
USE_XVFB=1
SKIP_PRECHECKS=0

usage() {
    sed -n '2,56p' "$0" | sed 's/^# \{0,1\}//'
    exit 0
}

while [ $# -gt 0 ]; do
    case "$1" in
        --fexcore) FEXCORE="${2:?--fexcore needs a value}"; shift ;;
        --dxvk) DXVK="${2:?--dxvk needs a value}"; shift ;;
        --tests) TESTS="${2:?--tests needs a value}"; shift ;;
        --wine) WINE_BIN="${2:?--wine needs a value}"; shift ;;
        --prefix) PREFIX="${2:?--prefix needs a value}"; shift ;;
        --prefix-pack) PREFIX_PACK="${2:?--prefix-pack needs a value}"; shift ;;
        --work) WORK="${2:?--work needs a value}"; shift ;;
        --only) ONLY="${2:?--only needs a value}"; shift ;;
        --require) REQUIRE="${2:?--require needs a value}"; shift ;;
        --timeout) TEST_TIMEOUT="${2:?--timeout needs a value}"; shift ;;
        --skip-prechecks) SKIP_PRECHECKS=1 ;;
        --reuse-prefix) REUSE_PREFIX=1 ;;
        --debug) DEBUG=1 ;;
        --keep) KEEP=1 ;;
        --no-xvfb) USE_XVFB=0 ;;
        -h | --help) usage ;;
        *)
            echo "unknown argument: $1" >&2
            exit 2
            ;;
    esac
    shift
done

die() {
    echo "error: $*" >&2
    exit 1
}

note() { printf '  %s\n' "$*"; }
pass_row() { printf '  %-4s %-28s %s\n' "PASS" "$1" "${2:-}"; }
fail_row() { printf '  %-4s %-28s %s\n' "FAIL" "$1" "${2:-}"; }
skip_row() { printf '  %-4s %-28s %s\n' "SKIP" "$1" "${2:-}"; }

wants() {
    case ",$ONLY," in
        *",$1,"*) return 0 ;;
        *) return 1 ;;
    esac
}

requires() {
    case ",$REQUIRE," in
        *",$1,"*) return 0 ;;
        *) return 1 ;;
    esac
}

[ -n "$FEXCORE" ] || die "--fexcore is required"
[ -n "$DXVK" ] || die "--dxvk is required"

# --- wine discovery --------------------------------------------------------

if [ -z "$WINE_BIN" ]; then
    WINE_BIN="$(command -v wine || true)"
fi
[ -n "$WINE_BIN" ] || die "no wine found; pass --wine /path/to/wine"
[ -x "$WINE_BIN" ] || die "wine is not executable: $WINE_BIN"

# Remember the launcher directory *before* resolving symlinks: in the .wcp,
# bin/wine -> ../lib/wine/aarch64-unix/wine and bin/wineboot -> wine, so the
# install root is bin/.. and NOT dirname(readlink -f wine).
WINE_LAUNCHER_DIR="$(cd "$(dirname "$WINE_BIN")" && pwd)"
WINE_BIN="$(readlink -f "$WINE_BIN")"
WINE_INSTALL_ROOT="$(cd "$WINE_LAUNCHER_DIR/.." && pwd)"
WINE_NTDLL="$WINE_INSTALL_ROOT/lib/wine/aarch64-windows/ntdll.dll"
# wineserver/wineboot live next to the loader in both the package and a
# normal install; Termux needs this explicitly.
PATH="$WINE_LAUNCHER_DIR:$PATH"
export PATH

# A glibc runner cannot execute the Android (bionic) wine from the package.
# Refuse with the reason instead of letting wineboot fail cryptically.
if command -v readelf > /dev/null 2>&1; then
    interp="$(readelf -l "$WINE_BIN" 2> /dev/null |
        sed -n 's/.*Requesting program interpreter: \(.*\)]/\1/p' | head -1)"
    if [ "${interp:-}" = "/system/bin/linker64" ]; then
        cat >&2 << EOF
error: $WINE_BIN is an Android/bionic binary (interpreter /system/bin/linker64)
       and cannot run on this host.

       The wine-arm64-*.wcp package is built with
       --host=aarch64-linux-android28 and only runs on Android. Use the Android
       rootfs driver instead:

         ci/build-android-rootfs.sh --root ci-root ...
         ci/run-android-rootfs-tests.sh --root ci-root

       To validate the PE DLLs on a glibc ARM64 runner, build a Linux ARM64X
       Wine instead (this validates machine types/imports/DllMain, not the
       Android package):

         ci/build-wine-test.sh --prefix /opt/wine-test
         ci/run-wine-tests.sh --wine /opt/wine-test/bin/wine ...
EOF
        exit 2
    fi
fi

WINE_VERSION="$("$WINE_BIN" --version 2> /dev/null || true)"
[ -n "$WINE_VERSION" ] || die "cannot execute $WINE_BIN --version"

# --- scratch dir / prefix --------------------------------------------------

if [ -z "$WORK" ]; then
    WORK="$(mktemp -d)"
fi
mkdir -p "$WORK"
[ -n "$PREFIX" ] || PREFIX="$WORK/prefix"
LOGS="$WORK/logs"
mkdir -p "$LOGS"

XVFB_PID=""

cleanup() {
    if [ -n "$XVFB_PID" ]; then
        kill "$XVFB_PID" 2> /dev/null || true
    fi
    WINEPREFIX="${WINEPREFIX:-$PREFIX}" wineserver -k 2> /dev/null || true
    if [ "$KEEP" -eq 1 ]; then
        echo "kept work directory: $WORK"
    else
        rm -rf "$WORK"
    fi
}
trap cleanup EXIT

# --- Xvfb ------------------------------------------------------------------

if [ "$USE_XVFB" -eq 1 ] && [ -z "${DISPLAY:-}" ] && command -v Xvfb > /dev/null 2>&1; then
    for display in 99 98 97 96; do
        if ! [ -e "/tmp/.X11-unix/X$display" ]; then
            Xvfb ":$display" -screen 0 1024x768x24 > "$LOGS/xvfb.log" 2>&1 &
            XVFB_PID=$!
            export DISPLAY=":$display"
            sleep 1
            break
        fi
    done
fi

# --- artifacts -------------------------------------------------------------

unpack() {
    local src="$1"
    local name="$2"
    local dest="$WORK/components/$name"

    if [ -d "$src" ]; then
        printf '%s\n' "$src"
        return 0
    fi
    [ -f "$src" ] || return 1

    mkdir -p "$dest"
    case "$src" in
        *.zip)
            command -v unzip > /dev/null 2>&1 || return 1
            unzip -q "$src" -d "$dest" || return 1
            ;;
        *)
            tar -xJf "$src" -C "$dest" || return 1
            ;;
    esac
    printf '%s\n' "$dest"
}

FEX_DIR="$(unpack "$FEXCORE" fexcore)" || die "cannot unpack fexcore: $FEXCORE"
DXVK_DIR="$(unpack "$DXVK" dxvk)" || die "cannot unpack dxvk: $DXVK"

[ -d "$FEX_DIR/system32" ] || die "fexcore package has no system32/: $FEX_DIR"
[ -d "$DXVK_DIR/system32" ] || die "dxvk package has no system32/: $DXVK_DIR"
[ -d "$DXVK_DIR/syswow64" ] || die "dxvk package has no syswow64/: $DXVK_DIR"

# --- env -------------------------------------------------------------------

export WINEPREFIX="$PREFIX"
export WINEARCH=win64
# Prefer the native DLLs we installed over Wine's builtins.
export WINEDLLOVERRIDES="d3d8,d3d9,d3d10core,d3d11,dxgi=n,b;winemenubuilder.exe=d"
if [ "$DEBUG" -eq 1 ]; then
    export WINEDEBUG="+loaddll,+module"
else
    export WINEDEBUG="-all"
fi
unset LD_PRELOAD

printf '== wine runtime test ==\n'
printf 'mode:       linux (glibc wine)\n'
printf 'wine:       %s (%s)\n' "$WINE_BIN" "$WINE_VERSION"
printf 'fexcore:    %s\n' "$FEXCORE"
printf 'dxvk:       %s\n' "$DXVK"
printf 'tests:      %s\n' "$TESTS"
printf 'prefix:     %s\n' "$PREFIX"
printf 'display:    %s\n' "${DISPLAY:-<none>}"

# --- prefix ----------------------------------------------------------------

printf '\n== prefix ==\n'
if [ "$REUSE_PREFIX" -eq 1 ] && [ -f "$PREFIX/.wine/system.reg" ]; then
    WINEPREFIX="$PREFIX/.wine"
    export WINEPREFIX
    note "reusing $WINEPREFIX"
elif [ "$REUSE_PREFIX" -eq 1 ] && [ -f "$PREFIX/system.reg" ]; then
    note "reusing $PREFIX"
elif [ -n "$PREFIX_PACK" ]; then
    # Winlator does not run wineboot on device: it unpacks the prefix pack that
    # ships inside the .wcp. Mirror that so the device path matches production.
    # The pack may be the .txz itself or a directory already extracted on the
    # host (Android devices do not always have xz).
    if [ -d "$PREFIX_PACK" ]; then
        rm -rf "$PREFIX"
        mkdir -p "$PREFIX"
        note "using extracted prefix pack $(basename "$PREFIX_PACK")"
        cp -a "$PREFIX_PACK/." "$PREFIX/" || die "cannot copy $PREFIX_PACK"
    else
        [ -f "$PREFIX_PACK" ] || die "prefix pack not found: $PREFIX_PACK"
        rm -rf "$PREFIX"
        mkdir -p "$PREFIX"
        note "unpacking prefix pack $(basename "$PREFIX_PACK")"
        tar -xJf "$PREFIX_PACK" -C "$PREFIX" || die "cannot unpack $PREFIX_PACK"
    fi
    if [ -d "$PREFIX/.wine" ]; then
        WINEPREFIX="$PREFIX/.wine"
    else
        WINEPREFIX="$PREFIX"
    fi
    export WINEPREFIX
    # dosdevices/c: in the pack points at an Android path; retarget it at the
    # drive_c we just extracted so the prefix is usable on any host.
    if [ -d "$WINEPREFIX/dosdevices" ]; then
        ln -sfn ../drive_c "$WINEPREFIX/dosdevices/c:" 2> /dev/null || true
        ln -sfn / "$WINEPREFIX/dosdevices/z:" 2> /dev/null || true
    fi
    note "prefix: $WINEPREFIX"
    # wineboot is run once, best effort: the packed prefix is already usable, and
    # wineboot on a headless device can sit waiting for a display.
    note "wineboot -u (best effort)"
    timeout -k 10 90 "$WINE_BIN" wineboot -u > "$LOGS/wineboot-pack.log" 2>&1 ||
        note "wineboot returned $? (continuing with the unpacked prefix)"
else
    rm -rf "$PREFIX"
    mkdir -p "$PREFIX"
    note "wineboot -i (this can take a minute)"
    timeout -k 10 180 "$WINE_BIN" wineboot -i > "$LOGS/wineboot-init.log" 2>&1
    boot_rc=$?
    if [ ! -f "$PREFIX/drive_c/windows/system32/ntdll.dll" ]; then
        note "wineboot -i rc=$boot_rc did not complete the prefix; trying wineboot -u"
        timeout -k 10 420 "$WINE_BIN" wineboot -u > "$LOGS/wineboot-update.log" 2>&1
    fi
    WINEPREFIX="$PREFIX" wineserver -k 2> /dev/null || true
fi

PREFIX_REG="$WINEPREFIX/system.reg"
[ -f "$PREFIX_REG" ] || {
    echo "---- wineboot-init.log ----" >&2
    tail -30 "$LOGS/wineboot-init.log" >&2 2> /dev/null
    echo "---- wineboot-update.log ----" >&2
    tail -30 "$LOGS/wineboot-update.log" >&2 2> /dev/null
    echo "---- wineboot-pack.log ----" >&2
    tail -30 "$LOGS/wineboot-pack.log" >&2 2> /dev/null
    die "wine prefix was not created: $PREFIX_REG missing"
}

if [ "$SKIP_PRECHECKS" -eq 1 ]; then
    note "prechecks skipped (--skip-prechecks); machine types are asserted by the test exes"
else
    # The install tree is authoritative: the packaged prefix does not carry
    # ntdll.dll (Wine resolves builtins from lib/wine), so check the loader the
    # prefix will actually use.
    NTDLL_PATH="$WINE_NTDLL"
    [ -f "$NTDLL_PATH" ] ||
        NTDLL_PATH="$WINEPREFIX/drive_c/windows/system32/ntdll.dll"
    [ -f "$NTDLL_PATH" ] ||
        die "cannot locate ntdll.dll under $WINE_INSTALL_ROOT/lib/wine or the prefix"

    NTDLL_MACHINE="$(pe_machine "$NTDLL_PATH")" ||
        die "cannot read machine type of $NTDLL_PATH (need llvm-readobj or python3)"
    note "wine loader ntdll.dll: $NTDLL_MACHINE ($(pe_machine_name "$NTDLL_MACHINE"))"
    if [ "${NTDLL_MACHINE^^}" != "0XA64E" ]; then
        cat >&2 << EOF
error: Wine is not an ARM64X build (loader ntdll is $NTDLL_MACHINE).

       An ARM64EC DLL can only be mapped by an ARM64EC-capable loader, so this
       runtime cannot validate the packages. Rebuild Wine with
       --enable-archs=arm64ec,aarch64,i386 (ci/build-wine-test.sh for Linux), or
       use the wine-arm64-*.wcp on an Android device.
EOF
        exit 2
    fi

    SYSWOW_NTDLL="$WINEPREFIX/drive_c/windows/syswow64/ntdll.dll"
    [ -f "$SYSWOW_NTDLL" ] ||
        SYSWOW_NTDLL="$WINE_INSTALL_ROOT/lib/wine/i386-windows/ntdll.dll"
    if [ -f "$SYSWOW_NTDLL" ]; then
        SYSWOW_MACHINE="$(pe_machine "$SYSWOW_NTDLL")"
        note "wine 32-bit ntdll.dll: $SYSWOW_MACHINE ($(pe_machine_name "$SYSWOW_MACHINE"))"
    else
        note "no 32-bit ntdll found (i386 tests may not run)"
    fi
fi

# --- install components ----------------------------------------------------

printf '\n== install ==\n'
FAILED=0
RAN=0
RESULTS=""
SYSTEM32="$WINEPREFIX/drive_c/windows/system32"
SYSWOW64="$WINEPREFIX/drive_c/windows/syswow64"
mkdir -p "$SYSTEM32" "$SYSWOW64"

installed=0
for dll in "$FEX_DIR"/system32/*.dll; do
    [ -f "$dll" ] || continue
    cp -f "$dll" "$SYSTEM32/"
    installed=$((installed + 1))
done
note "fexcore: $installed DLL(s) -> system32"
[ "$installed" -gt 0 ] || die "fexcore package contains no DLLs"

installed=0
for dll in "$DXVK_DIR"/system32/*.dll; do
    [ -f "$dll" ] || continue
    cp -f "$dll" "$SYSTEM32/"
    installed=$((installed + 1))
done
note "dxvk: $installed DLL(s) -> system32"

installed=0
for dll in "$DXVK_DIR"/syswow64/*.dll; do
    [ -f "$dll" ] || continue
    cp -f "$dll" "$SYSWOW64/"
    installed=$((installed + 1))
done
note "dxvk: $installed DLL(s) -> syswow64"

# Assert what actually landed in the prefix: a build.sh that copies the wrong
# directory would otherwise only show up as a confusing LoadLibrary failure.
check_prefix_machine() {
    local file="$1" label="$2"
    shift 2
    local machine allowed

    if [ "$SKIP_PRECHECKS" -eq 1 ]; then
        note "$label: (not checked)"
        return 0
    fi
    if ! machine="$(pe_machine "$file")"; then
        fail_row "$label" "unreadable"
        FAILED=$((FAILED + 1))
        return 1
    fi
    for allowed in "$@"; do
        if [ "${machine^^}" = "${allowed^^}" ]; then
            note "$label: $machine ($(pe_machine_name "$machine"))"
            return 0
        fi
    done
    fail_row "$label" "$machine ($(pe_machine_name "$machine"))"
    FAILED=$((FAILED + 1))
    return 1
}

check_prefix_machine "$SYSTEM32/libarm64ecfex.dll" "system32/libarm64ecfex.dll" 0xA641
check_prefix_machine "$SYSTEM32/d3d11.dll" "system32/d3d11.dll" 0xA641 0xA64E
check_prefix_machine "$SYSWOW64/d3d11.dll" "syswow64/d3d11.dll" 0x14C

TEST_DIR="$WINEPREFIX/drive_c/tests"
mkdir -p "$TEST_DIR"

# --- run tests -------------------------------------------------------------

printf '\n== tests ==\n'
run_one() {
    local key="$1" exe="$2" desc="$3"
    local src="$TESTS/$exe"
    local log="$LOGS/${key}.log"

    if [ ! -f "$src" ]; then
        if requires "$key"; then
            fail_row "$key" "missing $src"
            FAILED=$((FAILED + 1))
            RESULTS="$RESULTS$key: FAIL (missing binary)\n"
        else
            skip_row "$key" "missing $src"
            RESULTS="$RESULTS$key: SKIP (missing binary)\n"
        fi
        return
    fi

    cp -f "$src" "$TEST_DIR/"
    RAN=$((RAN + 1))
    timeout -k 10 "$TEST_TIMEOUT" "$WINE_BIN" "C:\\tests\\$exe" > "$log" 2>&1
    local rc=$?

    if [ "$rc" -eq 0 ]; then
        pass_row "$key" "$desc (exit 0)"
        RESULTS="$RESULTS$key: PASS\n"
    else
        fail_row "$key" "$desc (exit $rc)"
        FAILED=$((FAILED + 1))
        RESULTS="$RESULTS$key: FAIL (exit $rc)\n"
    fi

    if [ -s "$log" ]; then
        sed 's/^/    /' "$log" | head -40
    fi
    echo "    log: $log"
}

if wants x64; then
    run_one x64 load_dlls_x64.exe "x86-64 guest -> ARM64EC/FEX"
fi
if wants i386; then
    run_one i386 load_dlls_i386.exe "i386 guest -> WoW64/FEX"
fi
if wants arm64; then
    run_one arm64 load_dlls_arm64.exe "native ARM64"
fi

printf '\n== summary ==\n'
printf 'wine: %s\n' "$WINE_VERSION"
printf -v results_formatted '%b' "$RESULTS"
printf '%s' "$results_formatted"
if [ "$RAN" -eq 0 ]; then
    echo "no tests were selected (--only $ONLY)"
    exit 1
fi

if [ "$FAILED" -ne 0 ]; then
    cat << 'EOF'

Failures mean a DLL does not load under this Wine. A LoadLibrary error
126/193 is a machine-type or import problem, "could not load kernel32.dll"
is a broken prefix/Wine install, and a timeout is usually a hang in a DllMain
(WINEDEBUG=+loaddll shows how far it got).
EOF
    exit 1
fi

echo "All selected Wine tests passed."
exit 0
