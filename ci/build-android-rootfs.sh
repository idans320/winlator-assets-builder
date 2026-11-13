#!/bin/bash
# ci/build-android-rootfs.sh — assemble an Android runtime rootfs for the Wine
# DLL tests.
#
# The wine-arm64 package is bionic: it needs /system/bin/linker64, the Android
# system libs, and its Termux dependencies at exactly
# /data/data/com.termux/files/usr (that path is a hardcoded RUNPATH in the Wine
# and Termux binaries). The rootfs built here has that layout:
#
#   <root>/system/                              Android system image
#   <root>/data/data/com.termux/files/usr/      Termux prefix (libraries)
#   <root>/opt/wine/                             extracted wine-arm64-*.wcp
#   <root>/opt/components/{fexcore,dxvk}/        extracted component packages
#   <root>/opt/tests/                            PE test exes
#   <root>/opt/prefix/.wine/                     extracted prefixPack.txz
#   <root>/opt/ci/run-in-rootfs.sh               in-rootfs test driver
#
# System image sources (pick one):
#   --system-dir DIR        an already extracted /system tree
#   --system-image FILE     system.img (sparse or ext4); uses simg2img+debugfs
#   --sdk-package NAME      e.g. "system-images;android-28;default;arm64-v8a"
#                           (requires sdkmanager; installs into --android-home)
#
# Usage:
#   ci/build-android-rootfs.sh --root ci-root \
#       --sdk-package "system-images;android-28;default;arm64-v8a" \
#       --termux-rootfs wine/workdir/termux-rootfs \
#       --wine wine-arm64-*.wcp --fexcore fexcore-*.wcp --dxvk dxvk-*.wcp \
#       --tests ci/build/tests
#
# Requirements (Debian/Ubuntu):
#   e2fsprogs (debugfs), android-sdk-libsparse-utils (simg2img), tar, xz-utils,
#   binutils (ar), curl, openjdk-17-jre-headless + cmdline-tools for --sdk-package.
set -uo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ROOT=""
SYSTEM_DIR=""
SYSTEM_IMAGE=""
SDK_PACKAGE=""
SDKMANAGER=""
ANDROID_HOME="${ANDROID_HOME:-}"
TERMUX_ROOTFS=""
WINE=""
FEXCORE=""
DXVK=""
TESTS="$ROOT_DIR/ci/build/tests"
FORCE=0

usage() {
    sed -n '2,42p' "$0" | sed 's/^# \{0,1\}//'
    exit 0
}

die() {
    echo "error: $*" >&2
    exit 2
}

note() { printf '  %s\n' "$*"; }
say() { printf '\n== %s ==\n' "$*"; }

while [ $# -gt 0 ]; do
    case "$1" in
        --root) ROOT="${2:?}"; shift ;;
        --system-dir) SYSTEM_DIR="${2:?}"; shift ;;
        --system-image) SYSTEM_IMAGE="${2:?}"; shift ;;
        --sdk-package) SDK_PACKAGE="${2:?}"; shift ;;
        --sdkmanager) SDKMANAGER="${2:?}"; shift ;;
        --android-home) ANDROID_HOME="${2:?}"; shift ;;
        --termux-rootfs) TERMUX_ROOTFS="${2:?}"; shift ;;
        --wine) WINE="${2:?}"; shift ;;
        --fexcore) FEXCORE="${2:?}"; shift ;;
        --dxvk) DXVK="${2:?}"; shift ;;
        --tests) TESTS="${2:?}"; shift ;;
        --force) FORCE=1 ;;
        -h | --help) usage ;;
        *)
            echo "unknown argument: $1" >&2
            exit 2
            ;;
    esac
    shift
done

[ -n "$ROOT" ] || die "--root is required"
[ -n "$WINE" ] || die "--wine is required"
[ -n "$FEXCORE" ] || die "--fexcore is required"
[ -n "$DXVK" ] || die "--dxvk is required"
[ -n "$TERMUX_ROOTFS" ] || die "--termux-rootfs is required"

# --- helpers ---------------------------------------------------------------

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
            command -v unzip > /dev/null 2>&1 || die "unzip is required for $src"
            unzip -q "$src" -d "$dest" || return 1
            ;;
        *)
            tar -xJf "$src" -C "$dest" || return 1
            ;;
    esac
}

need() {
    command -v "$1" > /dev/null 2>&1 || die "missing tool: $1 ($2)"
}

need readelf "binutils"

# --- system image ----------------------------------------------------------

if [ "$FORCE" -eq 1 ] && [ -n "$ROOT" ] && [ -d "$ROOT" ]; then
    rm -rf "$ROOT"
fi
mkdir -p "$ROOT/system"

say "system image"
if [ -n "$SYSTEM_DIR" ]; then
    [ -d "$SYSTEM_DIR" ] || die "--system-dir not found: $SYSTEM_DIR"
    SRC="$SYSTEM_DIR"
    # Accept either <dir>/system/... or <dir>/bin/linker64.
    if [ -d "$SYSTEM_DIR/system" ]; then
        SRC="$SYSTEM_DIR/system"
    fi
    note "copying $SRC"
    cp -a "$SRC/." "$ROOT/system/" || die "cannot copy system tree"
