import type {
  CreateSessionRequest,
  HealthResponse,
  InfoResponse,
  ListMessagesResponse,
  ListSessionsResponse,
  OkResponse,
  SendRequest,
  SendResponse,
  Session,
} from './types.js';

export interface ApiClientConfig {
  /** Absolute daemon base URL, e.g. "http://localhost:8888". No trailing slash required. */
  baseUrl: string;
  /** Bearer token for this machine. */
  token: string;
  /** Optional fetch override (tests / non-browser runtimes). Defaults to global fetch. */
  fetch?: typeof fetch;
}

/**
 * Typed error carrying the HTTP status and the server's `{ error }` message.
 * Thrown by every ApiClient method on a non-2xx response.
 */
export class ApiError extends Error {
  readonly status: number;
  readonly url: string;
  /** Raw parsed body if it was JSON, else the text body. */
  readonly body: unknown;

  constructor(status: number, message: string, url: string, body: unknown) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.url = url;
    this.body = body;
  }
}

function trimTrailingSlash(s: string): string {
  return s.endsWith('/') ? s.slice(0, -1) : s;
}

/**
 * REST client for the agent-master daemon. All /api/* calls send
 * `Authorization: Bearer <token>`. On any non-2xx response an {@link ApiError}
 * is thrown carrying the status and the server's `{ error }` message.
 */
export class ApiClient {
  readonly baseUrl: string;
  private readonly token: string;
  private readonly fetchImpl: typeof fetch;

  constructor(config: ApiClientConfig) {
    this.baseUrl = trimTrailingSlash(config.baseUrl);
    this.token = config.token;
    const f = config.fetch ?? globalThis.fetch;
    if (!f) {
      throw new Error('No fetch implementation available; pass one via config.fetch');
    }
    // Bind so `this` is not lost when using the global.
    this.fetchImpl = f.bind(globalThis);
  }

  // --- endpoints ---

  /** GET /health (public, no auth). */
  health(): Promise<HealthResponse> {
    return this.request<HealthResponse>('GET', '/health', { auth: false });
  }

  /** GET /api/info. */
  info(): Promise<InfoResponse> {
    return this.request<InfoResponse>('GET', '/api/info');
  }

  /** GET /api/sessions?limit=&offset= */
  listSessions(limit?: number, offset?: number): Promise<ListSessionsResponse> {
    const q = new URLSearchParams();
    if (limit != null) q.set('limit', String(limit));
    if (offset != null) q.set('offset', String(offset));
    return this.request<ListSessionsResponse>('GET', `/api/sessions${qs(q)}`);
  }

  /** POST /api/sessions */
  createSession(body: CreateSessionRequest): Promise<Session> {
    return this.request<Session>('POST', '/api/sessions', { body });
  }

  /** GET /api/sessions/:id (404 -> ApiError). */
  getSession(id: string): Promise<Session> {
    return this.request<Session>('GET', `/api/sessions/${encodeURIComponent(id)}`);
  }

  /** DELETE /api/sessions/:id */
  deleteSession(id: string): Promise<OkResponse> {
    return this.request<OkResponse>('DELETE', `/api/sessions/${encodeURIComponent(id)}`);
  }

  /**
   * GET /api/sessions/:id/messages?before_seq=&limit=
   * Events are returned ascending by seq. Use the smallest returned seq as the
   * next `beforeSeq` to page backward.
   */
  getMessages(
    id: string,
    opts: { beforeSeq?: number; limit?: number } = {},
  ): Promise<ListMessagesResponse> {
    const q = new URLSearchParams();
    if (opts.beforeSeq != null) q.set('before_seq', String(opts.beforeSeq));
    if (opts.limit != null) q.set('limit', String(opts.limit));
    return this.request<ListMessagesResponse>(
      'GET',
      `/api/sessions/${encodeURIComponent(id)}/messages${qs(q)}`,
    );
  }

  /**
   * POST /api/sessions/:id/send -> 202 { runId }.
   * Idempotent on `clientIntentId`. Throws ApiError(409) if a run is active.
   */
  send(id: string, body: SendRequest): Promise<SendResponse> {
    return this.request<SendResponse>('POST', `/api/sessions/${encodeURIComponent(id)}/send`, {
      body,
    });
  }

  /** POST /api/sessions/:id/interrupt */
  interrupt(id: string): Promise<OkResponse> {
    return this.request<OkResponse>(
      'POST',
      `/api/sessions/${encodeURIComponent(id)}/interrupt`,
    );
  }

  // --- internals ---

  private async request<T>(
    method: string,
    path: string,
    opts: { body?: unknown; auth?: boolean } = {},
  ): Promise<T> {
    const url = `${this.baseUrl}${path}`;
    const headers: Record<string, string> = {};
    if (opts.auth !== false) headers['Authorization'] = `Bearer ${this.token}`;

    const init: RequestInit = { method, headers };
    if (opts.body !== undefined) {
      headers['Content-Type'] = 'application/json';
      init.body = JSON.stringify(opts.body);
    }

    const res = await this.fetchImpl(url, init);
    const text = await res.text();
    const parsed = parseJson(text);

    if (!res.ok) {
      const message = extractErrorMessage(parsed) ?? `HTTP ${res.status}`;
      throw new ApiError(res.status, message, url, parsed ?? text);
    }

    // 204 or empty body -> return an empty object cast to T.
    if (text.length === 0) return {} as T;
    return parsed as T;
  }
}

function qs(q: URLSearchParams): string {
  const s = q.toString();
  return s ? `?${s}` : '';
}

function parseJson(text: string): unknown {
  if (text.length === 0) return undefined;
  try {
    return JSON.parse(text);
  } catch {
    return undefined;
  }
}

function extractErrorMessage(body: unknown): string | undefined {
  if (body && typeof body === 'object' && 'error' in body) {
    const e = (body as { error: unknown }).error;
    if (typeof e === 'string') return e;
  }
  return undefined;
}
