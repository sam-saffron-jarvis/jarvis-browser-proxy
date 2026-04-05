#!/usr/bin/env bash
set -euo pipefail

SRC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"
UNIT_DIR="${UNIT_DIR:-$HOME/.config/systemd/user}"
CONFIG_DIR="${CONFIG_DIR:-$HOME/.config}"
STATE_DIR="${STATE_DIR:-$HOME/.local/state/jarvis-browser-proxy}"
PROFILE_DIR="${JARVIS_CHROME_PROFILE:-$HOME/.local/share/jarvis-chrome}"
DOWNLOADS_DIR="${JARVIS_CHROME_DOWNLOADS:-$PROFILE_DIR/downloads}"
PROXY_ENV_FILE="${PROXY_ENV_FILE:-$CONFIG_DIR/jarvis-browser-proxy.env}"
PROXY_ADDR="${JARVIS_BROWSER_PROXY_ADDR:-0.0.0.0:8787}"
CHROME_BASE_URL="${JARVIS_CHROME_BASE_URL:-http://127.0.0.1:9222}"
WORKSPACE="${JARVIS_CHROME_WORKSPACE:-9}"
HOMEPAGE="${JARVIS_CHROME_HOMEPAGE:-about:blank}"
DEBUG_HOST="${JARVIS_CHROME_DEBUG_HOST:-127.0.0.1}"
DEBUG_PORT="${JARVIS_CHROME_DEBUG_PORT:-9222}"
STARTUP_WAIT="${JARVIS_CHROME_STARTUP_WAIT:-15s}"
STOP_WAIT="${JARVIS_CHROME_STOP_WAIT:-10s}"
BROWSER_BINARY="${JARVIS_CHROME_BROWSER:-}"
HYPRLAND_CONFIG="${HYPRLAND_CONFIG:-$HOME/.config/hypr/hyprland.conf}"
HYPRLAND_BEGIN="# >>> jarvis-browser-proxy systemd env >>>"
HYPRLAND_END="# <<< jarvis-browser-proxy systemd env <<<"
SERVICE_NAME="jarvis-browser-proxy.service"

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

prompt_yes_no() {
  local prompt="$1"
  if [[ ! -t 0 ]]; then
    return 1
  fi

  local answer
  printf '%s [y/N] ' "$prompt"
  read -r answer
  case "$answer" in
    y|Y|yes|YES)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

hyprland_block_present() {
  [[ -f "$HYPRLAND_CONFIG" ]] && grep -Fq "$HYPRLAND_BEGIN" "$HYPRLAND_CONFIG"
}

append_hyprland_block() {
  mkdir -p "$(dirname "$HYPRLAND_CONFIG")"
  touch "$HYPRLAND_CONFIG"
  cat >> "$HYPRLAND_CONFIG" <<EOF

$HYPRLAND_BEGIN
exec-once = systemctl --user import-environment WAYLAND_DISPLAY XDG_RUNTIME_DIR DISPLAY DBUS_SESSION_BUS_ADDRESS HYPRLAND_INSTANCE_SIGNATURE
exec-once = dbus-update-activation-environment --systemd WAYLAND_DISPLAY XDG_RUNTIME_DIR DISPLAY DBUS_SESSION_BUS_ADDRESS HYPRLAND_INSTANCE_SIGNATURE
$HYPRLAND_END
EOF
}

maybe_configure_hyprland() {
  if hyprland_block_present; then
    log "Hyprland systemd environment import block already present in $HYPRLAND_CONFIG"
    return 0
  fi

  if [[ ! -t 0 ]]; then
    log "Hyprland config not amended automatically because install.sh is not running interactively."
    log "Add this block to $HYPRLAND_CONFIG:"
    cat <<EOF
$HYPRLAND_BEGIN
exec-once = systemctl --user import-environment WAYLAND_DISPLAY XDG_RUNTIME_DIR DISPLAY DBUS_SESSION_BUS_ADDRESS HYPRLAND_INSTANCE_SIGNATURE
exec-once = dbus-update-activation-environment --systemd WAYLAND_DISPLAY XDG_RUNTIME_DIR DISPLAY DBUS_SESSION_BUS_ADDRESS HYPRLAND_INSTANCE_SIGNATURE
$HYPRLAND_END
EOF
    return 0
  fi

  if prompt_yes_no "Amend $HYPRLAND_CONFIG with Hyprland exec-once lines for systemd user env import?"; then
    append_hyprland_block
    log "Updated $HYPRLAND_CONFIG"
  else
    log "Skipped Hyprland config changes."
    log "Without those exec-once lines, GUI launch after reboot/login may be unreliable."
  fi
}

