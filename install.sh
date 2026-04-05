#!/usr/bin/env bash
set -euo pipefail

SRC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"
UNIT_DIR="${UNIT_DIR:-$HOME/.config/systemd/user}"
CONFIG_DIR="${CONFIG_DIR:-$HOME/.config}"
PROFILE_DIR="${JARVIS_CHROME_PROFILE:-$HOME/.local/share/jarvis-chrome}"
PROXY_ENV_FILE="${PROXY_ENV_FILE:-$CONFIG_DIR/jarvis-browser-proxy.env}"
PROXY_ADDR="${JARVIS_BROWSER_PROXY_ADDR:-127.0.0.1:8787}"
CHROME_BASE_URL="${JARVIS_CHROME_BASE_URL:-http://127.0.0.1:9222}"
BROWSER_COMMAND="${JARVIS_BROWSER_COMMAND:-$BIN_DIR/jarvis-browser}"

log() {
  printf '%s\n' "$*"
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

generate_token() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 24
  else
    od -An -N24 -tx1 /dev/urandom | tr -d ' \n'
    printf '\n'
  fi
}

mkdir -p "$BIN_DIR" "$UNIT_DIR" "$CONFIG_DIR" "$PROFILE_DIR"

require_cmd install
require_cmd systemctl

install -m 0755 "$SRC_DIR/jarvis-browser" "$BIN_DIR/jarvis-browser"
install -m 0755 "$SRC_DIR/launch-jarvis-chrome.sh" "$BIN_DIR/launch-jarvis-chrome.sh"
install -m 0644 "$SRC_DIR/jarvis-chrome.service" "$UNIT_DIR/jarvis-chrome.service"
install -m 0644 "$SRC_DIR/jarvis-browser-proxy.service" "$UNIT_DIR/jarvis-browser-proxy.service"

if command -v go >/dev/null 2>&1; then
  log "Building jarvis-browser-proxy"
  (
    cd "$SRC_DIR"
    go build -o "$BIN_DIR/jarvis-browser-proxy" ./cmd/jarvis-browser-proxy
  )
else
  log "Go not found; skipping proxy binary build"
  log "Install Go, then run: cd $SRC_DIR && go build -o $BIN_DIR/jarvis-browser-proxy ./cmd/jarvis-browser-proxy"
fi

if [[ ! -f "$PROXY_ENV_FILE" ]]; then
  TOKEN="$(generate_token)"
  cat > "$PROXY_ENV_FILE" <<EOF
JARVIS_BROWSER_PROXY_TOKEN=$TOKEN
JARVIS_BROWSER_PROXY_ADDR=$PROXY_ADDR
JARVIS_BROWSER_COMMAND=$BROWSER_COMMAND
JARVIS_CHROME_BASE_URL=$CHROME_BASE_URL
EOF
  chmod 600 "$PROXY_ENV_FILE"
  log "Created $PROXY_ENV_FILE"
else
  log "Keeping existing $PROXY_ENV_FILE"
fi

systemctl --user daemon-reload

cat <<EOF
Installed:
  $BIN_DIR/jarvis-browser
  $BIN_DIR/launch-jarvis-chrome.sh
  $UNIT_DIR/jarvis-chrome.service
  $UNIT_DIR/jarvis-browser-proxy.service
EOF

if [[ -x "$BIN_DIR/jarvis-browser-proxy" ]]; then
  cat <<EOF
  $BIN_DIR/jarvis-browser-proxy
EOF
fi

cat <<EOF

Created/used config:
  $PROXY_ENV_FILE

One-time, in your logged-in desktop session:

systemctl --user import-environment \
  WAYLAND_DISPLAY XDG_RUNTIME_DIR DISPLAY \
  DBUS_SESSION_BUS_ADDRESS HYPRLAND_INSTANCE_SIGNATURE

dbus-update-activation-environment --systemd \
  WAYLAND_DISPLAY XDG_RUNTIME_DIR DISPLAY \
  DBUS_SESSION_BUS_ADDRESS HYPRLAND_INSTANCE_SIGNATURE

Then test locally:
  jarvis-browser start
  jarvis-browser status

If the proxy binary was built, start it with:
  systemctl --user start jarvis-browser-proxy
  systemctl --user status jarvis-browser-proxy --no-pager

Show the token:
  sed -n 's/^JARVIS_BROWSER_PROXY_TOKEN=//p' $PROXY_ENV_FILE

Recommended next step:
  copy the handlers into your existing host HTTP service,
  or reverse-proxy this local service behind your existing auth.
EOF
