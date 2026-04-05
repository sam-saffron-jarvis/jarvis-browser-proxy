#!/usr/bin/env bash
set -euo pipefail

SRC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVICE_NAME="jarvis-browser-proxy.service"
PROXY_ENV_FILE="${PROXY_ENV_FILE:-$HOME/.config/jarvis-browser-proxy.env}"
DO_PULL=0

usage() {
  cat <<EOF
usage: ./upgrade.sh [--pull]

Rebuild and reinstall jarvis-browser-proxy from the current checkout,
reload the user unit, restart the service, and show status.

Options:
  --pull   git pull --ff-only before rebuilding
EOF
}

for arg in "$@"; do
  case "$arg" in
    --pull)
      DO_PULL=1
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $arg" >&2
      usage >&2
      exit 2
      ;;
  esac
done

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

require_cmd git
require_cmd systemctl
require_cmd bash

cd "$SRC_DIR"

if [[ "$DO_PULL" == "1" ]]; then
  git pull --ff-only
fi

bash "$SRC_DIR/install.sh"
systemctl --user daemon-reload
systemctl --user restart "$SERVICE_NAME"
systemctl --user --no-pager --full status "$SERVICE_NAME" || true

if [[ -f "$PROXY_ENV_FILE" ]]; then
  # shellcheck disable=SC1090
  source "$PROXY_ENV_FILE"
  addr="${JARVIS_BROWSER_PROXY_ADDR:-127.0.0.1:8787}"
  token="${JARVIS_BROWSER_PROXY_TOKEN:-}"
  if [[ -n "$token" ]]; then
    echo
    echo "Smoke check:"
    curl -fsS -H "Authorization: Bearer $token" "http://$addr/jarvis-browser/status" || true
  fi
fi
