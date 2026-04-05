#!/usr/bin/env bash
set -euo pipefail

SRC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_DIR="$HOME/.local/bin"
UNIT_DIR="$HOME/.config/systemd/user"

mkdir -p "$BIN_DIR" "$UNIT_DIR"

install -m 0755 "$SRC_DIR/jarvis-browser" "$BIN_DIR/jarvis-browser"
install -m 0755 "$SRC_DIR/launch-jarvis-chrome.sh" "$BIN_DIR/launch-jarvis-chrome.sh"
install -m 0644 "$SRC_DIR/jarvis-chrome.service" "$UNIT_DIR/jarvis-chrome.service"

systemctl --user daemon-reload

cat <<'EOF'
Installed:
  ~/.local/bin/jarvis-browser
  ~/.local/bin/launch-jarvis-chrome.sh
  ~/.config/systemd/user/jarvis-chrome.service

Next, in your desktop session, run:

systemctl --user import-environment \
  WAYLAND_DISPLAY XDG_RUNTIME_DIR DISPLAY \
  DBUS_SESSION_BUS_ADDRESS HYPRLAND_INSTANCE_SIGNATURE

dbus-update-activation-environment --systemd \
  WAYLAND_DISPLAY XDG_RUNTIME_DIR DISPLAY \
  DBUS_SESSION_BUS_ADDRESS HYPRLAND_INSTANCE_SIGNATURE

Then test:
  jarvis-browser start
  jarvis-browser status

Optional:
  systemctl --user enable jarvis-chrome.service

Not recommended unless you actually want it auto-starting.
EOF
