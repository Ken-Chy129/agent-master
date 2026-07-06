# agent-master

A per-machine daemon that controls **Claude Code** on the host and exposes an
HTTP API so you can manage sessions remotely from a desktop app (Electron),
a web page, or (later) an Android client.

Topology: run one daemon per machine; a client holds a list of machines and
switches between them. No central hub. Reach machines from anywhere with a
private overlay network like Tailscale.

> Status: **M1** — drives Claude Code end to end: create a session, send a
> message, stream the reply over SSE, and resume context across turns. See
> `docs/DESIGN.md` for the full plan.

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
