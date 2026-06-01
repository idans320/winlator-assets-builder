#!/bin/bash -e
# Run Turnip sync tests (native + aarch64 QEMU)
TESTS_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$(dirname "$TESTS_DIR")")"

green='\033[0;32m'
red='\033[0;31m'
nocolor='\033[0m'

PASSED=0
FAILED=0

# --- Native test ---
echo -e "${green}=== Native test ===${nocolor}"
if [ -x "$TESTS_DIR/test_kgsl_sync" ]; then
    if "$TESTS_DIR/test_kgsl_sync"; then
        PASSED=$((PASSED + 1))
        echo -e "${green}Native test PASSED${nocolor}"
    else
        FAILED=$((FAILED + 1))
        echo -e "${red}Native test FAILED${nocolor}"
    fi
else
    echo "Native binary not found. Run ./build.sh first."
    FAILED=$((FAILED + 1))
fi

# --- Aarch64 QEMU test ---
echo ""
echo -e "${green}=== Aarch64 QEMU test ===${nocolor}"

QEMU="$(which qemu-aarch64-static 2>/dev/null || echo '')"
if [ -z "$QEMU" ]; then
    echo "qemu-aarch64-static not found (install qemu-user package)"
    echo "Skipping aarch64 test"
elif [ -x "$TESTS_DIR/test_kgsl_sync_aarch64" ]; then
    if "$QEMU" "$TESTS_DIR/test_kgsl_sync_aarch64"; then
        PASSED=$((PASSED + 1))
        echo -e "${green}Aarch64 QEMU test PASSED${nocolor}"
    else
        FAILED=$((FAILED + 1))
        echo -e "${red}Aarch64 QEMU test FAILED${nocolor}"
    fi
else
    echo "Aarch64 binary not found. Build with ./build.sh inside devbox."
    FAILED=$((FAILED + 1))
fi

# --- Aarch64 test vs actual Turnip .so (via LD_PRELOAD) ---
echo ""
echo -e "${green}=== Aarch64 test with real Turnip stub ===${nocolor}"

TURNIP_SO="$ROOT_DIR/mesa/workdir/package/vulkan.turnip.so"
STUB_SO="$TESTS_DIR/libstub_kgsl.so"

if [ -f "$TURNIP_SO" ] && [ -f "$STUB_SO" ]; then
    if [ -x "$TESTS_DIR/test_kgsl_sync_aarch64" ] && [ -n "$QEMU" ]; then
        echo "Running with LD_PRELOAD=$STUB_SO"
        if LD_PRELOAD="$STUB_SO" "$QEMU" "$TESTS_DIR/test_kgsl_sync_aarch64"; then
            echo -e "${green}Real .so stub test PASSED${nocolor}"
        else
            echo -e "${red}Real .so stub test FAILED${nocolor}"
        fi
    fi
else
    echo "Skipping: build libstub_kgsl.so first"
fi

echo ""
echo -e "Tests: ${green}$PASSED passed${nocolor}, ${red}$FAILED failed${nocolor}"
if [ "$FAILED" -gt 0 ]; then exit 1; fi
