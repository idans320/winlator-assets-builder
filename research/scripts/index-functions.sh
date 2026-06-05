#!/bin/bash
set -euo pipefail
MESA_SRC="${1:-$MESA_SRC}"
OUT="${2:-output/ctags_index.json}"

echo "Indexing functions in $MESA_SRC/src/freedreno/vulkan ..."
mkdir -p "$(dirname "$OUT")"

ctags --output-format=json --fields=+ln --languages=C++ \
  $(find "$MESA_SRC/src/freedreno/vulkan" -name '*.cc' -o -name '*.h' 2>/dev/null) \
  > "$OUT" 2>/dev/null || {
    echo "ERROR: ctags failed. Is universal-ctags installed?"
    exit 1
}

count=$(grep -c '"kind":"function"' "$OUT" 2>/dev/null || echo 0)
echo "  Found $count function definitions → $OUT"
