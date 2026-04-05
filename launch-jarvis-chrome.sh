#!/usr/bin/env bash
set -euo pipefail

PROFILE_DIR="${JARVIS_CHROME_PROFILE:-$HOME/.local/share/jarvis-chrome}"
DOWNLOAD_DIR="${JARVIS_CHROME_DOWNLOADS:-$PROFILE_DIR/downloads}"
DEBUG_HOST="127.0.0.1"
DEBUG_PORT="${JARVIS_CHROME_DEBUG_PORT:-9222}"
HOMEPAGE="${JARVIS_CHROME_HOMEPAGE:-about:blank}"

mkdir -p "$PROFILE_DIR" "$DOWNLOAD_DIR"

find_browser() {
  local candidates=(
    chromium
    google-chrome-stable
    google-chrome
    chromium-browser
  )

  local c
  for c in "${candidates[@]}"; do
    if command -v "$c" >/dev/null 2>&1; then
      printf '%s\n' "$c"
      return 0
    fi
  done

  echo "No supported browser found. Install chromium or chrome." >&2
  return 1
}

BROWSER="$(find_browser)"

exec "$BROWSER" \
  --ozone-platform=wayland \
  --enable-features=UseOzonePlatform \
  --user-data-dir="$PROFILE_DIR" \
  --remote-debugging-address="$DEBUG_HOST" \
  --remote-debugging-port="$DEBUG_PORT" \
  --class=JarvisChrome \
  --no-first-run \
  --no-default-browser-check \
  --disable-sync \
  --disable-features=MediaRouter \
  --homepage="$HOMEPAGE" \
  "$HOMEPAGE"
