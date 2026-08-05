#!/bin/bash
VERSION="1.2.0"
BASE="/media/fat/Scripts/.config/MiSTerHiFi"
BIN="$BASE/mister_hifi"
SOCK="/tmp/misterhifi.sock"

if [ "$1" = "--version" ] || [ "$1" = "-v" ]; then
  echo "MiSTer Hi-Fi v$VERSION"
  exit 0
fi

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
