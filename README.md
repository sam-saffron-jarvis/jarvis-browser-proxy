# Jarvis shared browser setup

This gives you a **separate visible Chromium/Chrome window** that Jarvis can use, without handing over your normal browser profile.

What you get:

- a dedicated `jarvis-chrome` browser profile
- a normal Wayland window on your desktop
- a fixed Chrome DevTools port (`9222`) that stays bound to `127.0.0.1`
- a tiny wrapper command: `jarvis-browser start|stop|status|restart`
- a small Go HTTP service that can:
  - authenticate requests from Jarvis
  - start/stop/status the browser via `jarvis-browser`
  - proxy `/json/version`, `/json/list`, `/json/protocol`
  - proxy DevTools websockets without exposing raw CDP on the network

## Files in this directory

- `README.md` — this file
- `launch-jarvis-chrome.sh` — starts the dedicated browser instance
- `jarvis-browser` — helper command with `start|stop|restart|status`
- `jarvis-chrome.service` — systemd user unit for the browser
- `install.sh` — copies browser files into place and prints next steps
- `go.mod` — Go module for the proxy service
- `cmd/jarvis-browser-proxy/main.go` — proxy service entrypoint
- `proxy/server.go` — proxy implementation
- `proxy/server_test.go` — tests

## Assumptions

- Arch/Hyprland/Wayland-ish desktop
- `systemd --user` is available
- one of these exists on the host:
  - `chromium`
  - `google-chrome-stable`
  - `google-chrome`
  - `chromium-browser`
- `curl` exists for status checks
- Go is installed if you want to build the proxy binary yourself

## Install the browser bits on the host

From this directory:

```bash
bash install.sh
```

That will copy:

- `jarvis-chrome.service` → `~/.config/systemd/user/`
- `jarvis-browser` → `~/.local/bin/`
- `launch-jarvis-chrome.sh` → `~/.local/bin/`

## One-time environment import

For GUI apps launched by `systemd --user`, you want your current desktop env imported so the browser can appear in the session.

Run this once in your logged-in desktop session:

```bash
systemctl --user import-environment \
  WAYLAND_DISPLAY XDG_RUNTIME_DIR DISPLAY \
  DBUS_SESSION_BUS_ADDRESS HYPRLAND_INSTANCE_SIGNATURE

dbus-update-activation-environment --systemd \
  WAYLAND_DISPLAY XDG_RUNTIME_DIR DISPLAY \
  DBUS_SESSION_BUS_ADDRESS HYPRLAND_INSTANCE_SIGNATURE
```

## Start / stop / status locally

```bash
jarvis-browser start
jarvis-browser status
jarvis-browser stop
```

## Build the proxy

```bash
go build -o jarvis-browser-proxy ./cmd/jarvis-browser-proxy
```

## Run the proxy

```bash
export JARVIS_BROWSER_PROXY_TOKEN='pick-a-token'
./jarvis-browser-proxy
```

Optional env vars:

```bash
export JARVIS_BROWSER_PROXY_ADDR='127.0.0.1:8787'
export JARVIS_BROWSER_COMMAND="$HOME/.local/bin/jarvis-browser"
export JARVIS_CHROME_BASE_URL='http://127.0.0.1:9222'
```

## API

Every protected request needs one of:

- `Authorization: Bearer <token>`
- `X-API-Key: <token>`
- `?token=<token>` for websocket clients that are annoying about headers

### Lifecycle

```text
GET  /jarvis-browser/status
POST /jarvis-browser/start
POST /jarvis-browser/stop
POST /jarvis-browser/restart
```

### DevTools discovery

```text
GET /jarvis-browser/json/version
GET /jarvis-browser/json/list
GET /jarvis-browser/json/protocol
```

Returned `webSocketDebuggerUrl` values are rewritten to point back at this proxy.

### Websocket proxy

```text
GET /jarvis-browser/devtools/{path}
```

Example:

```text
ws://host:8787/jarvis-browser/devtools/devtools/browser/<id>?token=pick-a-token
```

## Test it

```bash
go test ./...
```

Tests cover:

- auth on HTTP endpoints
- command result handling
- `/json/version` rewrite
- `/json/list` rewrite
- websocket proxying
- forwarded-proto → `wss://` rewrite
- unauthorized websocket rejection

## Browser profile location

The dedicated profile lives at:

```bash
~/.local/share/jarvis-chrome
```

That is the actual safety boundary. Keep it separate from your main browser profile.
