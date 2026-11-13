#!/bin/bash
# ci/lib/pe-machine.sh — shared PE machine-type helpers.
#
# Sourced by ci/check-machine-types.sh and ci/run-wine-tests.sh. Not executable
# on its own.
#
# pe_machine <file>        -> prints 0xNNNN (uppercase hex) or fails
# pe_machine_name <hex>    -> prints a human name
#
# Detection order:
#   1. llvm-readobj (any versioned name), authoritative
#   2. llvm-objdump --private-headers
#   3. python3 (reads e_machine directly, no toolchain needed)
#   4. `file` — WARNING: file(1) reports ARM64X as "ARM64" and ARM64EC as
#      "x86-64", so it is only used to name the machine, never to decide a
#      pass/fail. When only `file` is available pe_machine fails.

_pe_readobj() {
    local candidate
    for candidate in llvm-readobj llvm-readobj-21 llvm-readobj-20 \
        llvm-readobj-19 llvm-readobj-18 llvm-readobj-17 llvm-readobj-16 \
        llvm-readobj-15 llvm-readobj-14; do
        if command -v "$candidate" > /dev/null 2>&1; then
            printf '%s\n' "$candidate"
            return 0
        fi
    done
    return 1
}

_pe_objdump() {
    local candidate
    for candidate in llvm-objdump llvm-objdump-21 llvm-objdump-20 \
        llvm-objdump-19 llvm-objdump-18 llvm-objdump-17 llvm-objdump-16 \
        llvm-objdump-15 llvm-objdump-14; do
        if command -v "$candidate" > /dev/null 2>&1; then
            printf '%s\n' "$candidate"
            return 0
        fi
    done
    return 1
}

pe_machine() {
    local file="$1" tool raw
    [ -f "$file" ] || return 1

    if tool="$(_pe_readobj)"; then
        raw=$("$tool" --file-headers "$file" 2> /dev/null |
            sed -n 's/^[[:space:]]*Machine:.*(\(0x[0-9A-Fa-f][0-9A-Fa-f]*\)).*/\1/p' |
            head -1)
        if [ -n "$raw" ]; then
            printf '0x%04X\n' "$raw"
            return 0
        fi
    fi

    if tool="$(_pe_objdump)"; then
        raw=$("$tool" -p "$file" 2> /dev/null |
            sed -n 's/^[[:space:]]*[Mm]achine:[[:space:]]*\(IMAGE_FILE_MACHINE_[A-Z0-9_]*\).*/\1/p' |
            head -1)
        case "$raw" in
            IMAGE_FILE_MACHINE_I386) printf '0x014C\n'; return 0 ;;
            IMAGE_FILE_MACHINE_AMD64) printf '0x8664\n'; return 0 ;;
            IMAGE_FILE_MACHINE_ARM64) printf '0xAA64\n'; return 0 ;;
            IMAGE_FILE_MACHINE_ARM64EC) printf '0xA641\n'; return 0 ;;
            IMAGE_FILE_MACHINE_ARM64X) printf '0xA64E\n'; return 0 ;;
        esac
    fi

    if command -v python3 > /dev/null 2>&1; then
        raw=$(python3 - "$file" << 'PY' 2> /dev/null
import struct
import sys

with open(sys.argv[1], "rb") as fh:
    dos = fh.read(0x40)
    if len(dos) < 0x40 or dos[:2] != b"MZ":
        sys.exit(1)
    offset = struct.unpack_from("<I", dos, 0x3C)[0]
    fh.seek(offset)
    header = fh.read(6)
    if len(header) < 6 or header[:4] != b"PE\0\0":
        sys.exit(1)
    print("0x%04X" % struct.unpack_from("<H", header, 4)[0])
PY
        )
        if [ -n "$raw" ]; then
            printf '%s\n' "$raw"
            return 0
        fi
    fi

    return 1
}

pe_machine_name() {
    case "${1^^}" in
        0X014C | 0X14C) echo "i386" ;;
        0X8664) echo "x86_64" ;;
        0XAA64) echo "arm64" ;;
        0XA641) echo "arm64ec" ;;
        0XA64E) echo "arm64x" ;;
        *) echo "unknown" ;;
    esac
}

# pe_file_note <file> — last-resort human description, for log context only.
pe_file_note() {
    if command -v file > /dev/null 2>&1; then
        file -b "$1" 2> /dev/null
    else
        echo "(file not available)"
    fi
}
