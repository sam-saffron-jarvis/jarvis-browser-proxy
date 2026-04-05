#!/usr/bin/env bash
set -euo pipefail

PROFILE_DIR="${JARVIS_CHROME_PROFILE:-$HOME/.local/share/jarvis-chrome}"
DOWNLOAD_DIR="${JARVIS_CHROME_DOWNLOADS:-$PROFILE_DIR/downloads}"
DEBUG_HOST="127.0.0.1"
DEBUG_PORT="${JARVIS_CHROME_DEBUG_PORT:-9222}"
HOMEPAGE="${JARVIS_CHROME_HOMEPAGE:-about:blank}"
WORKSPACE="${JARVIS_CHROME_WORKSPACE:-9}"

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

move_to_workspace() {
  local workspace="$1"

  if [[ -z "$workspace" ]]; then
    return 0
  fi

  if ! command -v hyprctl >/dev/null 2>&1; then
    return 0
  fi

  (
    local i
    for ((i=0; i<40; i++)); do
      hyprctl dispatch movetoworkspacesilent "$workspace,class:^(JarvisChrome)$" >/dev/null 2>&1 || true
      sleep 0.25
    done
  ) &
}

BROWSER="$(find_browser)"

browser_args=(
  --ozone-platform=wayland
  --enable-features=UseOzonePlatform
  --user-data-dir="$PROFILE_DIR"
  --remote-debugging-address="$DEBUG_HOST"
  --remote-debugging-port="$DEBUG_PORT"
  --class=JarvisChrome
  --no-first-run
  --no-default-browser-check
  --disable-sync
  --disable-features=MediaRouter
  --homepage="$HOMEPAGE"
  "$HOMEPAGE"
)

"$BROWSER" "${browser_args[@]}" &
browser_pid=$!

move_to_workspace "$WORKSPACE"

wait "$browser_pid"