elif [ -n "$SYSTEM_IMAGE" ]; then
    [ -f "$SYSTEM_IMAGE" ] || die "--system-image not found: $SYSTEM_IMAGE"
    need debugfs "e2fsprogs"
    TMP="$(mktemp -d)"
    trap 'rm -rf "$TMP"' EXIT
    IMG="$SYSTEM_IMAGE"
    if command -v simg2img > /dev/null 2>&1 &&
        head -c 4 "$SYSTEM_IMAGE" | od -An -tx1 | grep -qi '3a ff 26 ed'; then
        note "converting sparse image with simg2img"
        simg2img "$SYSTEM_IMAGE" "$TMP/system.raw.img" || die "simg2img failed"
        IMG="$TMP/system.raw.img"
    fi
    note "extracting /system with debugfs"
    mkdir -p "$TMP/out"
    debugfs -R "rdump /system $TMP/out" "$IMG" > /dev/null 2>&1 ||
        die "debugfs could not extract /system from $IMG"
    cp -a "$TMP/out/." "$ROOT/system/" || die "cannot copy extracted system"
elif [ -n "$SDK_PACKAGE" ]; then
    [ -n "$ANDROID_HOME" ] || die "--android-home (or ANDROID_HOME) is required with --sdk-package"
    if [ -z "$SDKMANAGER" ]; then
        for candidate in \
            "$ANDROID_HOME/cmdline-tools/latest/bin/sdkmanager" \
            "$ANDROID_HOME/cmdline-tools/bin/sdkmanager" \
            "$(command -v sdkmanager || true)"; do
            [ -n "$candidate" ] && [ -x "$candidate" ] && SDKMANAGER="$candidate" && break
        done
    fi
    [ -n "$SDKMANAGER" ] ||
        die "sdkmanager not found; install cmdline-tools or pass --sdkmanager"
    note "installing $SDK_PACKAGE (this downloads ~1 GiB)"
    yes | "$SDKMANAGER" --sdk_root="$ANDROID_HOME" --install "$SDK_PACKAGE" > /dev/null ||
        die "sdkmanager failed for $SDK_PACKAGE"
    # system-images;android-28;default;arm64-v8a -> android-28/default/arm64-v8a/system.img
    REST="${SDK_PACKAGE#*;}"    # android-28;default;arm64-v8a
    API="${REST%%;*}"           # android-28
    REST="${REST#*;}"           # default;arm64-v8a
    TAG="${REST%%;*}"           # default
    ABI="${REST#*;}"            # arm64-v8a
    SYSIMG="$ANDROID_HOME/system-images/$API/$TAG/$ABI/system.img"
    [ -f "$SYSIMG" ] || die "system image not found after install: $SYSIMG"
    rm -rf "$TMP"
    exec "$0" --root "$ROOT" --system-image "$SYSIMG" \
        --termux-rootfs "$TERMUX_ROOTFS" \
        ${FORCE:+--force} \
        --wine "$WINE" --fexcore "$FEXCORE" --dxvk "$DXVK" --tests "$TESTS"
else
    die "one of --system-dir, --system-image, --sdk-package is required"
fi

[ -x "$ROOT/system/bin/linker64" ] ||
    die "no /system/bin/linker64 in the rootfs: the system image is not 64-bit Android"
[ -f "$ROOT/system/lib64/libc.so" ] ||
    die "no /system/lib64/libc.so in the rootfs"

# --- Termux prefix ---------------------------------------------------------

say "termux prefix"
# The path is hardcoded in the Wine/Termux RUNPATHs: do not relocate it.
TERMUX_USR="$ROOT/data/data/com.termux/files/usr"
if [ -d "$TERMUX_ROOTFS/data/data/com.termux/files/usr" ]; then
    SRC_TERMUX="$TERMUX_ROOTFS/data/data/com.termux/files/usr"
elif [ -d "$TERMUX_ROOTFS/usr" ]; then
    SRC_TERMUX="$TERMUX_ROOTFS/usr"
elif [ -d "$TERMUX_ROOTFS/lib" ]; then
    SRC_TERMUX="$TERMUX_ROOTFS"
else
    die "no Termux usr tree in $TERMUX_ROOTFS"
fi
mkdir -p "$TERMUX_USR"
note "$SRC_TERMUX -> /data/data/com.termux/files/usr"
cp -a "$SRC_TERMUX/." "$TERMUX_USR/" || die "cannot copy Termux prefix"
for lib in libc.so libdl.so; do
    [ -e "$TERMUX_USR/lib/$lib" ] &&
        note "note: $lib is present in the Termux prefix (unexpected on Android)"
done

# --- components ------------------------------------------------------------

