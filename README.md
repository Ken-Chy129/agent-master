# agent-master

A per-machine daemon that controls **Claude Code** on the host and exposes an
HTTP API so you can manage sessions remotely from a desktop app (Electron),
a web page, or (later) an Android client.

Topology: run one daemon per machine; a client holds a list of machines and
switches between them. No central hub. Reach machines from anywhere with a
private overlay network like Tailscale.

> Status: drives Claude Code end to end (session, send, SSE stream, resume),
> a React web UI + Electron desktop app + Android client that manage **multiple
> machines** from one client, server-side **render_state** with token-level
> streaming, and `agent-master pair` for onboarding.
>
> **New here? Read [docs/HANDOFF.md](docs/HANDOFF.md)** (current status, what's
> done, what's left, how to build/run, and the gotchas). Architecture:
> [docs/DESIGN.md](docs/DESIGN.md); API contract: [docs/API.md](docs/API.md).

## Quick start (from source)

```bash
make build                 # builds Web assets + dist/agent-master
./dist/agent-master serve  # API + Web UI on http://127.0.0.1:8888
```

Verify:

```bash
curl -s localhost:8888/health
# {"status":"ok","version":"..."}

TOKEN=$(./dist/agent-master token)
curl -s localhost:8888/api/info -H "Authorization: Bearer $TOKEN"
# {"name":"<host>","providers":{"claude":{"available":true,...}},...}
```

Drive Claude in a workspace:

```bash
AUTH="Authorization: Bearer $TOKEN"

# create a session bound to a working directory
SID=$(curl -s -X POST localhost:8888/api/sessions -H "$AUTH" \
  -d '{"title":"demo","workspaceDir":"/path/to/repo"}' | jq -r .id)

# stream the session (SSE) in one terminal
curl -sN "localhost:8888/api/sessions/$SID/stream?token=$TOKEN"

# send a message in another — the reply streams into the SSE above
curl -s -X POST localhost:8888/api/sessions/$SID/send -H "$AUTH" \
  -d '{"message":"list the files here","clientIntentId":"abc"}'
```

## API (M1)

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/health` | liveness (public) |
| GET | `/api/info` | machine name + provider availability |
| GET | `/api/sessions` | list sessions (recent projection) |
| POST | `/api/sessions` | create `{workspaceDir, model?, title?}` |
| GET | `/api/sessions/{id}` | session detail |
| PATCH | `/api/sessions/{id}` | rename `{title}` |
| DELETE | `/api/sessions/{id}` | delete |
| GET | `/api/sessions/{id}/messages?before_seq=&limit=` | history (ledger events) |
| POST | `/api/sessions/{id}/send` | `{message, clientIntentId}` → starts a run |
| POST | `/api/sessions/{id}/interrupt` | cancel the active run |
| GET | `/api/sessions/{id}/stream` | resumable SSE (`Last-Event-ID` / `?after_seq=`) |

SSE frames: `id: <seq>` + `event: am_event` + `data: {seq,type,runId,payload,createdAt}`.
Event types: `user_message`, `run_started`, `assistant_message`, `tool_call`,
`tool_result`, `run_finished`, `error`.

## Clients (M4)

One control client manages many machines: run a daemon on each machine, and the
client holds a list of machine profiles (`{name, baseUrl, token}`) and switches
between them. Get a machine's connection info with:

```bash
agent-master pair    # prints base URLs, token, an agentmaster:// deep link, and a QR
```

**Web**: production builds are embedded in the native daemon. After
`agent-master start`, open:

```text
http://127.0.0.1:8888
```

The first-run form defaults to the page's own origin, so a LAN or Tailscale URL
also connects to the daemon that served it. Paste the token printed by
`agent-master start` or `agent-master pair`. Browser tokens stay in that
browser's local storage.

For Web development (`frontend/`, npm workspaces):

```bash
cd frontend && npm install
npm run dev -w @agent-master/web    # Vite on http://localhost:5173
```

`packages/core` (`@agent-master/core`) is a framework-free TS client (`ApiClient`,
`SseClient`, machine model) reused across web/desktop/(future) mobile.

**Desktop** (Electron, `frontend/apps/desktop`): loads the same web UI, stores
tokens in the OS-encrypted secure store (Electron `safeStorage`), and handles
`agentmaster://` pairing deep links. It connects to the daemon over its existing
API, so the desktop app and embedded Web client can be open at the same time.

Download the prebuilt app from the
[Releases page](https://github.com/Ken-Chy129/agent-master/releases/latest):
macOS `agent-master-<version>-<arch>.dmg` (`arm64` for Apple Silicon, `x64` for
Intel) or the Windows installer `agent-master-<version>-x64.exe`. Both builds
are unsigned: on Windows click through the SmartScreen warning once; on macOS
clear the download quarantine on first launch:

```bash
xattr -cr "/Applications/Agent Master.app"   # then open normally
```

Or build it yourself on macOS:

```bash
npm run dev -w @agent-master/desktop    # dev (needs the web dev server)
npm run dist -w @agent-master/desktop   # package a .dmg + .zip into release/
```

**Reachability**: put your machines and client on one **Tailscale** tailnet and
use the tailnet URL (set it as `public_url`) — no public port exposure.

## Install (release)

**npm (recommended, macOS / Linux / Windows, Node.js 20+):**

```bash
npm install -g agent-master
agent-master start
# open http://127.0.0.1:8888
```

The npm installer downloads the matching native release binary and verifies its
SHA-256 checksum. The daemon remains a standalone Go process; Node only provides
the installation and command shim.

**Native installer:** installs the latest release binary into `~/.local/bin`
(no sudo). Override with `INSTALL_DIR=` (a system dir like `/usr/local/bin` then
uses sudo).

**Linux / macOS**:

```bash
curl -fsSL https://raw.githubusercontent.com/Ken-Chy129/agent-master/main/install.sh | bash
# ensure ~/.local/bin is on PATH (the installer prints this if needed)
agent-master start   # install + start the background service (systemd / launchd)
agent-master pair    # show URL/token/QR for desktop or remote clients
```

**Windows** (10 1903+, PowerShell):

```powershell
irm https://raw.githubusercontent.com/Ken-Chy129/agent-master/main/install.ps1 | iex
agent-master start   # runs in the background + auto-starts at logon
agent-master pair
```

Service commands: `start` / `stop` / `restart` / `status` / `uninstall`.
`serve` runs in the foreground (dev/debug). On Linux/macOS the background
service is a systemd user unit / launchd LaunchAgent; on Windows it's a
windowless background process plus a per-user Run-key autostart (no admin
needed).

## Layout

```
cmd/agent-master/   CLI entry: start / stop / status / pair / token / serve / version
internal/
  config/           ~/.agent-master/config.json (host, port, token)
  store/            SQLite (modernc.org/sqlite): event ledger + projections
  server/           HTTP: embedded Web UI + /health + token-protected API
  service/          systemd / launchd / Windows Run-key install
  version/          build version
npm/agent-master/   npm installer + native command shim
install.sh          release installer (Linux/macOS)
install.ps1         release installer (Windows)
Makefile            build / release (cross-compile)
```

## Config

`~/.agent-master/config.json` (created on first run, `0600`):

```json
{
  "host": "0.0.0.0",
  "port": 8888,
  "token": "<generated>",
  "claude_bin": "",
  "workspace_roots": []
}
```

`claude_bin` empty → resolve `claude` from `PATH`.

## Security

The API controls Claude on this machine (it can read/write files and run
commands with the daemon's permissions). Keep it on a trusted network
(Tailscale recommended); do not expose the port to the public internet without
TLS and the token.
