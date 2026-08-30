#!/usr/bin/env bash
# scripts/subset-font.sh — Locally rebuild the Unifont OTF subset using font_ranges.json.
#
# Usage:
#   scripts/subset-font.sh [full_font.otf] [output.otf]
#
# Defaults:
#   full_font.otf: internal/ui/assets/unifont-full.otf (downloaded if missing)
#   output.otf:    internal/ui/assets/unifont.otf

set -euo pipefail
cd "$(dirname "$0")/.."

FULL_FONT="${1:-internal/ui/assets/unifont-full.otf}"
OUT_FONT="${2:-internal/ui/assets/unifont.otf}"
RANGES_JSON="internal/ui/font_ranges.json"
FONT_URL="https://unifoundry.com/pub/unifont/unifont-17.0.05/font-builds/unifont-17.0.05.otf"

if [ ! -f "$FULL_FONT" ]; then
    echo "=== Downloading $FONT_URL ==="
    mkdir -p "$(dirname "$FULL_FONT")"
    curl -fL -o "$FULL_FONT" "$FONT_URL"
fi

UNICODES=$(python3 -c "import json; print(','.join(json.load(open('$RANGES_JSON'))))")

echo "=== Subsetting with ranges: $UNICODES ==="
mkdir -p "$(dirname "$OUT_FONT")"
python3 -m fontTools.subset "$FULL_FONT" \
    --unicodes="$UNICODES" \
    --no-subset-tables+=OS/2 \
    --no-prune-unicode-ranges \
    --output-file="$OUT_FONT"

echo "=== Built $OUT_FONT ==="
