#!/bin/bash
# ci/android-run-tests.sh — drive the Android Wine tests over adb.
#
# The wine-arm64 package is aarch64-linux-android (bionic): it needs the Android
# linker and the Termux libs at exactly /data/data/com.termux/files/usr. This
# script reproduces the rootfs layout on a device/emulator:
#
#   /data/data/com.termux/files/usr   Termux prefix (pushed from the host unless
#                                     the device already has Termux)
#   /data/local/tmp/winlator-ci/      wine payload, components, tests, prefix, driver
#
# and then runs /system/bin/sh /data/local/tmp/winlator-ci/ci/run-in-rootfs.sh.
# /system already exists on the device, so only the Termux prefix and the Wine
# payload are pushed.
#
# Use ci/build-android-rootfs.sh + ci/run-android-rootfs-tests.sh instead if you
# have an ARM64 Linux runner: no emulator/device needed.
#
# Usage (host + adb):
#   ci/android-run-tests.sh --serial emulator-5554 \
#       --wine wine-arm64-*.wcp --fexcore fexcore-*.wcp --dxvk dxvk-*.wcp \
#       --termux-rootfs wine/workdir/termux-rootfs [--tests DIR]
#
# Usage (device already in Termux, no adb):
#   ci/android-run-tests.sh --local --wine ... --fexcore ... --dxvk ...
#
# Options:
#   --termux-rootfs DIR  Termux tree (data/data/com.termux/files/usr or usr/)
#   --no-rootfs          device already provides /data/data/com.termux/files/usr
#   --log-dir DIR        where to pull the device logs (default ./ci-logs/android)
#
# Exit codes: 0 all tests passed, 1 failure, 2 usage/preflight.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REMOTE_DIR="/data/local/tmp/winlator-ci"
TERMUX_USR="/data/data/com.termux/files/usr"

SERIAL=""
LOCAL=0
WINE_ARG=""
FEXCORE=""
DXVK=""
TESTS="$SCRIPT_DIR/build/tests"
TERMUX_ROOTFS=""
NO_ROOTFS=0
LOG_DIR="$PWD/ci-logs/android"

usage() {
    sed -n '2,40p' "$0" | sed 's/^# \{0,1\}//'
    exit 0
}

while [ $# -gt 0 ]; do
    case "$1" in
        --serial) SERIAL="${2:?}"; shift ;;
        --local) LOCAL=1 ;;
        --wine) WINE_ARG="${2:?}"; shift ;;
        --fexcore) FEXCORE="${2:?}"; shift ;;
        --dxvk) DXVK="${2:?}"; shift ;;
        --tests) TESTS="${2:?}"; shift ;;
        --termux-rootfs) TERMUX_ROOTFS="${2:?}"; shift ;;
        --no-rootfs) NO_ROOTFS=1 ;;
        --log-dir) LOG_DIR="${2:?}"; shift ;;
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
    exit 2
}
note() { printf '  %s\n' "$*"; }
say() { printf '\n== %s ==\n' "$*"; }

[ -n "$WINE_ARG" ] || die "--wine is required"
[ -n "$FEXCORE" ] || die "--fexcore is required"
[ -n "$DXVK" ] || die "--dxvk is required"

unpack() {
    local src="$1"
    local dest="$2"
    rm -rf "$dest"
    mkdir -p "$dest"
    if [ -d "$src" ]; then
        cp -a "$src/." "$dest/"
        return 0
    fi
    case "$src" in
        *.zip)
            command -v unzip > /dev/null 2>&1 || return 1
            unzip -q "$src" -d "$dest"
            ;;
        *)
            tar -xJf "$src" -C "$dest"
            ;;
    esac
}

# ---------------------------------------------------------------------------
# On-device (Termux runner): everything is local, just stage and run
# ---------------------------------------------------------------------------
if [ "$LOCAL" -eq 1 ]; then
    CASE="${WINLATOR_CI_DIR:-$HOME/.cache/winlator-ci}"
    mkdir -p "$CASE"
    say "stage"
    unpack "$WINE_ARG" "$CASE/wine" || die "cannot unpack $WINE_ARG"
    unpack "$FEXCORE" "$CASE/components/fexcore" || die "cannot unpack $FEXCORE"
    unpack "$DXVK" "$CASE/components/dxvk" || die "cannot unpack $DXVK"
    if [ -f "$CASE/wine/prefixPack.txz" ] && [ ! -f "$CASE/prefix/.wine/system.reg" ]; then
        mkdir -p "$CASE/prefix"
        tar -xJf "$CASE/wine/prefixPack.txz" -C "$CASE/prefix" ||
            die "cannot extract prefixPack.txz (install xz-utils)"
    fi
    [ -e "$CASE/wine/bin/wineboot" ] ||
        die "wine package is missing bin/wineboot; prefix creation fails without it"

    WINE_ROOT="$CASE/wine" COMPONENTS="$CASE/components" TESTS="$TESTS" \
        PREFIX_DIR="$CASE/prefix" WORK="$CASE/work" TERMUX_USR="$TERMUX_USR" \
        /system/bin/sh "$SCRIPT_DIR/android/run-in-rootfs.sh"
    exit $?
