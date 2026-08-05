#!/bin/sh
set -eu

cd "$(dirname "$0")"

VERSION="0.11.25"
URL="https://raw.githubusercontent.com/mackron/miniaudio/${VERSION}/miniaudio.h"
OUT="miniaudio.h"
TMP="${OUT}.tmp"

if [ -s "$OUT" ]; then
    echo "miniaudio ${VERSION} already present: $OUT"
    exit 0
fi

rm -f "$OUT" "$TMP"
echo "Fetching miniaudio ${VERSION}..."

if command -v curl >/dev/null 2>&1; then
    curl -fL --retry 3 --connect-timeout 15 "$URL" -o "$TMP"
elif command -v wget >/dev/null 2>&1; then
    wget -O "$TMP" "$URL"
else
    echo "Error: curl or wget is required to fetch miniaudio.h" >&2
    exit 1
fi

if [ ! -s "$TMP" ]; then
    echo "Error: downloaded miniaudio.h is empty" >&2
    rm -f "$TMP"
    exit 1
fi

if ! grep -q "#define miniaudio_h" "$TMP"; then
    echo "Error: downloaded file does not look like miniaudio.h" >&2
    rm -f "$TMP"
    exit 1
fi

mv "$TMP" "$OUT"
echo "Saved miniaudio ${VERSION} to $OUT"
