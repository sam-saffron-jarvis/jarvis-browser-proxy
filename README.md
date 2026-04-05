# Jarvis shared browser setup

This gives you a **separate visible Chromium/Chrome window** that Jarvis can use, without handing over your normal browser profile.

What you get:

- a dedicated `jarvis-chrome` browser profile
- a normal Wayland window on your desktop
- Chrome DevTools bound to `127.0.0.1:9222`
- a tiny wrapper command: `jarvis-browser start|stop|status|restart`
- a small Go proxy that can:
  - authenticate requests from Jarvis
  - start/stop/status the browser via `jarvis-browser`
  - proxy `/json/version`, `/json/list`, `/json/protocol`
  - proxy DevTools websockets without exposing raw CDP on the network
- a one-shot installer that copies files, builds the Go binary, creates a config file, and installs user services

## Quick install

From this directory:

```bash
bash install.sh
```

Then, once in your logged-in desktop session:

```bash
systemctl --user import-environment \
  WAYLAND_DISPLAY XDG_RUNTIME_DIR DISPLAY \
  DBUS_SESSION_BUS_ADDRESS HYPRLAND_INSTANCE_SIGNATURE

dbus-update-activation-environment --systemd \
  WAYLAND_DISPLAY XDG_RUNTIME_DIR DISPLAY \
  DBUS_SESSION_BUS_ADDRESS HYPRLAND_INSTANCE_SIGNATURE
```

Then test:

```bash
jarvis-browser start
jarvis-browser status
systemctl --user start jarvis-browser-proxy
systemctl --user status jarvis-browser-proxy --no-pager
```

Your generated proxy token is stored in:

```bash
~/.config/jarvis-browser-proxy.env
```

Show it with:

```bash
sed -n 's/^JARVIS_BROWSER_PROXY_TOKEN=//p' ~/.config/jarvis-browser-proxy.env
```

## Installed files

Browser bits:

- `~/.local/bin/jarvis-browser`
- `~/.local/bin/launch-jarvis-chrome.sh`
- `~/.config/systemd/user/jarvis-chrome.service`

Proxy bits:

- `~/.local/bin/jarvis-browser-proxy`
- `~/.config/systemd/user/jarvis-browser-proxy.service`
- `~/.config/jarvis-browser-proxy.env`

## Files in this repo

- `README.md` — this file
- `install.sh` — installs browser bits, builds proxy, creates config, reloads user units
- `launch-jarvis-chrome.sh` — starts the dedicated browser instance
- `jarvis-browser` — helper command with `start|stop|restart|status`
- `jarvis-chrome.service` — systemd user unit for the browser
- `jarvis-browser-proxy.service` — systemd user unit for the Go proxy
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
- Go is installed if you want `install.sh` to build the proxy automatically

## Manual build / run

If you do not want the service unit yet:

```bash
go build -o jarvis-browser-proxy ./cmd/jarvis-browser-proxy
./jarvis-browser-proxy -token "$(sed -n 's/^JARVIS_BROWSER_PROXY_TOKEN=//p' ~/.config/jarvis-browser-proxy.env)"
```

Flags are available for all config if you want them:

```bash
./jarvis-browser-proxy \
  -addr 127.0.0.1:8787 \
  -token your-token \
  -browser-command "$HOME/.local/bin/jarvis-browser" \
  -chrome-base-url http://127.0.0.1:9222
```

Environment variables work too:

```bash
export JARVIS_BROWSER_PROXY_TOKEN='pick-a-token'
export JARVIS_BROWSER_PROXY_ADDR='127.0.0.1:8787'
export JARVIS_BROWSER_COMMAND="$HOME/.local/bin/jarvis-browser"
export JARVIS_CHROME_BASE_URL='http://127.0.0.1:9222'
./jarvis-browser-proxy
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
