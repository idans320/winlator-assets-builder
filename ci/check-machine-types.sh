#!/bin/bash
# ci/check-machine-types.sh — static PE machine-type gate.
#
# Asserts the machine type of every PE image in the built packages BEFORE any
# runtime test runs. This catches the class of bug where a component is built
# as plain ARM64 (0xAA64) and then silently fails to load inside an ARM64EC
# process (0xA641) or an ARM64X image (0xA64E).
#
# Rules:
#   fexcore  system32/libarm64ecfex.dll                 must be 0xA641
#            system32/libFEXCore.dll, libwow64fex.dll   must be 0xAA64
#            everything else in system32/               {0xAA64, 0xA641}
#   dxvk     system32/*.dll                             {0xA641, 0xA64E} — never 0xAA64
#            syswow64/*.dll                             0x14C
#   wine     lib/wine/aarch64-windows/ core DLLs       0xA64E (ARM64X)
#            lib/wine/aarch64-windows/ everything else {0xA64E, 0xAA64, 0xA641}
#            lib/wine/i386-windows/*.dll                0x14C
#   tests    load_dlls_x64.exe 0x8664, load_dlls_i386.exe 0x14C,
#            load_dlls_arm64.exe 0xAA64
#
# Usage:
#   ci/check-machine-types.sh [options]
#
#   --fexcore PATH   .wcp/.zip or an extracted directory
#                    (default: newest fexcore-*.wcp in the repo root)
#   --dxvk PATH      .wcp/.zip or an extracted directory
#                    (default: newest dxvk-*.wcp, then *-arm64ec-gplasync.wcp)
#   --wine PATH      .wcp/.zip or an extracted directory (optional)
#   --tests DIR      directory holding load_dlls_*.exe (optional)
#   --strict-missing fail when no artifact at all was found (guards a mistyped
#                    path or an empty artifact download)
#   --allow-legacy-dxvk-arm64
#                    TEMPORARY escape hatch: accept 0xAA64 in dxvk/system32.
#                    Prints a loud warning; do not use on main.
#   --verbose        print one line per PE file (wine has ~600)
#   --keep           keep extracted artifacts under ci/build/check
#
# Exit code: 0 when every check passed, 1 otherwise.
set -uo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=lib/pe-machine.sh
. "$ROOT_DIR/ci/lib/pe-machine.sh"

FEXCORE=""
DXVK=""
WINE=""
TESTS=""
STRICT_MISSING=0
ALLOW_LEGACY_DXVK=0
VERBOSE=0
KEEP=0

usage() {
    sed -n '2,45p' "$0" | sed 's/^# \{0,1\}//'
    exit 0
}

while [ $# -gt 0 ]; do
    case "$1" in
        --fexcore) FEXCORE="${2:?--fexcore needs a value}"; shift ;;
        --dxvk) DXVK="${2:?--dxvk needs a value}"; shift ;;
        --wine) WINE="${2:?--wine needs a value}"; shift ;;
        --tests) TESTS="${2:?--tests needs a value}"; shift ;;
        --strict-missing) STRICT_MISSING=1 ;;
        --allow-legacy-dxvk-arm64) ALLOW_LEGACY_DXVK=1 ;;
        --verbose) VERBOSE=1 ;;
        --keep) KEEP=1 ;;
        -h | --help) usage ;;
        *)
            echo "unknown argument: $1" >&2
            exit 2
            ;;
    esac
    shift
done

CHECKS=0
FAILURES=0

WORK_ROOT="$ROOT_DIR/ci/build/check"
if [ "$KEEP" -eq 1 ]; then
    mkdir -p "$WORK_ROOT"
    WORKDIR="$(mktemp -d "$WORK_ROOT/run.XXXXXX")"
else
    WORKDIR="$(mktemp -d)"
fi

cleanup() {
    if [ "$KEEP" -eq 1 ]; then
        echo "kept: $WORKDIR"
    else
        rm -rf "$WORKDIR"
    fi
}
trap cleanup EXIT

pass_row() {
    CHECKS=$((CHECKS + 1))
    printf '  %-4s %-62s %-10s %s\n' "OK" "$1" "$2" "${3:-}"
}

fail_row() {
    CHECKS=$((CHECKS + 1))
    FAILURES=$((FAILURES + 1))
    printf '  %-4s %-62s %-10s %s\n' "FAIL" "$1" "$2" "$3"
}

skip_row() {
    printf '  %-4s %-62s %s\n' "SKIP" "$1" "${2:-}"
}

note_row() {
    printf '  %-4s %-62s %s\n' "NOTE" "$1" "${2:-}"
}

section() {
    printf '\n== %s ==\n' "$1"
}

