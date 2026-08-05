#!/bin/sh
set -eu

cd "$(dirname "$0")"

VERSION="0.11.25"
BASE_URL="https://raw.githubusercontent.com/mackron/miniaudio/${VERSION}"

fetch_file() {
    url="$1"
    out="$2"
    check="$3"
    tmp="${out}.tmp"

    if [ -s "$out" ] && grep -q "$check" "$out"; then
        echo "Already present: $out"
        return
    fi

    rm -f "$out" "$tmp"
    echo "Fetching $out..."
    if command -v curl >/dev/null 2>&1; then
        curl -fL --retry 3 --connect-timeout 15 "$url" -o "$tmp"
    elif command -v wget >/dev/null 2>&1; then
        wget -O "$tmp" "$url"
    else
        echo "Error: curl or wget is required" >&2
        exit 1
    fi

    if [ ! -s "$tmp" ] || ! grep -q "$check" "$tmp"; then
        echo "Error: downloaded $out is invalid" >&2
        rm -f "$tmp"
        exit 1
    fi
    mv "$tmp" "$out"
}

fetch_file "$BASE_URL/miniaudio.h" "miniaudio.h" "#define miniaudio_h"
rm -f stb_vorbis.c
fetch_file "$BASE_URL/extras/stb_vorbis.c" "stb_vorbis.h" "STB_VORBIS_INCLUDE_STB_VORBIS_H"

echo "miniaudio ${VERSION} + stb_vorbis ready"
