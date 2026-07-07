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
make build                 # → dist/agent-master (static, CGO_ENABLED=0)
./dist/agent-master serve  # listens on :8888
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

**Web** (`frontend/`, npm workspaces):

```bash
cd frontend && npm install
npm run dev -w @agent-master/web    # Vite on http://localhost:5173
```

`packages/core` (`@agent-master/core`) is a framework-free TS client (`ApiClient`,
`SseClient`, machine model) reused across web/desktop/(future) mobile.

**Desktop** (Electron, `frontend/apps/desktop`): loads the same web UI, stores
tokens in the OS-encrypted secure store (Electron `safeStorage`), and handles
`agentmaster://` pairing deep links.

```bash
npm run dev -w @agent-master/desktop    # dev (needs the web dev server)
npm run dist -w @agent-master/desktop   # package a macOS app (run on macOS)
```

**Reachability**: put your machines and client on one **Tailscale** tailnet and
use the tailnet URL (set it as `public_url`) — no public port exposure.

## Install (release)

```bash
curl -fsSL https://raw.githubusercontent.com/Ken-Chy129/agent-master/main/install.sh | bash
agent-master service install   # systemd (Linux) / launchd (macOS)
agent-master token             # copy into your client
```

## Layout

```
cmd/agent-master/   CLI entry: serve / service / token / version
internal/
  config/           ~/.agent-master/config.json (host, port, token)
  store/            SQLite (modernc.org/sqlite): event ledger + projections
  server/           HTTP: /health + token-protected API
  service/          systemd / launchd install
  version/          build version
install.sh          release installer
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
