#!/bin/sh
# Fetch the sources of the GBA music engine and what it needs, at pinned versions:
#   agbplay (LGPL-3.0, https://github.com/ipatix/agbplay): plays the songs of GBA ROMs
#     that use Nintendo's MP2K sound engine. Only its core is used (no GUI, no export).
#   {fmt} and nlohmann/json: header-only libraries agbplay includes.
# Two patches, both so that fewer libraries are needed: agbplay takes sinc() from Boost for
# its lookup tables (replaced by the formula), and Rom.cpp reads zip files and GSF rips with
# libzip and zlib (replaced by reading a plain .gba file).
set -eu
cd "$(dirname "$0")"

AGBPLAY_COMMIT=0b87da48d2502da359e45718eec8566ac40fa9d7
FMT_VERSION=10.2.1
JSON_VERSION=v3.11.3
DEST=third_party/agbplay-src

if [ -f "$DEST/.ok-$AGBPLAY_COMMIT" ]; then exit 0; fi
command -v curl >/dev/null 2>&1 || { echo "Error: curl is required." >&2; exit 1; }

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT INT TERM
rm -rf "$DEST"
mkdir -p "$DEST/include/nlohmann"

curl -fL --retry 3 -o "$TMP/agbplay.tar.gz" "https://github.com/ipatix/agbplay/archive/$AGBPLAY_COMMIT.tar.gz"
tar -xzf "$TMP/agbplay.tar.gz" -C "$TMP"
cp -r "$TMP"/agbplay-*/src/agbplay "$DEST/core"
cp "$TMP"/agbplay-*/LICENSE "$DEST/LICENSE-agbplay.txt"

curl -fL --retry 3 -o "$TMP/fmt.tar.gz" "https://github.com/fmtlib/fmt/archive/refs/tags/$FMT_VERSION.tar.gz"
tar -xzf "$TMP/fmt.tar.gz" -C "$TMP"
cp -r "$TMP"/fmt-*/include/fmt "$DEST/include/fmt"
cp "$TMP"/fmt-*/LICENSE "$DEST/LICENSE-fmt.txt"

curl -fL --retry 3 -o "$DEST/include/nlohmann/json.hpp" "https://github.com/nlohmann/json/releases/download/$JSON_VERSION/json.hpp"

# the patch: no Boost
sed -i 's|#include <boost/math/special_functions/sinc.hpp>|#include <cmath>\nstatic inline double agb_sinc_pi(double x) { return x == 0.0 ? 1.0 : std::sin(x) / x; }|; s|boost::math::sinc_pi(|agb_sinc_pi(|g' "$DEST/core/Resampler.cpp"
grep -q 'boost[/:]' "$DEST/core/Resampler.cpp" && { echo "Error: Boost is still referenced in Resampler.cpp" >&2; exit 1; }

# Rom.cpp reads ROMs from zip files and GSF rips (libzip, zlib); only plain .gba files are needed here.
perl -0pi -e '
sub cut { my ($sig, $body) = @_; $s =~ s/\Q$sig\E.*?\n\}\n/$sig $body\n/s or die "Rom.cpp: $sig not found\n"; }
$s = do { local $/; $_ };
$s =~ s/#include <zip\.h>\n/#include <fstream>\n/;
cut("std::string Rom::GetROMCode() const", "{\n    return ReadString(0xAC, 4);\n}");
cut("bool Rom::LoadZip(const std::filesystem::path &filePath)", "{\n    (void)filePath;\n    return false;\n}");
cut("bool Rom::LoadGsflib(const std::filesystem::path &filePath)", "{\n    (void)filePath;\n    return false;\n}");
cut("bool Rom::LoadGsflib(FileReader &fileReader)", "{\n    (void)fileReader;\n    return false;\n}");
cut("bool Rom::LoadRaw(const std::filesystem::path &filePath)", <<"B");
{
    std::ifstream f(filePath, std::ios::binary | std::ios::ate);
    if (!f)
        return false;
    const size_t size = static_cast<size_t>(f.tellg());
    if (size <= 0x200)
        throw Xcept("ERROR: Attempting to load tiny ROM (<= 512 bytes). Incorrect or damaged file?");
    if (size > AGB_ROM_SIZE)
        throw Xcept("ERROR: Attempting to illegally large ROM (> 32 MiB). Incorrect or damaged file?");
    romContainer.resize(size);
    f.seekg(0);
    f.read(reinterpret_cast<char *>(romContainer.data()), static_cast<std::streamsize>(size));
    isGsf = false;
    return f.good();
}
B
die "Rom.cpp still depends on zip/GSF/FileReader\n" if $s =~ /zip\.h|Gsf::|FileReader::/;
$_ = $s;
' "$DEST/core/Rom.cpp"

# the AVX2 resamplers are x86 only: the plain ones are used
perl -0pi -e 's/#include "ResamplerAVX2\.hpp"\n//; s/[ \t]*if \(AVX2_SUPPORTED\)\n[ \t]*return std::make_unique<\w+AVX2>\(\);\n[ \t]*else\n//g; die "Resampler.cpp still uses AVX2\n" if /AVX2>/' "$DEST/core/Resampler.cpp"

# agbplay's generated version header
cat > "$DEST/core/Version.hpp" <<'H'
#pragma once
#define AGBPLAY_VERSION "embedded"
#define AGBPLAY_BUILD_YEAR "2026"
H
touch "$DEST/.ok-$AGBPLAY_COMMIT"
echo "agbplay $AGBPLAY_COMMIT, fmt $FMT_VERSION, json $JSON_VERSION ready in $DEST"
