#!/bin/bash
# ci/build-wine-test.sh — build a *Linux/glibc* ARM64X Wine for the runtime
# tests. This is a test fixture, not a Winlator package.
#
# The wine-arm64-*.wcp in this repo is built with
# --host=aarch64-linux-android28 (bionic, NDK sysroot, interpreter
# /system/bin/linker64). It runs on Android only. To execute the built PE DLLs
# on a glibc ARM64 runner (ubuntu-24.04-arm or an arm64 container) we need a
# Wine whose Unix side is glibc, built with the same PE architecture set:
#
#     --enable-archs=arm64ec,aarch64,i386 --with-mingw=clang
#
# The PE side is what is under test (machine types, imports, DllMain through
# the ARM64EC/FEX path). The Unix side of the shipped package is NOT validated
# by this build — that requires an Android device; see ci/android-run-tests.sh.
#
# Requirements (Debian/Ubuntu names):
#   build-essential autoconf automake bison flex gettext pkg-config python3 \
#   git xz-utils ca-certificates
#   plus an ARM64EC-capable llvm-mingw (bylaws fork) — fetched automatically by
#   scripts/setup-arm64ec-toolchain.sh when not already present.
#
# Usage:
#   ci/build-wine-test.sh [--prefix DIR] [--src DIR] [--jobs N] [--archs LIST]
#                         [--commit SHA] [--repo URL] [--branch NAME]
#                         [--rebuild] [--keep-src]
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PREFIX="$ROOT_DIR/ci/build/wine-test"
SRC="$ROOT_DIR/ci/build/wine-src"
JOBS="$(nproc 2> /dev/null || echo 4)"
ARCHS="arm64ec,aarch64,i386"
COMMIT=""
REPO=""
BRANCH=""
REBUILD=0

while [ $# -gt 0 ]; do
    case "$1" in
        --prefix) PREFIX="${2:?--prefix needs a value}"; shift ;;
        --src) SRC="${2:?--src needs a value}"; shift ;;
        --jobs) JOBS="${2:?--jobs needs a value}"; shift ;;
        --archs) ARCHS="${2:?--archs needs a value}"; shift ;;
        --commit) COMMIT="${2:?--commit needs a value}"; shift ;;
        --repo) REPO="${2:?--repo needs a value}"; shift ;;
        --branch) BRANCH="${2:?--branch needs a value}"; shift ;;
        --rebuild) REBUILD=1 ;;
        -h | --help)
            sed -n '2,35p' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *)
            echo "unknown argument: $1" >&2
            exit 2
            ;;
    esac
    shift
done

say() { printf '\n== %s ==\n' "$*"; }

if [ "$(uname -m)" != "aarch64" ]; then
    echo "warning: host is $(uname -m); a native aarch64 build is expected (the Wine binaries are not portable across host arches)" >&2
fi

# --- wine source coordinates (packages.yml when yq is available) -----------

if command -v yq > /dev/null 2>&1 && [ -f "$ROOT_DIR/packages.yml" ]; then
    [ -n "$REPO" ] || REPO="$(yq '.wine.repo' "$ROOT_DIR/packages.yml")"
    [ -n "$BRANCH" ] || BRANCH="$(yq '.wine.branch' "$ROOT_DIR/packages.yml")"
fi
[ -n "$REPO" ] || REPO="https://github.com/GameNative/proton-wine.git"
[ -n "$BRANCH" ] || BRANCH="proton_11.0"

# --- toolchain -------------------------------------------------------------

say "toolchain"
if [ -z "${ARM64EC_MINGW_ROOT:-}" ] || [ ! -x "${ARM64EC_MINGW_ROOT}/bin/arm64ec-w64-mingw32-clang" ]; then
    if [ -x "$ROOT_DIR/scripts/setup-arm64ec-toolchain.sh" ]; then
        echo "ARM64EC toolchain not in environment; running scripts/setup-arm64ec-toolchain.sh"
        "$ROOT_DIR/scripts/setup-arm64ec-toolchain.sh" > /dev/null
        ARM64EC_MINGW_ROOT="$ROOT_DIR/toolchain/arm64ec"
    else
        echo "error: no ARM64EC llvm-mingw; set ARM64EC_MINGW_ROOT" >&2
        exit 1
    fi
fi
export ARM64EC_MINGW_ROOT
export PATH="$ARM64EC_MINGW_ROOT/bin:$PATH"
echo "mingw: $ARM64EC_MINGW_ROOT ($(clang --version 2> /dev/null | head -1))"