# newest_artifact <glob> — newest matching file in the repo root, or nothing.
newest_artifact() {
    local pattern="$1" match best=""
    for match in $pattern; do
        [ -f "$match" ] || continue
        if [ -z "$best" ] || [ "$match" -nt "$best" ]; then
            best="$match"
        fi
    done
    printf '%s\n' "$best"
}

# unpack <path> <name> — echoes a directory containing the package tree.
unpack() {
    local src="$1"
    local name="$2"
    local dest="$WORKDIR/$name"

    if [ -d "$src" ]; then
        printf '%s\n' "$src"
        return 0
    fi
    if [ ! -f "$src" ]; then
        return 1
    fi

    mkdir -p "$dest"
    case "$src" in
        *.zip)
            if ! command -v unzip > /dev/null 2>&1; then
                echo "unzip is required to read $src" >&2
                return 1
            fi
            unzip -q "$src" -d "$dest" || return 1
            ;;
        *)
            tar -xJf "$src" -C "$dest" 2> /dev/null ||
                tar -xJf "$src" -C "$dest" || return 1
            ;;
    esac
    printf '%s\n' "$dest"
}

is_pe() {
    [ "$(head -c 2 "$1" 2> /dev/null)" = "MZ" ]
}

# require_file <root> <relative path> <label> [description]
require_file() {
    local root="$1" rel="$2" label="$3"
    if [ -e "$root/$rel" ]; then
        pass_row "$label" "present" "${4:-}"
        return 0
    fi
    fail_row "$label" "missing" "${4:-}"
    return 1
}

# require_symlink <root> <relative path> <label> — the entry must exist and
# resolve (Wine's bin/ launchers are symlinks to bin/wine).
require_symlink() {
    local root="$1" rel="$2" label="$3" target
    if [ -L "$root/$rel" ] && [ -e "$root/$rel" ]; then
        target="$(readlink "$root/$rel")"
        pass_row "$label" "-> $target" ""
        return 0
    fi
    if [ -f "$root/$rel" ]; then
        pass_row "$label" "regular file" ""
        return 0
    fi
    fail_row "$label" "missing or dangling symlink" ""
    return 1
}

# check_pe <root> <relative path> <label> <allowed machine>...
check_pe() {
    local root="$1" rel="$2" label="$3"
    shift 3
    local file="$root/$rel" machine allowed norm_allowed

    if [ ! -f "$file" ]; then
        fail_row "$label" "missing" "expected $(printf '%s ' "$@")"
        return 1
    fi

    if ! machine="$(pe_machine "$file")"; then
        fail_row "$label" "unreadable" "$(pe_file_note "$file")"
        return 1
    fi

    for allowed in "$@"; do
        norm_allowed=$(printf '0x%04X' "$allowed")
        if [ "${machine^^}" = "${norm_allowed^^}" ]; then
            pass_row "$label" "$machine ($(pe_machine_name "$machine"))" "expected $norm_allowed"
            return 0
        fi
    done

    fail_row "$label" "$machine ($(pe_machine_name "$machine"))" \
        "expected $(printf '%s ' "$@")"
    return 1
}

# check_tree <dir> <label> <allowed machine>...
check_tree() {
    local dir="$1" label="$2"
    shift 2
    local -a allowed=()
    local total=0 bad=0 file machine ok a raw

    for raw in "$@"; do
        allowed+=("$(printf '0x%04X' "$raw")")
    done

    if [ ! -d "$dir" ]; then
        fail_row "$label" "missing directory" ""
        return 1
    fi

    while IFS= read -r file; do
        is_pe "$file" || continue
        total=$((total + 1))
        if ! machine="$(pe_machine "$file")"; then
            bad=$((bad + 1))
            fail_row "$label/$(basename "$file")" "unreadable" "$(pe_file_note "$file")"
            continue
        fi
        ok=0
        for a in "${allowed[@]}"; do
            if [ "${machine^^}" = "${a^^}" ]; then
                ok=1
                break
            fi
        done
        if [ "$ok" -eq 1 ]; then
            if [ "$VERBOSE" -eq 1 ]; then
                printf '  %-4s %-62s %-10s %s\n' "OK" \
                    "$label/$(basename "$file")" "$machine" ""
            fi
        else
            bad=$((bad + 1))
            fail_row "$label/$(basename "$file")" "$machine ($(pe_machine_name "$machine"))" \
                "expected one of: $(printf '%s ' "${allowed[@]}")"
        fi
    done < <(find "$dir" -maxdepth 1 -type f | sort)

    if [ "$total" -eq 0 ]; then
        fail_row "$label" "no PE files found" ""
        return 1
    fi
    if [ "$bad" -eq 0 ]; then
        pass_row "$label" "$total PE files" "allowed: $(printf '%s ' "${allowed[@]}")"
        return 0
    fi
    return 1
}

# --- discovery -------------------------------------------------------------

