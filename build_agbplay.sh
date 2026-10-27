#!/bin/sh
# Builds agbplay's core and gba_player.cpp into third_party/agbplay-src/build-<target>/libagbcore.a
# usage: build_agbplay.sh arm|host     (arm: the ARM cross compiler of the build image)
set -eu
cd "$(dirname "$0")"
TARGET=${1:?arm or host}
./fetch_agbplay.sh
SRC=third_party/agbplay-src
OUT=$SRC/build-$TARGET
case "$TARGET" in
arm) CXX=${CXX_ARM:-arm-none-linux-gnueabihf-g++}; AR=${AR_ARM:-arm-none-linux-gnueabihf-ar}; EXTRA="-mfpu=neon -mfloat-abi=hard -mcpu=cortex-a9 -Wno-psabi" ;;
host) CXX=${CXX_HOST:-g++}; AR=${AR_HOST:-ar}; EXTRA="" ;;
*) echo "usage: $0 arm|host" >&2; exit 1 ;;
esac
command -v "$CXX" >/dev/null 2>&1 || { echo "Error: $CXX not found" >&2; exit 1; }
# This archive is rebuilt with one compiler, so GCC's historical ARM
# parameter-passing ABI notices are not actionable here (-Wno-psabi above).
# Downloaded fmt/json headers are system dependencies: keep their optimizer
# diagnostics out of this build while retaining warnings in agbplay's core
# and our wrapper. Errors in dependency headers are still reported.
mkdir -p "$OUT/obj"
rm -f "$OUT/obj"/*.o "$OUT/libagbcore.a"
# not needed for playback: the exporter (libsndfile), GSF files (zlib), the x86 SIMD resampler, the
# configuration/profile files and zip reading
for f in "$SRC"/core/*.cpp; do
    b=$(basename "$f" .cpp)
    case "$b" in SoundExporter|Gsf|ResamplerAVX2|FileReader|OS|ProfileManager|Settings) continue ;; esac
    "$CXX" -std=c++20 -O2 $EXTRA -fPIC -DFMT_HEADER_ONLY -I"$SRC/core" -isystem "$SRC/include" -c "$f" -o "$OUT/obj/$b.o"
done
"$CXX" -std=c++20 -O2 $EXTRA -fPIC -DFMT_HEADER_ONLY -I"$SRC/core" -isystem "$SRC/include" -Igba -c gba/gba_player.cpp -o "$OUT/obj/gba_player.o"
"$AR" rcs "$OUT/libagbcore.a" "$OUT/obj"/*.o
echo "built $OUT/libagbcore.a"
