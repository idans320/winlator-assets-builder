#!/bin/bash
# Turnip gen8 (Adreno 8xx) AST Analysis & Graphviz Pipeline
# Run inside devbox for hermetic Go + Graphviz environment
set -e

DIR="$(cd "$(dirname "$0")" && pwd)"
MESA_SRC="${MESA_SRC:-$DIR/../mesa/workdir/mesa}"
OUTDIR="$DIR/output"

for arg in "$@"; do
    case "$arg" in
        --mesa=*) MESA_SRC="${arg#*=}" ;;
        --outdir=*) OUTDIR="${arg#*=}" ;;
        --clean) rm -rf "$OUTDIR"/*.dot "$OUTDIR"/*.svg "$OUTDIR"/*.json; echo "Cleaned $OUTDIR"; exit 0 ;;
    esac
done

if [ -n "$DEVBOX_SHELL" ]; then
    echo "=== Turnip gen8 Neuron Tracer & Graphviz Pipeline ==="
    echo "Mesa:  $MESA_SRC"
    echo "Out:   $OUTDIR"
    echo ""

    mkdir -p "$OUTDIR"

    echo "[1/3] AST Analysis - extracting gen8 code paths..."
    go run ./analysis/cmd/ast-analyzer --mesa "$MESA_SRC" --out "$OUTDIR/analysis.json"
    echo ""

    echo "[2/3] Bottleneck discovery..."
    go run ./analysis/cmd/analyze-bottleneck --input "$OUTDIR/analysis.json"
    echo ""

    echo "[3/3] Generating Graphviz DOT diagrams..."
    go run ./analysis/cmd/graph-gen --input "$OUTDIR/analysis.json" --outdir "$OUTDIR/"
    echo ""

    echo "[render] Rendering DOT -> SVG (parallel sfdp)..."
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
    cd "$DIR"
    exec devbox run -- bash "$0" "$@"
else
    echo "Error: devbox not found. Install from https://jetify.com/devbox"
    echo "Running directly..."
    bash "$0" "$@"
fi
