#!/bin/bash
# build-all-and-zip.sh — build every package in packages.yml, then bundle the
# resulting artifacts into one archive.
#
# Usage:
#   ./build-all-and-zip.sh                     # build everything, then bundle
#   ./build-all-and-zip.sh --no-build          # bundle the artifacts already on disk
#   ./build-all-and-zip.sh --only mesa,dxvk    # build/bundle a subset
#   ./build-all-and-zip.sh --out dist/all.zip  # explicit archive path
#   ./build-all-and-zip.sh --strict            # missing artifact is an error
#
# Artifacts are resolved from each package's `output` pattern in packages.yml,
# with {version} treated as a wildcard and the newest match taking precedence.

set -uo pipefail

green='\033[0;32m'
red='\033[0;31m'
yellow='\033[0;33m'
nocolor='\033[0m'

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
PACKAGES_FILE="$ROOT_DIR/packages.yml"
DIST_DIR="$ROOT_DIR/dist"

DO_BUILD=1
STRICT=0
ONLY=""
OUT_FILE=""

usage() {
    sed -n '2,13p' "$0" | sed 's/^# \{0,1\}//'
    exit 0
}

while [ $# -gt 0 ]; do
    case "$1" in
        --no-build) DO_BUILD=0 ;;
        --strict)   STRICT=1 ;;
        --only)     ONLY="${2:-}"; shift ;;
        --only=*)   ONLY="${1#*=}" ;;
        --out)      OUT_FILE="${2:-}"; shift ;;
        --out=*)    OUT_FILE="${1#*=}" ;;
        -h|--help)  usage ;;
        *) echo -e "${red}Unknown argument: $1${nocolor}" >&2; exit 2 ;;
    esac
    shift
done

if [ ! -f "$PACKAGES_FILE" ]; then
    echo -e "${red}packages.yml not found${nocolor}"
    exit 1
fi

# name<TAB>output, in packages.yml order (winlator is last, so it can pick up
# the other components).
mapfile -t PACKAGES < <(python3 - "$PACKAGES_FILE" <<'PY'
import sys, yaml
data = yaml.safe_load(open(sys.argv[1]))
for name, cfg in data.items():
    if isinstance(cfg, dict) and cfg.get("output"):
        print(f"{name}\t{cfg.get('output', '')}")
PY
)

if [ "${#PACKAGES[@]}" -eq 0 ]; then
    echo -e "${red}No packages found in packages.yml${nocolor}"
    exit 1
fi

in_only_list() {
    [ -z "$ONLY" ] && return 0
    case ",$ONLY," in
        *",$1,"*) return 0 ;;
        *) return 1 ;;
    esac
}

# Per-package build arguments.
build_args_for() {
    case "$1" in
        wine) echo "--setup" ;;
        *)    echo "" ;;
    esac
}

# Resolve a packages.yml output pattern to the newest matching file.
# Echoes the path, or nothing if there is no match.
resolve_artifact() {
    local pattern="$1"
    local glob="${pattern//\{version\}/\*}"
    local -a matches=()
    local f best=""

    while IFS= read -r f; do
        [ -n "$f" ] && matches+=("$f")
    done < <(compgen -G "$ROOT_DIR/$glob" || true)

    for f in "${matches[@]}"; do
        [ -f "$f" ] || continue
        if [ -z "$best" ] || [ "$f" -nt "$best" ]; then
            best="$f"
        fi
    done

    if [ -n "$best" ] && [ "${#matches[@]}" -gt 1 ]; then
        echo -e "${yellow}  note: ${#matches[@]} files match $glob; using newest: $(basename "$best")${nocolor}" >&2
    fi

    printf '%s\n' "$best"
}

echo -e "${green}=== Winlator Assets Build + Bundle ===${nocolor}"

FAILED=""
BUILT=""

