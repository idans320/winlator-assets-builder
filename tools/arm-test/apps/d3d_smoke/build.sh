#!/bin/bash
# Build the D3D smoke test as an x86_64 PE, i.e. the exact shape a Wine-on-ARM64
# host runs through FEX (libarm64ecfex) — the Winlator case.
#
#   tools/arm-test/apps/d3d_smoke/build.sh [out-dir]
#
# Uses the ARM64EC-capable llvm-mingw from packages.yml, which also ships the
# x86_64-w64-mingw32 target.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../../../.." && pwd)"
OUT="${1:-$HERE}"

TC="${ARM64EC_MINGW_ROOT:-$ROOT/toolchain/arm64ec}"
CXX="$TC/bin/x86_64-w64-mingw32-clang++"
if [ ! -x "$CXX" ]; then
    echo "x86_64-w64-mingw32-clang++ not found at $CXX" >&2
    echo "run scripts/setup-arm64ec-toolchain.sh first" >&2
    exit 1
fi

mkdir -p "$OUT"
"$CXX" -std=c++17 -O2 -Wall -Wextra \
    -o "$OUT/smoke.exe" "$HERE/smoke.cpp" \
    -ld3d11 -ld3d9 -ldxgi -ld3dcompiler_47 -lole32 -luuid
echo "built $OUT/smoke.exe"
