#!/bin/bash
# Termux on-device GPU benchmark runner
# Usage: ./run.sh [--driver-so /path/to/vulkan.turnip.so] [benchmark args...]

BENCH_BIN="${BENCH_BIN:-./bench-vulkan}"
TMPDIR="${TMPDIR:-/tmp}"
ICD_DIR="$TMPDIR/bench-icd.d"
ICD_JSON="$ICD_DIR/turnip_icd.json"

usage() {
    echo "Usage: $0 [--driver-so <path>] [-- <bench-args>]"
    echo ""
    echo "Options:"
    echo "  --driver-so PATH   Path to vulkan.turnip.so to benchmark"
    echo "  --list-drivers     List available Vulkan drivers on device"
    echo "  --help             Show this help"
    echo ""
    echo "Benchmark args are passed to bench-vulkan:"
    echo "  --iterations N     Benchmark iterations (default: 20)"
    echo "  --compute          Compute benchmark only"
    echo "  --graphics         Graphics benchmark only"
    echo "  --output FILE      Write JSON to file"
    echo ""
    echo "Examples:"
    echo "  # Use system default driver"
    echo "  $0 -- --iterations 50"
    echo ""
    echo "  # Test a specific Turnip build"
    echo "  $0 --driver-so ./vulkan.turnip.so -- --iterations 50 --output results.json"
}

list_drivers() {
    echo "=== Available Vulkan ICDs ==="
    for dir in /vendor/etc/vulkan/icd.d /system/etc/vulkan/icd.d /data/local/tmp; do
        if [ -d "$dir" ]; then
            echo "  [$dir]"
            ls -la "$dir"/*.json 2>/dev/null || echo "    (none)"
        fi
    done
    echo ""
    echo "=== Vulkan loader env ==="
    echo "  VK_ICD_FILENAMES: ${VK_ICD_FILENAMES:-<unset>}"
    echo "  LD_LIBRARY_PATH: ${LD_LIBRARY_PATH:-<unset>}"
}

DRIVER_SO=""
BENCH_ARGS=()

while [ $# -gt 0 ]; do
    case "$1" in
        --driver-so)
            DRIVER_SO="$2"
            shift 2
            ;;
        --list-drivers)
            list_drivers
            exit 0
            ;;
        --help)
            usage
            exit 0
            ;;
        --)
            shift
            BENCH_ARGS=("$@")
            break
            ;;
        *)
            BENCH_ARGS+=("$1")
            shift
            ;;
    esac
done

if [ ! -f "$BENCH_BIN" ]; then
    echo "ERROR: bench-vulkan not found at $BENCH_BIN"
    echo "  Build with: cd benchmark && bash build.sh"
    exit 1
fi

cleanup_icd() {
    if [ -n "$OLD_VK_ICD_FILENAMES" ]; then
        export VK_ICD_FILENAMES="$OLD_VK_ICD_FILENAMES"
    else
        unset VK_ICD_FILENAMES
    fi
    rm -rf "$ICD_DIR" 2>/dev/null
}

if [ -n "$DRIVER_SO" ]; then
    if [ ! -f "$DRIVER_SO" ]; then
        echo "ERROR: Driver not found: $DRIVER_SO"
        exit 1
    fi

    DRIVER_SO=$(realpath "$DRIVER_SO")
    echo "=== Using driver: $DRIVER_SO ==="

    mkdir -p "$ICD_DIR"
    cat > "$ICD_JSON" << JSONEOF
{
    "file_format_version": "1.0.0",
    "ICD": {
        "library_path": "$DRIVER_SO",
        "api_version": "1.3.0"
    }
}
JSONEOF

    OLD_VK_ICD_FILENAMES="${VK_ICD_FILENAMES:-}"
    export VK_ICD_FILENAMES="$ICD_JSON"
    trap cleanup_icd EXIT

    echo "  ICD JSON: $ICD_JSON"
    echo "  VK_ICD_FILENAMES=$VK_ICD_FILENAMES"
    echo ""
fi

echo "=== Running benchmark ==="
echo "  Binary: $BENCH_BIN"
echo "  Args: ${BENCH_ARGS[*]}"
echo ""

"$BENCH_BIN" "${BENCH_ARGS[@]}"
BENCH_EXIT=$?

if [ -n "$DRIVER_SO" ]; then
    cleanup_icd
    trap - EXIT
fi

exit $BENCH_EXIT
