#!/bin/bash
set -euo pipefail

PROBE="${1:-probes/probe_draw}"
TRACE_DIR="output/traces"
DEVICE_PATH="/data/local/tmp"

echo "=== Trace Capture: $PROBE ==="

PROBE_NAME=$(basename "$PROBE")
TS=$(date +%Y-%m-%d_%H%M%S)

adb push "$PROBE" "$DEVICE_PATH/$PROBE_NAME"
adb shell chmod +x "$DEVICE_PATH/$PROBE_NAME"

TRACE_LOG="$DEVICE_PATH/trace_${TS}.log"
DMESG_LOG="$DEVICE_PATH/dmesg_${TS}.log"

echo "Running on device with TU_DEBUG=trace,bo ..."
adb shell "TU_DEBUG=trace,bo TU_DEBUG_EXTRA=1 $DEVICE_PATH/$PROBE_NAME > $TRACE_LOG 2>&1; echo EXIT:\$?" 

echo "Pulling trace..."
mkdir -p "$TRACE_DIR"
adb pull "$TRACE_LOG" "$TRACE_DIR/trace_${TS}.log"
adb shell dmesg > "$TRACE_DIR/dmesg_${TS}.log"

echo "Cleaning up device..."
adb shell rm "$DEVICE_PATH/$PROBE_NAME" "$TRACE_LOG"

echo "Done. Trace saved to $TRACE_DIR/trace_${TS}.log"
grep -c '\[TU_PKT4\]' "$TRACE_DIR/trace_${TS}.log" 2>/dev/null && echo "PKT4 writes found" || echo "No PKT4 writes (check TU_DEBUG build)"