for tool in gcc make bison flex autoconf pkg-config; do
    command -v "$tool" > /dev/null 2>&1 ||
        { echo "error: missing build dependency: $tool" >&2; exit 1; }
done

# --- source ----------------------------------------------------------------

say "source"
if [ ! -d "$SRC/.git" ]; then
    mkdir -p "$(dirname "$SRC")"
    echo "cloning $REPO ($BRANCH)"
    git clone --depth 1 --branch "$BRANCH" "$REPO" "$SRC"
fi
if [ -n "$COMMIT" ]; then
    git -C "$SRC" fetch --depth 1 origin "$COMMIT" 2> /dev/null || true
    git -C "$SRC" checkout "$COMMIT"
fi
WINE_COMMIT="$(git -C "$SRC" rev-parse --short HEAD)"
echo "wine commit: $WINE_COMMIT"

# --- build -----------------------------------------------------------------

if [ "$REBUILD" -eq 0 ] &&
    [ -f "$PREFIX/lib/wine/aarch64-windows/ntdll.dll" ]; then
    echo "existing install found at $PREFIX (use --rebuild to redo)"
    found_ntdll="$PREFIX/lib/wine/aarch64-windows/ntdll.dll"
else
    say "configure"
    cd "$SRC"
    if [ ! -x ./configure ]; then
        echo "generating configure"
        if [ -x ./autogen.sh ]; then
            NOCONFIGURE=1 ./autogen.sh
        else
            autoreconf -f -i
        fi
    fi

    # Headless, dependency-light: the loader tests only need ntdll/kernel32 and
    # the PE DLLs under test. Keep the arch set identical to the Android build.
    ./configure \
        --prefix="$PREFIX" \
        --enable-archs="$ARCHS" \
        --with-mingw=clang \
        --disable-tests \
        --disable-nls \
        --without-x \
        --without-freetype \
        --without-fontconfig \
        --without-alsa \
        --without-pulse \
        --without-opengl \
        --without-vulkan \
        --without-gstreamer \
        --without-gnutls \
        --without-dbus \
        --without-udev \
        --without-cups \
        --without-sane \
        --without-krb5 \
        --without-netapi \
        --without-opencl \
        --without-oss \
        --without-pcap \
        --without-pcsclite \
        --without-sdl \
        --without-usb \
        --without-v4l2 \
        --without-wayland \
        --without-unwind \
        --without-ffmpeg

    say "build"
    make -j"$JOBS"
    make install
    found_ntdll="$PREFIX/lib/wine/aarch64-windows/ntdll.dll"
fi

say "verify"
[ -f "$found_ntdll" ] || {
    echo "error: $found_ntdll was not installed" >&2
    exit 1
}

machine=""
if command -v llvm-readobj > /dev/null 2>&1; then
    machine="$(llvm-readobj --file-headers "$found_ntdll" |
        sed -n 's/.*Machine:.*(\(0x[0-9A-Fa-f]*\)).*/\1/p' | head -1)"
elif command -v python3 > /dev/null 2>&1; then
    machine="$(python3 - "$found_ntdll" << 'PY'
import struct, sys
with open(sys.argv[1], "rb") as fh:
    off = struct.unpack_from("<I", fh.read(0x40), 0x3C)[0]
    fh.seek(off)
    hdr = fh.read(6)
    print("0x%04X" % struct.unpack_from("<H", hdr, 4)[0])
PY
    )"
fi

echo "ntdll.dll machine: ${machine:-unknown}"
if [ -n "$machine" ] && [ "${machine^^}" != "0XA64E" ]; then
    cat >&2 << EOF
error: the test Wine is not ARM64X ($machine). It cannot load ARM64EC DLLs.
       Check that --enable-archs=$ARCHS was accepted and that the ARM64EC
       llvm-mingw was first in PATH.
EOF
    exit 1
fi

if ! "$PREFIX/bin/wine" --version > /dev/null 2>&1; then
    echo "error: $PREFIX/bin/wine does not run on this host" >&2
    exit 1
fi

cat << EOF

Linux ARM64X test Wine ready:
  prefix:  $PREFIX
  wine:    $PREFIX/bin/wine ($("$PREFIX/bin/wine" --version))
  commit:  $WINE_COMMIT

Run the DLL tests with:
  ci/run-wine-tests.sh --wine "$PREFIX/bin/wine" \\
      --fexcore <fexcore.wcp> --dxvk <dxvk.wcp> --tests ci/build/tests
EOF
