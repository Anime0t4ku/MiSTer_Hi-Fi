#!/bin/sh
set -eu

cd "$(dirname "$0")"

./fetch_miniaudio.sh

TOOLCHAIN="/opt/gcc-arm-10.2-2020.11-x86_64-arm-none-linux-gnueabihf/bin"
if [ -d "$TOOLCHAIN" ]; then
    PATH="$TOOLCHAIN:$PATH"
    export PATH
fi

: "${CC:=arm-none-linux-gnueabihf-gcc}"
command -v "$CC" >/dev/null 2>&1 || {
    echo "Error: ARM cross compiler not found: $CC" >&2
    exit 1
}
command -v go >/dev/null 2>&1 || {
    echo "Error: Go is not installed or not in PATH" >&2
    exit 1
}

mkdir -p Scripts/.config/MiSTerHiFi

GOOS=linux \
GOARCH=arm \
GOARM=7 \
CGO_ENABLED=1 \
CC="$CC" \
CGO_LDFLAGS="${CGO_LDFLAGS:-} -latomic" \
go build -trimpath -ldflags="-s -w" -o Scripts/.config/MiSTerHiFi/mister_hifi .

echo "Built Scripts/.config/MiSTerHiFi/mister_hifi"
