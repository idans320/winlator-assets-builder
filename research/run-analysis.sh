#!/bin/bash
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
MESA_SRC="${MESA_SRC:-$DIR/../mesa/workdir/mesa}"
OUTDIR="$DIR/output"

for arg in "$@"; do
    case "$arg" in
        --mesa=*) MESA_SRC="${arg#*=}" ;;
        --outdir=*) OUTDIR="${arg#*=}" ;;
        --clean) rm -rf "$OUTDIR"/*.dot "$OUTDIR"/*.svg "$OUTDIR"/*.json "$OUTDIR"/traces "$OUTDIR"/diffs
                 mkdir -p "$OUTDIR" "$OUTDIR/traces" "$OUTDIR/diffs"
                 echo "Cleaned $OUTDIR"; exit 0 ;;
    esac
done

if [ -n "${DEVBOX_SHELL:-}" ] || command -v go &>/dev/null; then
    echo "=== Turnip Gen8 Laboratory ==="
    echo "Mesa:  $MESA_SRC"
    echo "Out:   $OUTDIR"
    echo ""

    mkdir -p "$OUTDIR" "$OUTDIR/traces" "$OUTDIR/diffs"

    echo "[1/4] Static index (ctags + ripgrep)..."
    bash "$DIR/scripts/index-functions.sh" "$MESA_SRC" "$OUTDIR/ctags_index.json"
    bash "$DIR/scripts/search-gen8.sh" "$MESA_SRC" "$OUTDIR/gen8_sites.json"
    echo ""

    echo "[2/4] Building Turnip index..."
    go run ./cmd/index-builder --ctags "$OUTDIR/ctags_index.json" --gen8 "$OUTDIR/gen8_sites.json" --out "$OUTDIR/index.json"
    echo ""

    echo "[3/4] Generating Graphviz DOT diagrams..."
    go run ./cmd/graph-builder --input "$OUTDIR/index.json" --outdir "$OUTDIR/"
    echo ""

    echo "[4/4] Rendering DOT -> SVG..."
    for f in "$OUTDIR"/*.dot; do
        base="$(basename "$f" .dot)"
        (sfdp -Tsvg -Goverlap=false -Gsplines=true "$f" -o "$OUTDIR/$base.svg" 2>/dev/null && echo "  ok $base.svg") &
    done
    wait
    echo ""

    echo "=== Complete ==="
    echo ""
    echo "Diagrams:"
    ls -lh "$OUTDIR"/*.svg 2>/dev/null | awk '{printf "  %s  %s\n", $5, $NF}'
elif command -v devbox &>/dev/null; then
    echo "Launching via devbox..."
    exec devbox run -- bash "$0" "$@"
else
    echo "Error: devbox not found and no Go in PATH."
    echo "Install from https://jetify.com/devbox or install Go >=1.22."
    exit 1
fi
