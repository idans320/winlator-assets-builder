#!/bin/bash
# ci/build-test-programs.sh — build the Win32 PE smoke tests.
#
# Produces load_dlls_x64.exe (0x8664), load_dlls_i386.exe (0x14C) and
# load_dlls_arm64.exe (0xAA64). The x64/i386 binaries are executed under Wine
# on an ARM64 runner (FEX ARM64EC / WoW64 paths); the arm64 one is optional and
# runs on native ARM64 Wine.
#
# Toolchain discovery order:
#   1. --toolchain DIR
#   2. $ARM64EC_MINGW_ROOT (bylaws llvm-mingw; superset toolchain)
#   3. /opt/llvm-mingw-arm64ec
#   4. $LLVM_MINGW_ROOT, /opt/llvm-mingw
#   5. whatever is on PATH
#
# Usage:
#   ci/build-test-programs.sh [--out DIR] [--toolchain DIR] [--only x64,i386,arm64]
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
OUT_DIR="$ROOT_DIR/ci/build/tests"
TOOLCHAIN=""
ONLY="x64,i386,arm64"

while [ $# -gt 0 ]; do
    case "$1" in
        --out) OUT_DIR="${2:?--out needs a value}"; shift ;;
        --toolchain) TOOLCHAIN="${2:?--toolchain needs a value}"; shift ;;
        --only) ONLY="${2:?--only needs a value}"; shift ;;
        -h | --help)
            sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *)
            echo "unknown argument: $1" >&2
            exit 2
            ;;
    esac
    shift
done

for candidate in "$TOOLCHAIN" "${ARM64EC_MINGW_ROOT:-}" /opt/llvm-mingw-arm64ec \
    "${LLVM_MINGW_ROOT:-}" /opt/llvm-mingw; do
    if [ -n "$candidate" ] && [ -x "$candidate/bin/x86_64-w64-mingw32-clang" ]; then
        TOOLCHAIN="$candidate"
        break
    fi
done

if [ -n "$TOOLCHAIN" ]; then
    echo "toolchain: $TOOLCHAIN"
    PATH="$TOOLCHAIN/bin:$PATH"
    export PATH
else
    echo "toolchain: PATH ($(command -v clang || echo 'clang not found'))"
fi

CFLAGS=(-Os -nostdlib -fno-stack-protector -fno-builtin
    '-Wl,--entry,mainCRTStartup' '-Wl,--subsystem,console'
    '-Wl,--no-insert-timestamp' -lkernel32)

mkdir -p "$OUT_DIR"

want() {
    case ",$ONLY," in
        *",$1,"*) return 0 ;;
        *) return 1 ;;
    esac
}

build() {
    local triple="$1" out="$2" src="$3"
    local cc="${triple}-w64-mingw32-clang"
    if ! command -v "$cc" > /dev/null 2>&1; then
        echo "error: $cc not found (toolchain lacks the $triple driver)" >&2
        exit 1
    fi
    echo "  $cc -> $(basename "$out")"
    "$cc" "${CFLAGS[@]}" -o "$OUT_DIR/$out" "$ROOT_DIR/ci/tests/$src"
}

echo "building PE smoke tests into $OUT_DIR"
if want x64; then
    build x86_64 load_dlls_x64.exe load_dlls_x64.c
fi
if want i386; then
    build i686 load_dlls_i386.exe load_dlls_i386.c
fi
if want arm64; then
    build aarch64 load_dlls_arm64.exe load_dlls_arm64.c
fi

# Report machine types so a log alone is enough to spot a broken toolchain.
echo
echo "results:"
for exe in "$OUT_DIR"/*.exe; do
    [ -f "$exe" ] || continue
    if command -v llvm-readobj > /dev/null 2>&1; then
        machine=$(llvm-readobj --file-headers "$exe" |
            sed -n 's/.*Machine: \(.*\)/\1/p' | head -1)
    else
        machine="(llvm-readobj not available)"
    fi
    printf '  %-24s %s\n' "$(basename "$exe")" "$machine"
done
