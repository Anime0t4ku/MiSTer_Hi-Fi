#!/bin/bash
VERSION="1.9.1"
BASE="/media/fat/Scripts/.config/MiSTerHiFi"
BIN="$BASE/mister_hifi"
SOCK="/tmp/misterhifi.sock"

SAM_SCRIPT="/media/fat/Scripts/MiSTer_SAM_on.sh"
BGM_SCRIPT="/media/fat/Scripts/bgm.sh"
BGM_SOCK="/tmp/bgm.sock"

SAM_WAS_ENABLED=0
BGM_WAS_RUNNING=0
SERVICES_PREPARED=0

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

sam_autoplay_running() {
  ps 2>/dev/null | grep -q '[M]iSTer_SAM_MCP.py'
}

prepare_background_services() {
  # SAM's autoplay monitor can treat MiSTer as idle while this native app is
  # running over the menu core. Disable it only when it was actually active.
  if [ -f "$SAM_SCRIPT" ] && sam_autoplay_running; then
    SAM_WAS_ENABLED=1
    "$SAM_SCRIPT" disable >/dev/null 2>&1 || true
  fi

  # BGM keeps playing because the menu core remains loaded underneath the app.
  # Stop the service only if it was already running, then restore it on exit.
  if [ -f "$BGM_SCRIPT" ] && [ -S "$BGM_SOCK" ]; then
    BGM_WAS_RUNNING=1
    "$BGM_SCRIPT" stop >/dev/null 2>&1 || true
  fi

  SERVICES_PREPARED=1
}

restore_background_services() {
  [ "$SERVICES_PREPARED" = "1" ] || return
  SERVICES_PREPARED=0

  if [ "$SAM_WAS_ENABLED" = "1" ] && [ -f "$SAM_SCRIPT" ]; then
    "$SAM_SCRIPT" enable >/dev/null 2>&1 || true
  fi

  if [ "$BGM_WAS_RUNNING" = "1" ] && [ -f "$BGM_SCRIPT" ]; then
    "$BGM_SCRIPT" exec >/dev/null 2>&1 &
  fi
}

cleanup() {
  restore_background_services
  printf '\033[?25h'
}

printf '\033[?25l'
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

prepare_background_services

"$BIN" "$@"
exit $?