say "wine package"
unpack "$WINE" "$ROOT/opt/wine" || die "cannot unpack $WINE"
[ -x "$ROOT/opt/wine/lib/wine/aarch64-unix/wine" ] ||
    [ -x "$ROOT/opt/wine/bin/wine" ] ||
    die "wine package has no loader"
[ -e "$ROOT/opt/wine/bin/wineboot" ] ||
    die "wine package is missing bin/wineboot (Winlator prefix creation requires it)"
[ -e "$ROOT/opt/wine/bin/winecfg" ] ||
    die "wine package is missing bin/winecfg"
note "wine: $(basename "$WINE")"

say "components"
unpack "$FEXCORE" "$ROOT/opt/components/fexcore" || die "cannot unpack $FEXCORE"
unpack "$DXVK" "$ROOT/opt/components/dxvk" || die "cannot unpack $DXVK"
[ -f "$ROOT/opt/components/fexcore/system32/libarm64ecfex.dll" ] ||
    die "fexcore package has no system32/libarm64ecfex.dll"
note "fexcore: $(basename "$FEXCORE")"
note "dxvk:    $(basename "$DXVK")"

say "tests"
mkdir -p "$ROOT/opt/tests"
if [ -d "$TESTS" ]; then
    cp -f "$TESTS"/*.exe "$ROOT/opt/tests/" 2> /dev/null
    note "$(ls "$ROOT/opt/tests"/*.exe 2> /dev/null | wc -l) exe(s)"
else
    note "warning: no test directory at $TESTS"
fi

# --- prefix ----------------------------------------------------------------

say "prefix"
if [ -f "$ROOT/opt/wine/prefixPack.txz" ]; then
    mkdir -p "$ROOT/opt/prefix"
    tar -xJf "$ROOT/opt/wine/prefixPack.txz" -C "$ROOT/opt/prefix" ||
        die "cannot extract prefixPack.txz"
    note "prefixPack.txz -> /opt/prefix"
else
    note "warning: wine package has no prefixPack.txz; the in-rootfs driver will fail"
fi

# --- in-rootfs driver ------------------------------------------------------

mkdir -p "$ROOT/opt/ci"
cp -f "$ROOT_DIR/ci/android/run-in-rootfs.sh" "$ROOT/opt/ci/run-in-rootfs.sh"
chmod +x "$ROOT/opt/ci/run-in-rootfs.sh"

# --- RUNPATH sanity --------------------------------------------------------
#
# Termux binaries hardcode /data/data/com.termux/files/usr/lib and Wine's loader
# has the same RUNPATH. If anything still points at /usr/lib (a non-Termux
# build), add the symlink the userland expects and report every other absolute
# path that does not exist inside the rootfs.

say "runpath check"
UNRESOLVED=""
SCANNED=0
while IFS= read -r elf; do
    SCANNED=$((SCANNED + 1))
    while IFS= read -r dir; do
        [ -n "$dir" ] || continue
        case "$dir" in
            '$ORIGIN'* | /system/* | /data/data/com.termux/files/usr/*) ;;
            /usr/lib | /usr/lib64)
                if [ ! -e "$ROOT$dir" ]; then
                    mkdir -p "$ROOT/usr"
                    ln -sfn /data/data/com.termux/files/usr/lib "$ROOT/usr/lib" 2> /dev/null
                    note "symlinked $dir -> /data/data/com.termux/files/usr/lib"
                fi
                ;;
            /*)
                if [ ! -e "$ROOT$dir" ]; then
                    UNRESOLVED="$UNRESOLVED $dir"
                fi
                ;;
        esac
    done < <(readelf -d "$elf" 2> /dev/null |
        sed -n -e 's/.*(RUNPATH).*\[\(.*\)\]/\1/p' \
            -e 's/.*(RPATH).*\[\(.*\)\]/\1/p' |
        tr ':' '\n')
done < <(find "$ROOT/opt/wine" "$TERMUX_USR/lib" -type f \
    \( -name '*.so' -o -name '*.so.*' \) 2> /dev/null)

note "scanned $SCANNED shared objects"
if [ -n "$UNRESOLVED" ]; then
    echo "  unresolved RUNPATH directories:" >&2
    for dir in $(printf '%s\n' "$UNRESOLVED" | tr ' ' '\n' | sort -u); do
        echo "    $dir" >&2
    done
    die "the rootfs is missing directories the binaries link against"
fi
note "all absolute RUNPATHs resolve inside the rootfs"

# --- summary ---------------------------------------------------------------

say "rootfs ready"
du -sh "$ROOT" 2> /dev/null | sed 's/^/  size: /'
cat << EOF

  root:     $ROOT
  system:   $ROOT/system (linker64 present)
  termux:   $ROOT/data/data/com.termux/files/usr
  wine:     $ROOT/opt/wine
  prefix:   $ROOT/opt/prefix/.wine

Run the tests with:
  ci/run-android-rootfs-tests.sh --root "$ROOT"
EOF
