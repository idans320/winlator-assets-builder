#!/bin/bash
set -euo pipefail
MESA_SRC="${1:-$MESA_SRC}"
OUT="${2:-output/gen8_sites.json}"

echo "Searching for gen8 code sites in $MESA_SRC/src/freedreno/vulkan ..."
mkdir -p "$(dirname "$OUT")"

rg --json -n --context 3 '(?i)(chip\s*[<>=!]+\s*a8xx|A8XX_|gen8|a8xx)' \
  "$MESA_SRC/src/freedreno/vulkan" \
  > "$OUT" 2>/dev/null || {
    echo "ERROR: ripgrep failed. Is ripgrep installed?"
    exit 1
}

count=$(grep -c '"type":"match"' "$OUT" 2>/dev/null || echo 0)
echo "  Found $count gen8 code sites → $OUT"
