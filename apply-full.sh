#!/bin/bash -e
# Full Arsenal — SIMD Heresies + Bandwidth Lite
# ============================================================================
# Runs both injection scripts against a clean Mesa Turnip clone.
# Build with:  devbox run -- bash ./mesa/build.sh
#
# Proven effective (asm probe on Snapdragon X Elite / Oryon-1):
#   A  — rbit+clz branchless dispatch      -78.4%
#   DJ — 4-wide ILP UBO patching           -13.7%
#   E  — Branchless BITSET dispatch        -13.9%
#
# Removed (lose in isolation): C (+28%), N (+290%), K (+12%)
#
# GPU bandwidth attacks (VRS 4x4 + LOD bias +4.0) are unconditional.
# ============================================================================

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

echo "=== Stage 1/2: SIMD Heresies ==="
bash "$SCRIPT_DIR/apply-simd-heresies.sh"

echo ""
echo "=== Stage 2/2: Bandwidth Lite ==="
bash "$SCRIPT_DIR/apply-bandwidth-lite.sh"

echo ""
echo "=== Full Arsenal injection complete ==="
