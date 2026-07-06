# agent-master

A per-machine daemon that controls **Claude Code** on the host and exposes an
HTTP API so you can manage sessions remotely from a desktop app (Electron),
a web page, or (later) an Android client.

Topology: run one daemon per machine; a client holds a list of machines and
switches between them. No central hub. Reach machines from anywhere with a
private overlay network like Tailscale.

> Status: **M0** — daemon skeleton (config, SQLite schema, `/health`,
> token-protected API, service install). See `docs/DESIGN.md` for the full plan.

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
