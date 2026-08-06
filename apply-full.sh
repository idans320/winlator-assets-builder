#!/bin/bash -e
# Full Arsenal — SIMD Heresies + Bandwidth Lite
# ============================================================================
# Runs both injection scripts against a clean Mesa Turnip clone.
# Build with:  devbox run -- bash ./mesa/build.sh
#
# Applies:
#   11 CPU microarchitecture attacks (A,B,C,D/E/F/G/H/I/J/K/N/O/P)
#    2 GPU bandwidth attacks    (VRS 4x4 + LOD bias +4.0)
# ============================================================================

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

echo "=== Stage 1/2: SIMD Heresies ==="
bash "$SCRIPT_DIR/apply-simd-heresies.sh"

echo ""
echo "=== Stage 2/2: Bandwidth Lite ==="
bash "$SCRIPT_DIR/apply-bandwidth-lite.sh"

echo ""
echo "=== Full Arsenal injection complete ==="
