#!/bin/sh
set -eu

cd "$(dirname "$0")"

./fetch_miniaudio.sh

go mod download

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

command -v cargo >/dev/null 2>&1 || {
    echo "Error: Rust/Cargo is required for M4A (AAC/ALAC) support." >&2
    echo "Install Rust with rustup, then run: rustup target add armv7-unknown-linux-gnueabihf" >&2
    exit 1
}
rustup target add armv7-unknown-linux-gnueabihf >/dev/null 2>&1 || true
(
    cd m4a_decoder
    CARGO_TARGET_ARMV7_UNKNOWN_LINUX_GNUEABIHF_LINKER="$CC" cargo build --release --target armv7-unknown-linux-gnueabihf
)

GOOS=linux \
GOARCH=arm \
GOARM=7 \
CGO_ENABLED=1 \
CC="$CC" \
CGO_LDFLAGS="${CGO_LDFLAGS:-} -latomic" \
go build -trimpath -ldflags="-s -w" -o Scripts/.config/MiSTerHiFi/mister_hifi .

echo "Built Scripts/.config/MiSTerHiFi/mister_hifi"
