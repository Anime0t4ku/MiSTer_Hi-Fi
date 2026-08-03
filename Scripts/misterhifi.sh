#!/bin/bash
BASE="/media/fat/Scripts/.config/MiSTerHiFi"
BIN="$BASE/mister_hifi"
SOCK="/tmp/misterhifi.sock"

mkdir -p "$BASE/cache" "$BASE/tmp" "$BASE/mnt"

if [ ! -x "$BIN" ]; then
  chmod +x "$BIN" 2>/dev/null
fi

if [ "$#" -gt 0 ] && [ -S "$SOCK" ]; then
  if "$BIN" --send "$@"; then
    exit 0
  fi
fi

printf '\033[?25l'
trap 'printf "\033[?25h"' EXIT

exec "$BIN" "$@"