for entry in "${PACKAGES[@]}"; do
    pkg="${entry%%$'\t'*}"
    output="${entry#*$'\t'}"

    if ! in_only_list "$pkg"; then
        continue
    fi

    echo ""
    echo -e "${yellow}--- $pkg ---${nocolor}"

    if [ "$DO_BUILD" -eq 1 ]; then
        BUILD_SCRIPT="$ROOT_DIR/$pkg/build.sh"
        if [ ! -f "$BUILD_SCRIPT" ]; then
            echo -e "${yellow}No build.sh for $pkg, skipping build${nocolor}"
        else
            # shellcheck disable=SC2046
            if devbox run -- bash "$BUILD_SCRIPT" $(build_args_for "$pkg"); then
                echo -e "${green}[OK] $pkg${nocolor}"
                BUILT="$BUILT $pkg"
            else
                echo -e "${red}[FAIL] $pkg build failed${nocolor}"
                FAILED="$FAILED $pkg"
                continue
            fi
        fi
    fi
done

# --- Collect ---------------------------------------------------------------
STAMP="$(date +%Y%m%d-%H%M%S)"
STAGE_NAME="winlator-assets-$STAMP"
STAGE_DIR="$DIST_DIR/$STAGE_NAME"
mkdir -p "$STAGE_DIR"

declare -a BUNDLED=()
declare -A RESOLVED=()
MISSING=""

for entry in "${PACKAGES[@]}"; do
    pkg="${entry%%$'\t'*}"
    output="${entry#*$'\t'}"

    if ! in_only_list "$pkg"; then
        continue
    fi
    # A package that failed to build contributes nothing.
    case " $FAILED " in *" $pkg "*) continue ;; esac

    artifact="$(resolve_artifact "$output")"
    if [ -z "$artifact" ]; then
        echo -e "${yellow}no artifact for $pkg (pattern: $output)${nocolor}"
        MISSING="$MISSING $pkg"
        continue
    fi

    cp -f "$artifact" "$STAGE_DIR/"
    RESOLVED[$pkg]="$artifact"
    BUNDLED+=("$(basename "$artifact")")
    echo -e "${green}+ $(basename "$artifact")${nocolor} ($(du -h "$artifact" | cut -f1))"
done

if [ "${#BUNDLED[@]}" -eq 0 ]; then
    echo -e "${red}Nothing to bundle${nocolor}"
    rm -rf "$STAGE_DIR"
    exit 1
fi

# --- Manifest --------------------------------------------------------------
(
    cd "$STAGE_DIR" || exit 1
    sha256sum -- * > SHA256SUMS
    {
        echo "bundle: $STAGE_NAME"
        echo "created: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
        echo
        for entry in "${PACKAGES[@]}"; do
            pkg="${entry%%$'\t'*}"
            output="${entry#*$'\t'}"
            in_only_list "$pkg" || continue
            case " $FAILED " in *" $pkg "*) continue ;; esac
            artifact="${RESOLVED[$pkg]:-}"
            [ -n "$artifact" ] || continue
            printf '%-12s %-45s %s\n' "$pkg" "$(basename "$artifact")" "$(stat -c%s "$artifact")"
        done
    } > manifest.txt
)

# --- Archive ---------------------------------------------------------------
[ -n "$OUT_FILE" ] || OUT_FILE="$DIST_DIR/$STAGE_NAME.zip"
mkdir -p "$(dirname "$OUT_FILE")"
rm -f "$OUT_FILE"

echo ""
echo -e "${yellow}--- bundling ---${nocolor}"
# Store, don't deflate: the artifacts are already compressed archives.
python3 - "$DIST_DIR" "$OUT_FILE" "$STAGE_NAME" <<'PY'
import os
import sys
import zipfile

dist, out, stage = sys.argv[1], sys.argv[2], sys.argv[3]
root = os.path.join(dist, stage)

with zipfile.ZipFile(out, "w", zipfile.ZIP_STORED) as zf:
    for dirpath, _, files in os.walk(root):
        for name in sorted(files):
            path = os.path.join(dirpath, name)
            zf.write(path, os.path.relpath(path, dist))
PY

echo ""
echo -e "${green}Bundle: $OUT_FILE${nocolor}"
ls -lh "$OUT_FILE"
echo -e "${green}Contents:${nocolor}"
printf '  %s\n' "${BUNDLED[@]}"

if [ -n "$FAILED" ]; then
    echo -e "${red}Build failures:$FAILED${nocolor}"
    exit 1
fi

if [ -n "$MISSING" ]; then
    echo -e "${yellow}Missing artifacts:$MISSING${nocolor}"
    if [ "$STRICT" -eq 1 ]; then
        exit 1
    fi
fi

echo -e "${green}Done${nocolor}"
