#!/bin/bash -e

green='\033[0;32m'
red='\033[0;31m'
yellow='\033[0;33m'
nocolor='\033[0m'

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
PACKAGES_FILE="$ROOT_DIR/packages.yml"

echo -e "${green}=== Winlator Assets Builder ===${nocolor}"

if [ ! -f "$PACKAGES_FILE" ]; then
    echo -e "${red}packages.yml not found${nocolor}"
    exit 1
fi

PACKAGES=$(python3 -c "
import yaml, sys
with open('$PACKAGES_FILE') as f:
    data = yaml.safe_load(f)
for name, cfg in data.items():
    if isinstance(cfg, dict) and cfg.get("output"):
        print(name)
")

if [ -z "$PACKAGES" ]; then
    echo -e "${red}No packages found in packages.yml${nocolor}"
    exit 1
fi

FAILED=""

for pkg in $PACKAGES; do
    echo ""
    echo -e "${yellow}--- Building $pkg ---${nocolor}"
    BUILD_SCRIPT="$ROOT_DIR/$pkg/build.sh"

    if [ ! -f "$BUILD_SCRIPT" ]; then
        echo -e "${yellow}No build.sh found for $pkg, skipping${nocolor}"
        continue
    fi

    if devbox run -- bash "$BUILD_SCRIPT"; then
        echo -e "${green}[OK] $pkg built successfully${nocolor}"
    else
        echo -e "${red}[FAIL] $pkg build failed${nocolor}"
        FAILED="$FAILED $pkg"
    fi
done

echo ""
if [ -z "$FAILED" ]; then
    echo -e "${green}All packages built successfully${nocolor}"
else
    echo -e "${red}Failed packages:$FAILED${nocolor}"
    exit 1
fi
