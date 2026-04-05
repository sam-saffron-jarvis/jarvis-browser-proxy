# Jarvis shared browser setup

This gives you a **separate visible Chromium/Chrome window** that Jarvis can use, without handing over your normal browser profile.

The current design is intentionally simple: **one Go binary, one user service**.

What you get:

- a dedicated `jarvis-chrome` browser profile
- a normal Wayland window on your desktop
- Chrome DevTools bound to `127.0.0.1:9222`
- the browser lands on **Hyprland workspace 9 by default**
- a small Go service that:
  - authenticates requests from Jarvis
  - starts/stops/restarts the browser itself
  - reports browser status
  - proxies `/json/version`, `/json/list`, `/json/protocol`
  - proxies DevTools websockets without exposing raw CDP on the network
- a one-shot installer that builds the Go binary, writes config, and installs **one** user service

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

Then enable the single service:

```bash
systemctl --user enable --now jarvis-browser-proxy
systemctl --user status jarvis-browser-proxy --no-pager
```

At that point the proxy is running, but the browser is **not** started until someone calls the API.

By default the proxy listens on `0.0.0.0:8787` so Jarvis can reach it from outside the host. Keep auth enabled and put it behind your existing host service if you want a tighter exposure surface.

## API

All browser lifecycle is handled by the proxy itself.

Endpoints:

- `GET /healthz`
- `GET /jarvis-browser/status`
- `POST /jarvis-browser/start`
- `POST /jarvis-browser/stop`
- `POST /jarvis-browser/restart`
- `GET /jarvis-browser/json/version`
- `GET /jarvis-browser/json/list`
- `GET /jarvis-browser/json/protocol`
- `WS /jarvis-browser/devtools/{path}`

Auth options:

- `Authorization: Bearer <token>`
- `X-API-Key: <token>`
- `?token=<token>` for websocket clients

## Config file

The installer creates:

```bash
~/.config/jarvis-browser-proxy.env
```

Show the token with:

```bash
sed -n 's/^JARVIS_BROWSER_PROXY_TOKEN=//p' ~/.config/jarvis-browser-proxy.env
```

Common settings in that file:

- `JARVIS_BROWSER_PROXY_ADDR=0.0.0.0:8787`
- `JARVIS_CHROME_PROFILE=~/.local/share/jarvis-chrome`
- `JARVIS_CHROME_DOWNLOADS=~/.local/share/jarvis-chrome/downloads`
- `JARVIS_CHROME_STATE_DIR=~/.local/state/jarvis-browser-proxy`
- `JARVIS_CHROME_WORKSPACE=9`
- `JARVIS_CHROME_DEBUG_PORT=9222`
- `JARVIS_CHROME_HOMEPAGE=about:blank`
- `JARVIS_CHROME_HEALTH_CHECK_INTERVAL=30s`
- `JARVIS_CHROME_RESTART_COOLDOWN=15s`
- `JARVIS_CHROME_BROWSER=` (optional explicit browser binary)

The proxy also self-heals: if Chrome is still running but CDP on `127.0.0.1:9222` is wedged, request-time recovery will restart the browser once and retry, and the background watchdog will recycle unhealthy browser/CDP state automatically.

If you edit the env file, reload and restart the service:

```bash
systemctl --user daemon-reload
systemctl --user restart jarvis-browser-proxy
```

## Workspace selection

Default workspace is `9` so the window is easy to find.

Override it with:

```bash
sed -i 's/^JARVIS_CHROME_WORKSPACE=.*/JARVIS_CHROME_WORKSPACE=4/' ~/.config/jarvis-browser-proxy.env
systemctl --user restart jarvis-browser-proxy
```

Or disable workspace moves entirely:

```bash
sed -i 's/^JARVIS_CHROME_WORKSPACE=.*/JARVIS_CHROME_WORKSPACE=/' ~/.config/jarvis-browser-proxy.env
systemctl --user restart jarvis-browser-proxy
```

The move is best-effort and only runs when `hyprctl` is available.

## Installed files

- `~/.local/bin/jarvis-browser-proxy`
- `~/.config/systemd/user/jarvis-browser-proxy.service`
- `~/.config/jarvis-browser-proxy.env`

## Files in this repo

- `README.md` — this file
- `install.sh` — builds the binary, creates config, installs the single user unit
- `jarvis-browser-proxy.service` — systemd user unit for the Go proxy
- `cmd/jarvis-browser-proxy/main.go` — CLI entrypoint
- `proxy/server.go` — HTTP routes + browser lifecycle + CDP proxy
- `proxy/server_test.go` — tests for auth, rewrites, websocket proxying, and browser lifecycle smoke test
