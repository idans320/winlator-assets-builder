#!/bin/bash
set -e

DURATION="${1:-0}"
DEVICE="${2:-}"
LOG_FILE="mesa_debug_$(date +%Y%m%d_%H%M%S).log"

if [ -n "$DEVICE" ]; then
    ADB="adb -s $DEVICE"
elif [ -n "$ANDROID_SERIAL" ]; then
    ADB="adb -s $ANDROID_SERIAL"
else
    DEVICE_COUNT=$(adb devices 2>/dev/null | grep -wc "device$")
    if [ "$DEVICE_COUNT" -gt 1 ]; then
        echo "Multiple devices detected. Specify device:"
        adb devices | grep "device$" | while read serial _; do
            echo "  $serial"
        done
        echo "Usage: $0 [duration] [device_serial]"
        echo "Example: $0 0 10.0.0.4:41997"
        exit 1
    fi
    ADB="adb"
fi

MESA_TAGS=(
    "libvulkan:*"
    "vulkan:*"
    "MESA:*"
    "turnip:*"
    "TU:*"
    "freedreno:*"
    "fd:*"
    "fd-FD:*"
    "Freedreno-VK:*"
    "kgsl:*"
    "kgsl-3d0:*"
    "adreno:*"
)

TAG_ARGS=()
for tag in "${MESA_TAGS[@]}"; do
    TAG_ARGS+=(-s "$tag")
done

echo "=== Mesa Turnip logcat capture ==="
echo "Output file: $LOG_FILE"
echo "Tags: ${MESA_TAGS[*]}"
echo ""

if [ "$DURATION" -gt 0 ]; then
    echo "Capturing for ${DURATION}s..."
    timeout "$DURATION" $ADB logcat -v threadtime "${TAG_ARGS[@]}" 2>&1 | tee "$LOG_FILE"
else
    echo "Capturing until Ctrl+C..."
    $ADB logcat -c 2>/dev/null || true
    $ADB logcat -v threadtime "${TAG_ARGS[@]}" 2>&1 | tee "$LOG_FILE"
fi