[ -n "$FEXCORE" ] || FEXCORE="$(newest_artifact "$ROOT_DIR/fexcore-*.wcp")"
[ -n "$DXVK" ] || DXVK="$(newest_artifact "$ROOT_DIR/dxvk-*.wcp")"
if [ -z "$DXVK" ]; then
    DXVK="$(newest_artifact "$ROOT_DIR/*-arm64ec-gplasync.wcp")"
fi
[ -n "$WINE" ] || WINE="$(newest_artifact "$ROOT_DIR/wine-arm64-*.wcp")"
if [ -z "$TESTS" ] && [ -d "$ROOT_DIR/ci/build/tests" ]; then
    TESTS="$ROOT_DIR/ci/build/tests"
fi

printf '== machine-type gate ==\n'
printf 'repo: %s\n' "$ROOT_DIR"
printf 'tool: %s\n' "$(_pe_readobj || echo 'python3 fallback')"

if [ "$STRICT_MISSING" -eq 1 ] && [ -z "$FEXCORE" ] && [ -z "$DXVK" ] && [ -z "$WINE" ]; then
    echo "error: --strict-missing was given but no artifact was found" >&2
    exit 1
fi

# --- fexcore ---------------------------------------------------------------

section "fexcore"
if [ -z "$FEXCORE" ]; then
    skip_row "fexcore artifact" "not found (fexcore-*.wcp)"
else
    printf 'artifact: %s\n' "$FEXCORE"
    if FEX_DIR="$(unpack "$FEXCORE" fexcore)"; then
        check_pe "$FEX_DIR" "system32/libarm64ecfex.dll" "fexcore/system32/libarm64ecfex.dll" 0xA641
        check_pe "$FEX_DIR" "system32/libFEXCore.dll" "fexcore/system32/libFEXCore.dll" 0xAA64
        check_pe "$FEX_DIR" "system32/libwow64fex.dll" "fexcore/system32/libwow64fex.dll" 0xAA64
        check_tree "$FEX_DIR/system32" "fexcore/system32" 0xAA64 0xA641
        [ -f "$FEX_DIR/profile.json" ] &&
            pass_row "fexcore/profile.json" "present" "" ||
            fail_row "fexcore/profile.json" "missing" "wcp layout"
        [ -f "$FEX_DIR/Config.json" ] &&
            pass_row "fexcore/Config.json" "present" "" ||
            fail_row "fexcore/Config.json" "missing" "wcp layout"
        # The ARM64EC DLL must not be parked in a system32 subdirectory: Wine's
        # loader resolves exactly C:\windows\system32\libarm64ecfex.dll.
        if [ -d "$FEX_DIR/system32" ] &&
            [ "$(find "$FEX_DIR/system32" -mindepth 1 -maxdepth 1 -type d | wc -l)" -ne 0 ]; then
            fail_row "fexcore/system32" "contains subdirectories" "DLLs must be flat"
        fi
    else
        fail_row "fexcore artifact" "cannot unpack" "$FEXCORE"
    fi
fi

# --- dxvk ------------------------------------------------------------------

section "dxvk"
if [ -z "$DXVK" ]; then
    skip_row "dxvk artifact" "not found (dxvk-*.wcp)"
else
    printf 'artifact: %s\n' "$DXVK"
    if DXVK_DIR="$(unpack "$DXVK" dxvk)"; then
        if [ "$ALLOW_LEGACY_DXVK" -eq 1 ]; then
            printf '  WARNING: --allow-legacy-dxvk-arm64 is set; plain ARM64 system32 DLLs will pass.\n'
            printf '           This is a transition escape hatch only.\n'
            check_tree "$DXVK_DIR/system32" "dxvk/system32" 0xAA64 0xA641 0xA64E
        else
            check_tree "$DXVK_DIR/system32" "dxvk/system32" 0xA641 0xA64E
        fi
        check_tree "$DXVK_DIR/syswow64" "dxvk/syswow64" 0x14C
        if [ -d "$DXVK_DIR/system32" ] &&
            ! is_pe "$DXVK_DIR/system32/d3d11.dll"; then
            fail_row "dxvk/system32/d3d11.dll" "missing" ""
        fi
        [ -f "$DXVK_DIR/profile.json" ] &&
            pass_row "dxvk/profile.json" "present" "" ||
            fail_row "dxvk/profile.json" "missing" "wcp layout"
        [ -f "$DXVK_DIR/Config.json" ] &&
            pass_row "dxvk/Config.json" "present" "" ||
            fail_row "dxvk/Config.json" "missing" "wcp layout"
    else
        fail_row "dxvk artifact" "cannot unpack" "$DXVK"
    fi
fi

# --- wine (optional) -------------------------------------------------------

section "wine"
if [ -z "$WINE" ]; then
    skip_row "wine artifact" "not found (wine-arm64-*.wcp); pass --wine to check it"
else
    printf 'artifact: %s\n' "$WINE"
    if WINE_DIR="$(unpack "$WINE" wine)"; then
        A64="$WINE_DIR/lib/wine/aarch64-windows"
        for core in ntdll.dll kernel32.dll user32.dll gdi32.dll advapi32.dll \
            ucrtbase.dll msvcrt.dll combase.dll ole32.dll shell32.dll; do
            check_pe "$A64" "$core" "wine/aarch64-windows/$core" 0xA64E
        done
        check_tree "$A64" "wine/aarch64-windows" 0xA64E 0xAA64 0xA641
        if [ -d "$WINE_DIR/lib/wine/i386-windows" ]; then
            check_tree "$WINE_DIR/lib/wine/i386-windows" "wine/i386-windows" 0x14C
        fi

        # Packaging: Winlator launches programs through bin/<name> symlinks.
        # Without bin/wineboot (and friends) prefix creation fails, so this is
        # a hard requirement, not a nicety.
        require_symlink "$WINE_DIR" "bin/wine" "wine/bin/wine"
        require_file "$WINE_DIR" "bin/wineserver" "wine/bin/wineserver"
        require_file "$WINE_DIR" "bin/wine-preloader" "wine/bin/wine-preloader"
        # Hard requirement: without these Winlator cannot create/repair a prefix.
        for launcher in wineboot winecfg; do
            require_symlink "$WINE_DIR" "bin/$launcher" "wine/bin/$launcher"
        done
        # Nice to have: the rest of the launchers users may invoke directly.
        for launcher in regedit msiexec wineconsole winedbg winefile winepath notepad; do
            if [ -e "$WINE_DIR/bin/$launcher" ]; then
                note_row "wine/bin/$launcher" "present"
            else
                note_row "wine/bin/$launcher" "absent (not required)"
            fi
        done
        require_file "$WINE_DIR" "lib/wine/aarch64-windows/wineboot.exe" \
            "wine/lib/wine/aarch64-windows/wineboot.exe"
        require_file "$WINE_DIR" "lib/wine/aarch64-windows/winecfg.exe" \
            "wine/lib/wine/aarch64-windows/winecfg.exe"
        require_file "$WINE_DIR" "prefixPack.txz" "wine/prefixPack.txz" \
            "(prefix creation on device)"

        if [ -f "$WINE_DIR/bin/wine" ]; then
            if readelf -l "$WINE_DIR/bin/wine" 2> /dev/null |
                grep -q 'Requesting program interpreter: /system/bin/linker64'; then
                printf '  %-4s %-62s %s\n' "NOTE" "bin/wine is an Android/bionic ELF" \
                    "runnable on device, not on a glibc runner (see ci/README.md)"
            else
                printf '  %-4s %-62s %s\n' "NOTE" "bin/wine is a glibc ELF" \
                    "can be used by ci/run-wine-tests.sh"
            fi
        fi
    else
        fail_row "wine artifact" "cannot unpack" "$WINE"
    fi
fi

# --- test programs ---------------------------------------------------------

section "test programs"
if [ -z "$TESTS" ]; then
    skip_row "test programs" "no directory (build with ci/build-test-programs.sh)"
elif [ ! -d "$TESTS" ]; then
    fail_row "test programs" "not a directory" "$TESTS"
else
    check_pe "$TESTS" "load_dlls_x64.exe" "tests/load_dlls_x64.exe" 0x8664
    check_pe "$TESTS" "load_dlls_i386.exe" "tests/load_dlls_i386.exe" 0x14C
    if [ -f "$TESTS/load_dlls_arm64.exe" ]; then
        check_pe "$TESTS" "load_dlls_arm64.exe" "tests/load_dlls_arm64.exe" 0xAA64
    fi
fi

# --- summary ---------------------------------------------------------------

printf '\n== summary ==\n'
printf 'checks: %d   passed: %d   failed: %d\n' \
    "$CHECKS" "$((CHECKS - FAILURES))" "$FAILURES"

if [ "$FAILURES" -ne 0 ]; then
    cat << 'EOF'

Gate failed. Common causes:
  - dxvk/system32 is 0xAA64 (plain ARM64): an x64 guest process running under
    FEX is an ARM64EC process and cannot map plain ARM64 DLLs. Build DXVK with
    the ARM64EC/ARM64X configuration.
  - fexcore/system32/libarm64ecfex.dll is not 0xA641: the ARM64EC FEX build
    either did not run, or llvm-dlltool emitted the wrong import library
    (see the dlltool sed in fexcore/build.sh).
  - wine aarch64-windows DLLs are not 0xA64E: Wine was configured without
    arm64ec in --enable-archs.
  - A test exe has the wrong machine type: ci/build-test-programs.sh picked a
    different toolchain than expected.
EOF
    exit 1
fi

echo "All machine-type checks passed."
exit 0
