# agent-master HTTP API (v1)

Single source of truth for the daemon ↔ client contract. Backend (Go) and
frontend (TS) both conform to this. Base URL: `http://<host>:<port>` (default
port 8888).

## Auth

- All `/api/*` require `Authorization: Bearer <token>`.
- The SSE stream additionally accepts `?token=<token>` (browsers can't set
  headers on `EventSource`).
- Errors are JSON `{ "error": "<message>" }` with an appropriate HTTP status.
- CORS: the daemon sends permissive CORS headers (token is the real guard) and
  answers `OPTIONS` preflight, so browser clients may call it cross-origin.

## Types

```ts
type Session = {
  id: string
  title: string
  provider: string        // "claude"
  model: string           // "" = provider default
  workspaceDir: string
  createdAt: string       // RFC3339
  updatedAt: string
  archived: boolean
}

type RecentSession = {
  id: string
  title: string
  lastPreview: string
  lastSeq: number
  activeRunId?: string     // present while a run is active
  updatedAt: string
}

type WireEvent = {
  seq: number
  type: EventType
  runId?: string
  payload: object          // shape depends on type (below)
  createdAt: string
}

type EventType =
  | "user_message"      // { text: string }
  | "assistant_message" // { text: string }
  | "tool_call"         // { name: string, id: string, input: unknown }
  | "tool_result"       // { id: string, output: unknown }
  | "run_started"       // { runId: string }
  | "run_finished"      // { runId: string, state: "done"|"interrupted"|"failed" }
  | "error"             // { message: string }
```

## REST

| Method | Path | Body | Response |
| --- | --- | --- | --- |
| GET | `/health` | — | `{ status, version }` (public, no auth) |
| GET | `/api/info` | — | `{ name, version, providers }` |
| GET | `/api/sessions?limit=&offset=` | — | `{ sessions: RecentSession[], hasMore: boolean }` |
| POST | `/api/sessions` | `{ workspaceDir, model?, title? }` | `Session` |
| GET | `/api/sessions/{id}` | — | `Session` (404 if missing) |
| DELETE | `/api/sessions/{id}` | — | `{ ok: true }` |
| GET | `/api/sessions/{id}/messages?before_seq=&limit=` | — | `{ events: WireEvent[], hasMore: boolean }` |
| POST | `/api/sessions/{id}/send` | `{ message, clientIntentId? }` | `202 { runId }` (409 if a run is active) |
| POST | `/api/sessions/{id}/interrupt` | — | `{ ok: true }` |
| GET | `/api/sessions/{id}/stream?after_seq=&token=` | — | SSE (below) |

Notes:
- `messages` returns events **ascending by seq**; `hasMore` = older events
  exist before the returned window. Use the smallest returned `seq` as the next
  `before_seq` to page backward. `before_seq=0` (or omitted) = latest page.
- `send` is idempotent on `clientIntentId`: repeating one returns the same run
  without starting a second.

## SSE stream

`GET /api/sessions/{id}/stream`

- Resume with `Last-Event-ID: <seq>` header or `?after_seq=<seq>`.
- The server subscribes to live events before replaying history, so no event is
  missed at the boundary; overlap is de-duplicated by `seq`.
- Frame:

  ```
  id: <seq>
  event: am_event
  data: {"seq":42,"type":"assistant_message","runId":"r_..","payload":{"text":".."},"createdAt":".."}
  ```

- Keep-alive: a `: ping` comment line every 30s.
- On a dropped-subscriber overflow the server sends `event: reconnect` with
  `data: {"afterSeq": <n>}`; the client should reconnect using that seq.
