# Agent Master

Run Claude Code on your own machines and control its sessions from a browser or
desktop app. Agent Master installs one lightweight daemon per machine, embeds a
Web interface, and streams Claude's messages and tool activity in real time.

- No central server or hosted account
- Multiple machines in one client
- Resumable sessions and token-level streaming
- Embedded Web UI plus an optional Electron desktop app
- Per-machine access tokens and workspace restrictions

> Agent Master can read, modify, and execute files with the permissions of the
> daemon process. Keep it on a trusted private network such as Tailscale. Do not
> expose port `8888` directly to the public internet.

## How it works

Install the daemon on every machine where Claude Code should run. You can then
open that machine's embedded Web UI directly, or connect to several machines
from the desktop app.

```text
Browser / Desktop App
        │  HTTP + SSE
        ▼
Agent Master daemon  ──►  Claude Code CLI
        │
        └── sessions, configuration and logs in ~/.agent-master/
```

There is no central hub. Each client stores a list of machine URLs and tokens
and connects to those machines directly.

## Quick start

### Prerequisites

On every machine that will run the daemon:

1. Install and authenticate Claude Code (`claude`).
2. Install Node.js 20 or newer for the npm installer.

### 1. Install and start the daemon

```bash
npm install -g @ken-chy129/agent-master
agent-master start
```

`start` creates a per-user background service and prints the Web address and
access token. The daemon itself is a standalone Go binary; Node.js only provides
the installer and command shim.

### 2. Open the Web interface

Open:

```text
http://127.0.0.1:8888
```

Paste the token printed by `agent-master start`. The production Web app is
embedded in the daemon, so there is no separate Web deployment to configure.

To connect from another device, run:

```bash
agent-master pair
```

This prints reachable addresses, the token, a deep link, and a QR code.

### 3. Optional desktop app

Download the desktop client from the
[latest Release](https://github.com/Ken-Chy129/agent-master/releases/latest):

- macOS: choose the `.dmg` matching Apple Silicon (`arm64`) or Intel (`x64`)
- Windows: choose the versioned desktop installer `.exe`, not an
  `agent-master-windows-*` runtime binary

The desktop app is a client; it does not replace the daemon on machines where
Claude Code runs. It stores tokens using Electron `safeStorage` and supports
`agentmaster://` pairing links. The desktop app and Web UI can be open at the
same time.

Desktop builds are currently unsigned. Windows may show a SmartScreen warning.
On macOS, clear the download quarantine once if the app is blocked:

```bash
xattr -cr "/Applications/Agent Master.app"
```

## Multiple machines

Run Agent Master on each target machine, then add each machine using the output
from `agent-master pair`. For access outside the local network, put the daemon
machines and client devices on the same Tailscale tailnet and use the Tailscale
address as `public_url`.

Do not forward port `8888` to the public internet. If public exposure is
unavoidable, use a TLS reverse proxy, a strong token, and restricted CORS
origins.

## Commands

| Command | Purpose |
| --- | --- |
| `agent-master start` | Install or update the background service and start it |
| `agent-master status` | Show service and health status |
| `agent-master pair` | Print connection addresses, token, deep link, and QR code |
| `agent-master token` | Print the current access token |
| `agent-master restart` | Restart the installed service |
| `agent-master stop` | Stop the service without removing it |
| `agent-master uninstall` | Remove the service definition; keep data and configuration |
| `agent-master serve` | Run in the foreground for development or debugging |
| `agent-master version` | Print the installed version |

## Updating and uninstalling

Update an npm installation:

```bash
npm install -g @ken-chy129/agent-master@latest
agent-master restart
```

When switching from the native installer to npm, remove the old service first.
This does not delete sessions, configuration, or the token:

```bash
agent-master uninstall
npm install -g @ken-chy129/agent-master
agent-master start
```

To remove the npm installation completely:

```bash
agent-master uninstall
npm uninstall -g @ken-chy129/agent-master
```

If Node.js is managed with NVM, global npm packages belong to the active Node.js
version and may need to be installed again after switching versions.

## Native installation alternative

The native installers place the latest daemon in `~/.local/bin` and do not
require Node.js.

Linux and macOS:

```bash
curl -fsSL https://raw.githubusercontent.com/Ken-Chy129/agent-master/main/install.sh | bash
agent-master start
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/Ken-Chy129/agent-master/main/install.ps1 | iex
agent-master start
```

Set `INSTALL_DIR` to override the destination or `AGENT_MASTER_VERSION` to pin a
specific release.

## Configuration and data

Agent Master stores its state in `~/.agent-master/`:

- `config.json` — daemon settings and access token
- `agent-master.db` — session event ledger and projections
- `daemon.log` — rolling daemon log
- `uploads/` — images staged for sessions

Example configuration:

```json
{
  "host": "0.0.0.0",
  "port": 8888,
  "token": "<generated>",
  "claude_bin": "",
  "workspace_roots": [],
  "allowed_origins": [],
  "public_url": ""
}
```

- `claude_bin`: optional path to the Claude CLI; empty resolves `claude` from `PATH`
- `workspace_roots`: allowed session directories; empty currently means unrestricted
- `allowed_origins`: accepted browser origins; empty allows any origin with a valid token
- `public_url`: address advertised by `pair`, such as a Tailscale or reverse-proxy URL

The configuration file is created with user-only permissions. Treat its token
as a secret.

## Release files

Most users only need the npm command and, optionally, one desktop installer.
The remaining files on GitHub Releases are runtime assets used automatically by
the npm and native installers:

- macOS `.dmg` and the versioned Windows installer `.exe` — desktop clients
- `agent-master-<os>-<arch>` — daemon binaries selected by the installer
- `SHA256SUMS` — checksum manifest used to verify daemon downloads

## Development

Build the daemon and embedded Web UI from source:

```bash
make build
./dist/agent-master serve
```

Run backend tests and checks:

```bash
go test ./...
go vet ./...
```

Run the Web client in development mode:

```bash
cd frontend
npm ci
npm run dev -w @agent-master/web
```

Build or run the desktop client:

```bash
cd frontend
npm run build -w @agent-master/desktop
npm run dev -w @agent-master/desktop
```

The Android project contains a tested Kotlin core and a Compose application
scaffold, but the Android app has not yet been fully built, validated, or
published as a supported client.

## Project structure

```text
cmd/agent-master/        CLI and daemon entry point
internal/config/         configuration and local paths
internal/service/        systemd, launchd, and Windows service integration
internal/server/         HTTP API, SSE, and embedded Web UI
internal/session/        session execution and event projection
internal/store/          SQLite event ledger
frontend/apps/web/       React Web client
frontend/apps/desktop/   Electron desktop client
frontend/packages/core/  shared TypeScript API client and models
npm/agent-master/        npm installer and native command shim
android/                 experimental Android client
```

## Documentation

- [HTTP API](docs/API.md)
- [Architecture and design](docs/DESIGN.md)
- [Developer handoff and implementation status](docs/HANDOFF.md)