import_session_environment_now() {
  local vars=()
  local var
  for var in WAYLAND_DISPLAY XDG_RUNTIME_DIR DISPLAY DBUS_SESSION_BUS_ADDRESS HYPRLAND_INSTANCE_SIGNATURE; do
    if [[ -n "${!var:-}" ]]; then
      vars+=("$var")
    fi
  done

  if [[ ${#vars[@]} -eq 0 ]]; then
    log "Current shell does not have Wayland session variables; skipping immediate systemd env import."
    return 0
  fi

  systemctl --user import-environment "${vars[@]}"
  if command -v dbus-update-activation-environment >/dev/null 2>&1; then
    dbus-update-activation-environment --systemd "${vars[@]}"
  fi
  log "Imported current graphical session environment into systemd --user"
}

maybe_enable_and_start_service() {
  if ! [[ -t 0 ]]; then
    return 0
  fi

  if prompt_yes_no "Enable and start $SERVICE_NAME now?"; then
    systemctl --user enable --now "$SERVICE_NAME"
    systemctl --user --no-pager --full status "$SERVICE_NAME" || true
  else
    log "Skipped enabling $SERVICE_NAME"
  fi
}

mkdir -p "$BIN_DIR" "$UNIT_DIR" "$CONFIG_DIR" "$STATE_DIR" "$PROFILE_DIR" "$DOWNLOADS_DIR"

require_cmd install
require_cmd systemctl
require_cmd go

log "Building jarvis-browser-proxy"
(
  cd "$SRC_DIR"
  go build -o "$BIN_DIR/jarvis-browser-proxy" ./cmd/jarvis-browser-proxy
)

install -m 0644 "$SRC_DIR/jarvis-browser-proxy.service" "$UNIT_DIR/jarvis-browser-proxy.service"

if [[ ! -f "$PROXY_ENV_FILE" ]]; then
  TOKEN="$(generate_token)"
  cat > "$PROXY_ENV_FILE" <<EOF
JARVIS_BROWSER_PROXY_TOKEN=$TOKEN
JARVIS_BROWSER_PROXY_ADDR=$PROXY_ADDR
JARVIS_CHROME_BASE_URL=$CHROME_BASE_URL
JARVIS_CHROME_PROFILE=$PROFILE_DIR
JARVIS_CHROME_DOWNLOADS=$DOWNLOADS_DIR
JARVIS_CHROME_STATE_DIR=$STATE_DIR
JARVIS_CHROME_WORKSPACE=$WORKSPACE
JARVIS_CHROME_HOMEPAGE=$HOMEPAGE
JARVIS_CHROME_DEBUG_HOST=$DEBUG_HOST
JARVIS_CHROME_DEBUG_PORT=$DEBUG_PORT
JARVIS_CHROME_STARTUP_WAIT=$STARTUP_WAIT
JARVIS_CHROME_STOP_WAIT=$STOP_WAIT
EOF
  if [[ -n "$BROWSER_BINARY" ]]; then
    printf 'JARVIS_CHROME_BROWSER=%s\n' "$BROWSER_BINARY" >> "$PROXY_ENV_FILE"
  fi
  chmod 600 "$PROXY_ENV_FILE"
  log "Created $PROXY_ENV_FILE"
else
  log "Keeping existing $PROXY_ENV_FILE"
fi

maybe_configure_hyprland
import_session_environment_now
systemctl --user daemon-reload
maybe_enable_and_start_service

cat <<EOF
Installed:
  $BIN_DIR/jarvis-browser-proxy
  $UNIT_DIR/jarvis-browser-proxy.service

Created/used config:
  $PROXY_ENV_FILE

Show the token:
  sed -n 's/^JARVIS_BROWSER_PROXY_TOKEN=//p' $PROXY_ENV_FILE

Browser start/stop/status happens via the HTTP API on the proxy.
If you said yes to the Hyprland change, future logins will keep the GUI env wiring in place.
EOF
