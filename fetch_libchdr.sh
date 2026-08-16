#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"
DEST="$ROOT/third_party/libchdr"
VERSION="v0.3.0"
URL="https://github.com/rtissera/libchdr/archive/refs/tags/${VERSION}.tar.gz"

if [ -f "$DEST/build-arm/libchdr_bundle.o" ] && [ -f "$DEST/include/libchdr/chd.h" ]; then
    exit 0
fi

command -v curl >/dev/null 2>&1 || { echo "Error: curl is required to fetch libchdr." >&2; exit 1; }
command -v cmake >/dev/null 2>&1 || { echo "Error: cmake is required to build libchdr." >&2; exit 1; }
: "${CC:=arm-none-linux-gnueabihf-gcc}"
CC_PATH="$(command -v "$CC" 2>/dev/null || true)"
[ -n "$CC_PATH" ] || { echo "Error: ARM cross compiler not found: $CC" >&2; exit 1; }

# CMake's static-library rules require an archiver and ranlib.  Resolve both
# to absolute paths so cross builds do not accidentally fall back to the host
# tools (or emit a bare command that CMake cannot execute under WSL).
if [ -n "${AR:-}" ]; then
    AR_PATH="$(command -v "$AR" 2>/dev/null || true)"
else
    AR_PATH="$(command -v arm-none-linux-gnueabihf-gcc-ar 2>/dev/null || command -v arm-none-linux-gnueabihf-ar 2>/dev/null || true)"
fi
if [ -n "${RANLIB:-}" ]; then
    RANLIB_PATH="$(command -v "$RANLIB" 2>/dev/null || true)"
else
    RANLIB_PATH="$(command -v arm-none-linux-gnueabihf-gcc-ranlib 2>/dev/null || command -v arm-none-linux-gnueabihf-ranlib 2>/dev/null || true)"
fi
[ -n "$AR_PATH" ] || { echo "Error: ARM archiver not found (gcc-ar/ar)." >&2; exit 1; }
[ -n "$RANLIB_PATH" ] || { echo "Error: ARM ranlib not found (gcc-ranlib/ranlib)." >&2; exit 1; }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT INT TERM
curl -L --fail --retry 3 "$URL" -o "$TMP/libchdr.tar.gz"
tar -xzf "$TMP/libchdr.tar.gz" -C "$TMP"
SRC="$(find "$TMP" -maxdepth 1 -type d -name 'libchdr-*' | head -n 1)"
[ -n "$SRC" ] || { echo "Error: unable to unpack libchdr." >&2; exit 1; }
rm -rf "$DEST"
mkdir -p "$(dirname "$DEST")"
cp -R "$SRC" "$DEST"
mkdir -p "$DEST/build-arm"
cmake -S "$DEST" -B "$DEST/build-arm" \
    -DCMAKE_SYSTEM_NAME=Linux \
    -DCMAKE_SYSTEM_PROCESSOR=arm \
    -DCMAKE_C_COMPILER="$CC_PATH" \
    -DCMAKE_AR="$AR_PATH" \
    -DCMAKE_RANLIB="$RANLIB_PATH" \
    -DBUILD_SHARED_LIBS=OFF \
    -DCHDR_WANT_RAW_DATA_SECTOR=ON \
    -DCHDR_WANT_SUBCODE=ON \
    -DCHDR_VERIFY_BLOCK_CRC=ON \
    -DCMAKE_BUILD_TYPE=Release
cmake --build "$DEST/build-arm" --target chdr-static -j2

ARCHIVES="$(find "$DEST/build-arm" -type f -name '*.a' -print)"
[ -n "$ARCHIVES" ] || { echo "Error: libchdr static libraries were not produced." >&2; exit 1; }
"$CC" -r -nostdlib -Wl,--whole-archive $ARCHIVES -Wl,--no-whole-archive -o "$DEST/build-arm/libchdr_bundle.o"

echo "Prepared libchdr ${VERSION} for ARMv7"