fi

# ---------------------------------------------------------------------------
# Host + adb
# ---------------------------------------------------------------------------
command -v adb > /dev/null 2>&1 || die "adb not found"
[ -n "$SERIAL" ] || die "--serial is required for host mode"
ADB=(adb -s "$SERIAL")
"${ADB[@]}" wait-for-device || die "no device for serial $SERIAL"

say "device"
"${ADB[@]}" shell getprop ro.product.cpu.abi | tr -d '\r' | sed 's/^/  abi: /'
"${ADB[@]}" shell getprop ro.build.version.sdk | tr -d '\r' | sed 's/^/  sdk: /'
ABI="$("${ADB[@]}" shell getprop ro.product.cpu.abi | tr -d '\r')"
case "$ABI" in
    arm64-* | aarch64*) ;;
    *) die "device ABI is '$ABI'; the Wine package is ARM64 only" ;;
esac

if [ "$NO_ROOTFS" -eq 0 ]; then
    say "adb root + Termux prefix"
    if ! "${ADB[@]}" root 2>&1 | grep -qi "already running as root\|restarting adbd as root"; then
        die "adb root failed; use a userdebug/eng image, or pass --no-rootfs"
    fi
    "${ADB[@]}" wait-for-device
    [ -n "$TERMUX_ROOTFS" ] ||
        die "--termux-rootfs is required unless --no-rootfs is given"
    if [ -d "$TERMUX_ROOTFS/data/data/com.termux/files/usr" ]; then
        SRC_TERMUX="$TERMUX_ROOTFS/data/data/com.termux/files/usr"
    elif [ -d "$TERMUX_ROOTFS/usr" ]; then
        SRC_TERMUX="$TERMUX_ROOTFS/usr"
    else
        die "no usr tree in $TERMUX_ROOTFS"
    fi
    "${ADB[@]}" shell mkdir -p /data/data/com.termux/files || die "cannot create /data/data/com.termux/files"
    "${ADB[@]}" shell rm -rf "$TERMUX_USR"
    "${ADB[@]}" push "$SRC_TERMUX" "$TERMUX_USR" > /dev/null ||
        die "pushing the Termux prefix failed"
    note "$TERMUX_USR"
else
    note "rootfs push skipped (--no-rootfs)"
fi

say "stage"
STAGE="$(mktemp -d "${TMPDIR:-/tmp}/winlator-ci.XXXXXX")"
trap 'rm -rf "$STAGE"' EXIT
unpack "$WINE_ARG" "$STAGE/wine" || die "cannot unpack $WINE_ARG"
unpack "$FEXCORE" "$STAGE/components/fexcore" || die "cannot unpack $FEXCORE"
unpack "$DXVK" "$STAGE/components/dxvk" || die "cannot unpack $DXVK"
[ -e "$STAGE/wine/bin/wineboot" ] ||
    die "wine package is missing bin/wineboot; prefix creation fails without it"
if [ -f "$STAGE/wine/prefixPack.txz" ]; then
    mkdir -p "$STAGE/prefix"
    tar -xJf "$STAGE/wine/prefixPack.txz" -C "$STAGE/prefix" ||
        die "cannot extract prefixPack.txz"
fi
mkdir -p "$STAGE/tests" "$STAGE/ci"
cp -f "$SCRIPT_DIR/android/run-in-rootfs.sh" "$STAGE/ci/"
for exe in "$TESTS"/*.exe; do
    [ -f "$exe" ] || continue
    cp -f "$exe" "$STAGE/tests/"
done

say "push"
"${ADB[@]}" shell rm -rf "$REMOTE_DIR"
"${ADB[@]}" shell mkdir -p "$REMOTE_DIR" || die "cannot create $REMOTE_DIR"
for dir in wine components tests prefix ci; do
    [ -d "$STAGE/$dir" ] || continue
    note "$dir"
    "${ADB[@]}" push "$STAGE/$dir" "$REMOTE_DIR/$dir" > /dev/null ||
        die "adb push $dir failed"
done

say "run"
remote_cmd="cd $REMOTE_DIR
WINE_ROOT=$REMOTE_DIR/wine COMPONENTS=$REMOTE_DIR/components TESTS=$REMOTE_DIR/tests \
PREFIX_DIR=$REMOTE_DIR/prefix WORK=$REMOTE_DIR/work TERMUX_USR=$TERMUX_USR \
/system/bin/sh $REMOTE_DIR/ci/run-in-rootfs.sh"

"${ADB[@]}" shell "$remote_cmd"
rc=$?
echo "device exit code: $rc"

if "${ADB[@]}" shell "[ -d $REMOTE_DIR/work/logs ]"; then
    mkdir -p "$LOG_DIR"
    if "${ADB[@]}" pull "$REMOTE_DIR/work/logs/." "$LOG_DIR" > /dev/null 2>&1; then
        note "logs: $LOG_DIR"
    else
        echo "warning: could not pull device logs" >&2
    fi
fi

exit "$rc"
