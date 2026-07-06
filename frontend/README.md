# agent-master frontend

Web frontend + reusable TypeScript client for the agent-master daemon. It is a
**dumb client**: the daemon owns conversation structure and streams events; the
UI renders the event list it receives (see `docs/API.md` and `docs/DESIGN.md`
§10).

## Layout (npm workspaces monorepo)

```
frontend/
  package.json            # workspaces: ["packages/*","apps/*"]
  packages/core/          # @agent-master/core — pure TS, no DOM/React
    src/
      types.ts            # Session, RecentSession, WireEvent, payloads
      api.ts              # ApiClient (REST) + ApiError
      sse.ts              # SseClient (EventSource) with reconnect
      index.ts
    scripts/smoke.mjs     # integration smoke test vs a real daemon
  apps/web/               # @agent-master/web — Vite + React + TS
    src/
      main.tsx, App.tsx
      store.ts            # Zustand store
      components/         # ConnectionSetup, SessionList, Conversation, Composer
```

`packages/core` has no framework dependencies and can be reused by the future
desktop (Electron) and React Native apps.

## Getting started

```bash
cd frontend
npm install
```

### Run the web app

```bash
npm run dev            # from frontend/ — starts Vite on http://localhost:5173
# or explicitly:
npm run dev -w @agent-master/web
```

Open http://localhost:5173. On first load you'll see the **Connect** screen:

1. **Daemon URL** — defaults to `http://localhost:8888`. Point it at your
   daemon (e.g. a Tailscale address `http://100.x.x.x:8888`).
2. **Token** — run `agent-master token` on the daemon machine and paste it.

The connection (URL + token) is saved to `localStorage`. Use **Disconnect** to
clear it.

Then: **+ New session** (prompts for a workspace directory on the daemon
machine), select a session to load its history and open the live stream, and
send messages from the composer. Enter sends; Shift+Enter inserts a newline.
While a run is active the composer is disabled and an **Interrupt** button
appears.

## Pointing at a daemon

The `ApiClient`/`SseClient` always use the **absolute base URL** you enter in
the Connect screen — this is what production uses. The daemon sends permissive
CORS headers, so cross-origin calls from the Vite dev server work directly.

**Dev fallback (if CORS is unavailable):** `apps/web/vite.config.ts` also
proxies `/api` and `/health` to `http://localhost:8888` (override with the
`AM_PROXY_TARGET` env var). To route through the proxy, set the Daemon URL to
the Vite origin itself (`http://localhost:5173`).

## Scripts

Run from `frontend/`:

- `npm run typecheck` — typecheck every workspace (`tsc --noEmit`).
- `npm run build` — build every workspace (core → `tsc` emit; web → Vite build).
- `npm run dev` — run the web app (Vite dev server).

Per package:

- `npm run build -w @agent-master/web` — production web build to
  `apps/web/dist`.
- `npm run preview -w @agent-master/web` — preview the production build.

## Integration smoke test (real daemon)

Exercises `ApiClient` + `SseClient` end-to-end against a running daemon: creates
a session, sends a message, and reads the SSE stream until `run_finished`,
printing the assistant reply. Node 20 has global `fetch` but no `EventSource`,
so the script injects one from the `eventsource` devDependency.

```bash
# Start a daemon (auto-creates config on first run):
agent-master serve --port 8899 &
TOKEN=$(agent-master token)

# Run the smoke test (builds core first, then runs it):
AM_BASE_URL=http://localhost:8899 AM_TOKEN=$TOKEN \
  npm run smoke -w @agent-master/core

# Stop the daemon when done:
kill %1
```

A real Claude run takes ~10–45s; `claude` must be installed and logged in on the
daemon machine.
```
