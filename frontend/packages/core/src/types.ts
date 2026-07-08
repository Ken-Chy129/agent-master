/**
 * Wire types for the agent-master daemon HTTP+SSE API (v1).
 *
 * These mirror docs/API.md exactly. This is a "dumb client": the daemon owns
 * conversation structure; the client renders the event list it is given and
 * does not recompute grouping.
 */

/** Full session record (POST /api/sessions, GET /api/sessions/:id). */
export interface Session {
  id: string;
  title: string;
  provider: string; // "claude"
  model: string; // "" = provider default
  workspaceDir: string;
  createdAt: string; // RFC3339
  updatedAt: string; // RFC3339
  archived: boolean;
}

/** List-projection row (GET /api/sessions). */
export interface RecentSession {
  id: string;
  title: string;
  lastPreview: string;
  lastSeq: number;
  activeRunId?: string; // present while a run is active
  lastRunState?: 'running' | RunState; // state of the most recent run
  workspaceDir: string;
  createdAt: string;
  updatedAt: string;
}

/** Discriminant for a wire event. */
export type EventType =
  | 'user_message'
  | 'assistant_message'
  | 'tool_call'
  | 'tool_result'
  | 'run_started'
  | 'run_finished'
  | 'error';

/** A single ledger event streamed over SSE / returned by /messages. */
export interface WireEvent {
  seq: number;
  type: EventType;
  runId?: string;
  payload: unknown; // shape depends on `type`; see payload types below
  createdAt: string;
}

/**
 * A live-only, token-level assistant text fragment (SSE `am_delta`). Not part of
 * the committed ledger and not resumable: the committed `assistant_message`
 * carries the final text. Clients use deltas only for a live typing preview.
 */
export interface StreamDelta {
  runId: string;
  text: string;
  index: number;
}

// --- Per-type payload shapes (payload is `unknown` on WireEvent; narrow with the
// helpers/guards below or these interfaces when you know the event type). ---

export interface UserMessagePayload {
  text: string;
}
export interface AssistantMessagePayload {
  text: string;
}
export interface ToolCallPayload {
  name: string;
  id: string;
  input: unknown;
}
export interface ToolResultPayload {
  id: string;
  output: unknown;
}
export interface RunStartedPayload {
  runId: string;
}
export type RunState = 'done' | 'interrupted' | 'failed';
export interface RunFinishedPayload {
  runId: string;
  state: RunState;
}
export interface ErrorPayload {
  message: string;
}

/** Maps each EventType to its payload shape. */
export interface EventPayloadMap {
  user_message: UserMessagePayload;
  assistant_message: AssistantMessagePayload;
  tool_call: ToolCallPayload;
  tool_result: ToolResultPayload;
  run_started: RunStartedPayload;
  run_finished: RunFinishedPayload;
  error: ErrorPayload;
}

// --- REST response envelopes ---

export interface HealthResponse {
  status: string;
  version: string;
}

/**
 * GET /api/info. `providers` is an object keyed by provider id in the real
 * daemon (e.g. { claude: { available, path } }); typed loosely so we do not
 * over-constrain a v1-evolving shape.
 */
export interface InfoResponse {
  name: string;
  version: string;
  providers: Record<string, { available: boolean; path?: string }>;
}

export interface ListSessionsResponse {
  sessions: RecentSession[];
  hasMore?: boolean;
}

export interface ListMessagesResponse {
  events: WireEvent[];
  hasMore?: boolean;
}

export interface CreateSessionRequest {
  workspaceDir: string;
  model?: string;
  title?: string;
}

export interface RenameSessionRequest {
  title: string;
}

export interface SendRequest {
  message: string;
  clientIntentId?: string;
}

export interface SendResponse {
  runId: string;
}

export interface OkResponse {
  ok: true;
}

/** A browsable directory entry (GET /api/workspaces). */
export interface WorkspaceEntry {
  name: string;
  path: string;
}

// --- Server-derived render state (SSE `am_render`, GET /api/sessions/:id/render) ---

/** One ready-to-display row; `kind` selects which fields are meaningful. */
export interface RenderRow {
  kind: 'user' | 'assistant' | 'tool' | 'error';
  id: string;
  seq: number;
  text?: string; // user / assistant / error
  name?: string; // tool
  input?: unknown; // tool
  output?: unknown; // tool (present once the result lands)
  status?: 'running' | 'done'; // tool
}

/**
 * The server-folded transcript snapshot. Clients dumb-render `rows` and derive
 * run state from `tailActivity` / `lastRunState` — no local tool pairing,
 * run-status, or ordering logic.
 */
export interface RenderState {
  basedOnSeq: number;
  rows: RenderRow[];
  tailActivity: 'idle' | 'running';
  lastRunState?: 'done' | 'interrupted' | 'failed';
}

/** GET /api/workspaces?path= — directory listing for choosing a workspace. */
export interface WorkspaceListing {
  path: string; // current directory ("" when listing roots)
  parent: string; // parent directory ("" if none / not allowed)
  roots: string[]; // configured workspace roots (may be empty)
  entries: WorkspaceEntry[];
}
